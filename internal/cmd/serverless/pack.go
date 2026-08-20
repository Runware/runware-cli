package serverless

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// maxPackEntryBytes is the maximum size of a single file we will put in the
// archive. Keeps one accidental artefact — a checkpoint, a dataset someone left
// in the project — from blowing memory, since the whole archive is held in
// memory and base64 expands it by ~4/3.
const maxPackEntryBytes int64 = 10 << 20 // 10 MiB

// maxPackTotalBytes bounds the archive as a whole. The per-file cap alone does
// not: a virtualenv is thousands of small files and would sail past it.
const maxPackTotalBytes int64 = 25 << 20 // 25 MiB

// runwareIgnoreFile is the project's own exclude list. When it is absent the
// packer falls back to gitIgnoreFile, because a project that already tells git
// what not to track has usually said the same thing this needs to know.
const (
	runwareIgnoreFile = ".runwareignore"
	gitIgnoreFile     = ".gitignore"
)

// defaultIgnorePatterns are excluded when no rule says otherwise. They are
// ordinary gitignore patterns evaluated before the project's own, so a later
// `!*.pyc` re-includes what one of the file patterns here excluded. The
// directory patterns are not negatable the same way -- git does not allow
// re-including a file whose parent directory is excluded, and collectFiles
// prunes those directories for exactly that reason.
var defaultIgnorePatterns = []string{
	// Python build and cache output. None of it is source, and a virtualenv is
	// the difference between an 80KB upload and a 400MB one.
	"__pycache__/",
	"*.pyc",
	"*.pyo",
	".venv/",
	"venv/",
	"*.egg-info/",
	// Tool caches, which appear in any project that has been tested or linted.
	".pytest_cache/",
	".ruff_cache/",
	".mypy_cache/",
	".tox/",
	".ipynb_checkpoints/",
	// Everything else.
	"node_modules/",
	".DS_Store",
	runwareIgnoreFile,
}

// alwaysExcluded reports paths that no rule may re-include.
//
// `.env` and its variants hold credentials, and the build unpacks this archive
// into a pod where its contents are readable — the builder keeps top-level
// dotfiles out of the *image*, but that is a later step and not a promise about
// the build. Matching on every path segment rather than the root alone, because
// `config/.env` is the same secret one directory down. `.git` goes with it:
// nothing in the build reads it, and it carries the whole history of everything
// the other rules exclude.
func alwaysExcluded(segments []string) bool {
	for _, s := range segments {
		if s == ".git" || s == ".env" || strings.HasPrefix(s, ".env.") {
			return true
		}
	}
	return false
}

// packDirectory zips srcDir and returns the base64-encoded archive plus the path
// of modelFile inside it.
//
// srcDir is the archive root: every path in the zip is relative to it, which is
// what the builder puts on PYTHONPATH and hands to MLflow code_paths, so the
// project's own imports resolve at build and serve time exactly as they do
// locally. An empty srcDir means the working directory.
func packDirectory(srcDir, modelFile string) (zipBase64, modelFileRel string, err error) {
	root, err := resolveSrcDir(srcDir)
	if err != nil {
		return "", "", err
	}

	modelFileRel, err = relativeModelFile(root, modelFile)
	if err != nil {
		return "", "", err
	}

	matcher, err := loadIgnoreMatcher(root)
	if err != nil {
		return "", "", err
	}

	files, err := collectFiles(root, modelFileRel, matcher)
	if err != nil {
		return "", "", err
	}
	if len(files) == 0 {
		// Unreachable while the model file is forced in, but a packer that can
		// return an empty archive should say so rather than let the builder
		// answer with a 422 about a missing model file.
		return "", "", fmt.Errorf("no files to pack under %q", root)
	}

	raw, err := writeArchive(root, files)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(raw), modelFileRel, nil
}

// resolveSrcDir defaults an empty srcDir to the working directory and checks it
// is a directory. Symlinks are resolved so the relative path of the model file
// is computed against the same tree WalkDir will produce.
func resolveSrcDir(srcDir string) (string, error) {
	if srcDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
		srcDir = wd
	}
	abs, err := filepath.Abs(srcDir)
	if err != nil {
		return "", fmt.Errorf("resolve source directory: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("read source directory %q: %w", srcDir, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("read source directory %q: %w", srcDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source directory %q is not a directory", srcDir)
	}
	return resolved, nil
}

// relativeModelFile locates the model file inside the archive root.
//
// The builder rejects a modelFile that is absolute or escapes the codebase, and
// answers 422 for a modelFile it cannot find in the zip. Both are worth catching
// here: the failure is the caller's to fix and the archive may be megabytes.
func relativeModelFile(root, modelFile string) (string, error) {
	if modelFile == "" {
		return "", fmt.Errorf("model file is required")
	}
	abs := locateModelFile(root, modelFile)
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf(
			"read model file %q: %w (relative paths are resolved inside the source directory %s)",
			modelFile, err, root,
		)
	}
	if info.IsDir() {
		return "", fmt.Errorf("model file %q is a directory", modelFile)
	}
	// EvalSymlinks on the parent only: a model file reached through a symlinked
	// directory still belongs to the tree, and resolving the file itself would
	// reject the common case of an editor's saved-through link.
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", fmt.Errorf("read model file %q: %w", modelFile, err)
	}
	abs = filepath.Join(parent, filepath.Base(abs))

	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", fmt.Errorf("locate model file inside %q: %w", root, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf(
			"model file %q is outside the source directory %q; pass --src-dir pointing at the project root",
			modelFile, root,
		)
	}
	return filepath.ToSlash(rel), nil
}

