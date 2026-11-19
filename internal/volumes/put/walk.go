package put

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxTargetBytes = 4096
	// modeMask keeps the read and execute bits, the only ones a Volume file mode holds.
	modeMask = 0o555
)

var (
	// ErrNoFiles is a put whose paths hold no file.
	ErrNoFiles = errors.New("nothing to upload: the paths hold no files")
	// ErrEscapingSymlink is a symlink inside an uploaded directory that points outside it.
	ErrEscapingSymlink = errors.New("is a symlink that points outside the uploaded directory; remove the link or replace it with a copy of its target")
	// ErrSymlinkDirectory is a symlink to a directory, which put does not follow.
	ErrSymlinkDirectory = errors.New("is a symlink to a directory, which put does not follow")
	// ErrNotRegular is a path that is not a regular file; a Volume holds regular files only.
	ErrNotRegular = errors.New("is not a regular file")
	// ErrDuplicateTarget is two local files that would land at one path in the Volume.
	ErrDuplicateTarget = errors.New("would land at a path another file already takes")
	// ErrInvalidTarget is a path the Volume cannot hold.
	ErrInvalidTarget = errors.New("is not a valid Volume path")
)

// Source is one local file to upload.
type Source struct {
	Abs     string    // the absolute path the file is read from, symlinks resolved
	Target  string    // the file's path in the Volume
	Size    int64     // the size when it was hashed
	ModTime time.Time // the modification time when it was hashed
	Change  int64     // the change time when it was hashed, in Unix nanoseconds; zero when it cannot be read
	Mode    uint32    // the Volume mode: the file's read and execute bits
	SHA256  [32]byte  // the plaintext digest, set by hashing
}

// Walk lists the files the paths name, sorted by Volume path, under the directory to when it is set.
func Walk(paths []string, to string) ([]Source, error) {
	prefix, err := cleanTarget(to)
	if err != nil {
		return nil, fmt.Errorf("destination directory %q %w", to, err)
	}
	var sources []Source
	for _, p := range paths {
		got, err := walkPath(p, prefix)
		if err != nil {
			return nil, err
		}
		sources = append(sources, got...)
	}
	if len(sources) == 0 {
		return nil, ErrNoFiles
	}
	slices.SortStableFunc(sources, func(a, b Source) int { return strings.Compare(a.Target, b.Target) })
	for i := 1; i < len(sources); i++ {
		if sources[i].Target == sources[i-1].Target {
			return nil, fileErr(sources[i].Abs, fmt.Errorf("%w: %q is also taken by %s", ErrDuplicateTarget, sources[i].Target, sources[i-1].Abs))
		}
	}
	return sources, nil
}

// cleanTarget trims the slashes around the destination directory and checks the rest.
func cleanTarget(to string) (string, error) {
	to = strings.Trim(filepath.ToSlash(to), "/")
	if to == "" {
		return "", nil
	}
	if reason := checkTarget(to); reason != "" {
		return "", errors.New(reason)
	}
	return to, nil
}

func walkPath(p, prefix string) ([]Source, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fileErr(abs, err)
	}
	switch {
	case info.Mode().IsRegular():
		src, err := newSource(resolved, path.Join(prefix, filepath.Base(abs)), info)
		if err != nil {
			return nil, fileErr(abs, err)
		}
		return []Source{src}, nil
	case info.IsDir():
		return walkDir(resolved, prefix)
	default:
		return nil, fileErr(abs, ErrNotRegular)
	}
}

func walkDir(root, prefix string) ([]Source, error) {
	var sources []Source
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		src, err := dirSource(root, p, d, prefix)
		if err != nil {
			return err
		}
		sources = append(sources, src)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sources, nil
}

func dirSource(root, p string, d fs.DirEntry, prefix string) (Source, error) {
	info, err := d.Info()
	if err != nil {
		return Source{}, fileErr(p, err)
	}
	abs := p
	if info.Mode()&fs.ModeSymlink != 0 {
		abs, err = filepath.EvalSymlinks(p)
		if err != nil {
			return Source{}, fileErr(p, err)
		}
		if !within(root, abs) {
			return Source{}, fileErr(p, ErrEscapingSymlink)
		}
		if info, err = os.Stat(abs); err != nil {
			return Source{}, fileErr(p, err)
		}
		if info.IsDir() {
			return Source{}, fileErr(p, ErrSymlinkDirectory)
		}
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return Source{}, fileErr(p, err)
	}
	src, err := newSource(abs, path.Join(prefix, filepath.ToSlash(rel)), info)
	if err != nil {
		return Source{}, fileErr(p, err)
	}
	return src, nil
}

func newSource(abs, target string, info fs.FileInfo) (Source, error) {
	if !info.Mode().IsRegular() {
		return Source{}, ErrNotRegular
	}
	if reason := checkTarget(target); reason != "" {
		return Source{}, fmt.Errorf("%w: %q %s", ErrInvalidTarget, target, reason)
	}
	return Source{
		Abs:     abs,
		Target:  target,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Change:  changeTime(abs, info),
		Mode:    uint32(info.Mode().Perm() & modeMask),
	}, nil
}

// within reports whether p is root or a path under it.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// checkTarget returns why p is not a Volume path, or "" when it is.
func checkTarget(p string) string {
	if p == "" {
		return "is empty"
	}
	if len(p) > maxTargetBytes {
		return fmt.Sprintf("is %d bytes, over the %d-byte limit", len(p), maxTargetBytes)
	}
	if !utf8.ValidString(p) {
		return "is not valid UTF-8"
	}
	for comp := range strings.SplitSeq(p, "/") {
		switch comp {
		case "":
			return "has an empty component"
		case ".", "..":
			return fmt.Sprintf("has a %q component", comp)
		}
		if strings.ContainsFunc(comp, isControl) {
			return "has a control character"
		}
	}
	return ""
}

func isControl(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == 0x2028 || r == 0x2029
}
