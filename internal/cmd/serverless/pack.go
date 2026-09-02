package serverless

import (
	"archive/zip"
	"bytes"
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

// runwareIgnoreFile is the project's own exclude list, and the only one read.
//
// .gitignore is deliberately NOT consulted. What a project keeps out of version
// control is a different question from what it ships to a builder -- a generated
// asset the app needs at run time is a routine .gitignore entry -- and a file
// silently missing from a deployment because of a rule written for git is the
// kind of surprise that costs an afternoon. Exclusions are opt-in and local to
// this file.
const runwareIgnoreFile = ".runwareignore"

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

// packDirectory zips srcDir and returns the archive bytes plus the path of
// modelFile inside it.
//
// srcDir is the archive root: every path in the zip is relative to it, which is
// what the builder puts on PYTHONPATH and hands to MLflow code_paths, so the
// project's own imports resolve at build and serve time exactly as they do
// locally. An empty srcDir means the working directory.
func packDirectory(srcDir, modelFile string) (archive []byte, modelFileRel string, err error) {
	root, err := resolveSrcDir(srcDir)
	if err != nil {
		return nil, "", err
	}

	modelFileRel, err = relativeModelFile(root, modelFile)
	if err != nil {
		return nil, "", err
	}

	matcher, err := loadIgnoreMatcher(root)
	if err != nil {
		return nil, "", err
	}

	files, err := collectFiles(root, modelFileRel, matcher)
	if err != nil {
		return nil, "", err
	}
	if len(files) == 0 {
		// Unreachable while the model file is forced in, but a packer that can
		// return an empty archive should say so rather than let the builder
		// answer with a 422 about a missing model file.
		return nil, "", fmt.Errorf("no files to pack under %q", root)
	}

	raw, err := writeArchive(root, files)
	if err != nil {
		return nil, "", err
	}
	return raw, modelFileRel, nil
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
// then the project's own .runwareignore, so a project rule can override a
// default. Nothing else is read -- see runwareIgnoreFile for why .gitignore is
// not.
func loadIgnoreMatcher(root string) (gitignore.Matcher, error) {
	patterns := make([]gitignore.Pattern, 0, len(defaultIgnorePatterns))
	for _, p := range defaultIgnorePatterns {
		patterns = append(patterns, gitignore.ParsePattern(p, nil))
	}

	lines, err := readIgnoreFile(filepath.Join(root, runwareIgnoreFile))
	if err != nil {
		return nil, err
	}
	for _, line := range lines {
		patterns = append(patterns, gitignore.ParsePattern(line, nil))
	}

	return gitignore.NewMatcher(patterns), nil
}

// readIgnoreFile returns the file's lines, or nil when it is absent.
func readIgnoreFile(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}

	// Lines are passed through verbatim apart from a CR: gitignore syntax is
	// whitespace-significant, so trimming would rewrite valid patterns -- an
	// escaped trailing space (`name\ `) loses its escape and a leading space is
	// part of the pattern. Blank lines and comments are the parser's own
	// business; ParsePattern handles both, and a pattern that begins with an
	// escaped `#` must not be mistaken for one here.
	lines := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		lines = append(lines, strings.TrimSuffix(line, "\r"))
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
			if d.IsDir() {
				// An excluded directory is pruned rather than walked, which keeps
				// a .venv from costing a stat per file and reproduces git's own
				// rule that a negation cannot re-include a file whose parent
				// directory is excluded.
				//
				// Except when the model file is inside it. Pruning there would
				// drop the one entry the build cannot proceed without, and the
				// exemption above never fires because the walk stops at the
				// directory, whose path is not the model file's. Descend, and let
				// the per-file checks exclude everything else it holds.
				if isAncestorOf(rel, modelFileRel) {
					return nil
				}
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
			// Silently skipping the model file would upload an archive whose
			// declared entry point is missing, which the builder can only report
			// as a 422 after the upload. Say it here instead.
			if rel == modelFileRel {
				return fmt.Errorf(
					"model file %s is a %s, not a regular file; point --src-dir at the directory holding the real file",
					rel, d.Type().String(),
				)
			}
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

// isAncestorOf reports whether dir is a parent directory of file, both being
// slash-separated paths relative to the archive root.
func isAncestorOf(dir, file string) bool {
	return strings.HasPrefix(file, dir+"/")
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

	// Counted here rather than trusted from the walk: a file may grow between
	// being stat'd and being read, and several files each staying under the
	// per-file cap can still cross the total.
	var written int64
	for _, f := range files {
		n, err := writeArchiveEntry(zw, root, f)
		if err != nil {
			return nil, err
		}
		written += n
		if written > maxPackTotalBytes {
			return nil, fmt.Errorf(
				"the files grew past the %s archive limit while packing; exclude what the app does not need with a %s file",
				humanBytes(maxPackTotalBytes), runwareIgnoreFile,
			)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}
	return buf.Bytes(), nil
}

func writeArchiveEntry(zw *zip.Writer, root string, f packedFile) (int64, error) {
	path := filepath.Join(root, filepath.FromSlash(f.rel))
	src, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", f.rel, err)
	}
	defer src.Close() //nolint:errcheck

	info, err := src.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", f.rel, err)
	}
	// FileInfoHeader rather than zw.Create, because Create writes mode 0. A
	// codebase can hold an executable -- an entrypoint script, a helper binary --
	// and one that arrives without its executable bit fails at run time with
	// nothing about the archive to explain it. The name has to be re-set: the
	// header takes it from the FileInfo, which knows only the base name.
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return 0, fmt.Errorf("build zip header for %s: %w", f.rel, err)
	}
	header.Name = f.rel
	header.Method = zip.Deflate

	w, err := zw.CreateHeader(header)
	if err != nil {
		return 0, fmt.Errorf("create zip entry %s: %w", f.rel, err)
	}
	// Capped in case the file grew between the walk and here, so a file that
	// changes underneath us cannot defeat the per-file limit checked above.
	written, err := io.Copy(w, io.LimitReader(src, maxPackEntryBytes+1))
	if err != nil {
		return 0, fmt.Errorf("write zip entry %s: %w", f.rel, err)
	}
	if written > maxPackEntryBytes {
		return 0, fmt.Errorf(
			"%s grew past the maximum for a single file (%s) while packing",
			f.rel, humanBytes(maxPackEntryBytes),
		)
	}
	return written, nil
}