// locateModelFile resolves a model file argument to an absolute path.
//
// One rule: absolute is taken as given, relative is relative to the source
// directory. modelFile is a path *inside* the codebase -- that is what the field
// means on the wire and what the builder looks up in the zip -- so the source
// directory is the only sensible thing for it to be relative to. With no
// --src-dir the source directory is the working directory, so `deploy ./app.py`
// from the project means what it always did.
func locateModelFile(root, modelFile string) string {
	if filepath.IsAbs(modelFile) {
		return modelFile
	}
	return filepath.Join(root, modelFile)
}

// loadIgnoreMatcher builds the exclusion matcher: the built-in defaults first,
// then the project's own rules, so a project rule can override a default.
// .runwareignore wins outright when present — a project that writes one is
// saying what to ship, and silently unioning .gitignore into it would make the
// result impossible to reason about.
func loadIgnoreMatcher(root string) (gitignore.Matcher, error) {
	patterns := make([]gitignore.Pattern, 0, len(defaultIgnorePatterns))
	for _, p := range defaultIgnorePatterns {
		patterns = append(patterns, gitignore.ParsePattern(p, nil))
	}

	for _, name := range []string{runwareIgnoreFile, gitIgnoreFile} {
		lines, err := readIgnoreFile(filepath.Join(root, name))
		if err != nil {
			return nil, err
		}
		if lines == nil {
			continue
		}
		for _, line := range lines {
			patterns = append(patterns, gitignore.ParsePattern(line, nil))
		}
		break
	}

	return gitignore.NewMatcher(patterns), nil
}

// readIgnoreFile returns the file's meaningful lines, or nil when it is absent.
// A present-but-empty file returns a non-nil empty slice, so it still counts as
// "the project chose .runwareignore" and suppresses the .gitignore fallback.
func readIgnoreFile(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}

	lines := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines, nil
}

// packedFile is one archive entry: its path relative to the root, and its size.
type packedFile struct {
	rel  string
	size int64
}

// collectFiles walks the tree and returns what belongs in the archive, in the
// lexical order WalkDir yields — so the same tree always packs to the same bytes.
func collectFiles(root, modelFileRel string, matcher gitignore.Matcher) ([]packedFile, error) {
	var files []packedFile
	var total int64

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		segments := strings.Split(rel, "/")

		if alwaysExcluded(segments) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// The model file is packed whatever the rules say: the deploy cannot
		// succeed without it, and an ignore rule that happens to cover it is a
		// worse failure than a file the customer did not mean to ship.
		if rel != modelFileRel && matcher.Match(segments, d.IsDir()) {
			// Pruning the directory rather than descending is what keeps a
			// .venv from costing a stat per file. It also reproduces git's own
			// rule -- "it is not possible to re-include a file if a parent
			// directory of that file is excluded" -- so a `!node_modules/keep.js`
			// does not apply, exactly as it would not for git.
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// Regular files only. A symlink may point outside the tree, and the
		// archive is a copy rather than a checkout; sockets and devices have
		// nothing to copy.
		if !d.Type().IsRegular() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxPackEntryBytes {
			return fmt.Errorf(
				"%s is %s; the maximum for a single file is %s. Exclude it with a %s rule",
				rel, humanBytes(info.Size()), humanBytes(maxPackEntryBytes), runwareIgnoreFile,
			)
		}
		total += info.Size()
		files = append(files, packedFile{rel: rel, size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}

	if total > maxPackTotalBytes {
		return nil, fmt.Errorf(
			"the source directory holds %s of files to pack; the maximum is %s.\n%s\nExclude what the app does not need with a %s file",
			humanBytes(total), humanBytes(maxPackTotalBytes), largestFilesSummary(files), runwareIgnoreFile,
		)
	}
	return files, nil
}

// largestFilesSummary names what filled the archive. "Too big" on its own leaves
// the caller to find the offender by hand, which for a deep tree is the whole
// problem rather than a detail of it.
func largestFilesSummary(files []packedFile) string {
	sorted := make([]packedFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].size > sorted[j].size })

	var b strings.Builder
	b.WriteString("Largest files:")
	for i, f := range sorted {
		if i == 5 {
			break
		}
		fmt.Fprintf(&b, "\n  %8s  %s", humanBytes(f.size), f.rel)
	}
	return b.String()
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// writeArchive builds the zip. Entry names are the already-slashed relative
// paths: zip names are always "/"-separated, which is what makes an archive
// packed on Windows unpack correctly in the Linux build pod.
func writeArchive(root string, files []packedFile) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for _, f := range files {
		if err := writeArchiveEntry(zw, root, f); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}
	return buf.Bytes(), nil
}

func writeArchiveEntry(zw *zip.Writer, root string, f packedFile) error {
	src, err := os.Open(filepath.Join(root, filepath.FromSlash(f.rel)))
	if err != nil {
		return fmt.Errorf("open %s: %w", f.rel, err)
	}
	defer src.Close() //nolint:errcheck

	w, err := zw.Create(f.rel)
	if err != nil {
		return fmt.Errorf("create zip entry %s: %w", f.rel, err)
	}
	// Capped in case the file grew between the walk and here, so a file that
	// changes underneath us cannot defeat the limit checked above.
	written, err := io.Copy(w, io.LimitReader(src, maxPackEntryBytes+1))
	if err != nil {
		return fmt.Errorf("write zip entry %s: %w", f.rel, err)
	}
	if written > maxPackEntryBytes {
		return fmt.Errorf(
			"%s grew past the maximum for a single file (%s) while packing",
			f.rel, humanBytes(maxPackEntryBytes),
		)
	}
	return nil
}
