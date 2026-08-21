package serverless

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// writeTree materialises files under dir. Keys are slash-separated relative
// paths; parent directories are created as needed.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// unpack decodes an archive into path -> contents.
func unpack(t *testing.T, encoded string) map[string]string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	out := make(map[string]string, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(content)
	}
	return out
}

// Repeated across the table-free tests below; goconst wants them named.
const (
	testModelFile = "app.py"
	testPySource  = "x = 1\n"
)

func names(packed map[string]string) []string {
	out := make([]string, 0, len(packed))
	for name := range packed {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestPackDirectory proves the whole project ships, not just the entry file:
// nested modules and data files keep the relative paths the app imports them by,
// which is what the builder puts on PYTHONPATH.
func TestPackDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		testModelFile:      "import lib\n",
		"lib/__init__.py":  "",
		"lib/helpers.py":   "def go(): pass\n",
		"data/prompt.txt":  "hello",
		"nested/deep/x.py": testPySource,
	})

	encoded, modelFile, err := packDirectory(dir, filepath.Join(dir, testModelFile))
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}
	if modelFile != testModelFile {
		t.Errorf("modelFile = %q, want app.py", modelFile)
	}

	packed := unpack(t, encoded)
	for _, want := range []string{testModelFile, "lib/helpers.py", "data/prompt.txt", "nested/deep/x.py"} {
		if _, ok := packed[want]; !ok {
			t.Errorf("%q missing from the archive; got %v", want, names(packed))
		}
	}
	if packed["lib/helpers.py"] != "def go(): pass\n" {
		t.Errorf("lib/helpers.py content = %q", packed["lib/helpers.py"])
	}
}

// TestPackDirectory_DefaultsToWorkingDirectory covers `deploy ./app.py` with no
// --src-dir, which is the common invocation.
func TestPackDirectory_DefaultsToWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		testModelFile: testPySource,
		"lib.py":      "y = 2\n",
	})
	t.Chdir(dir)

	encoded, modelFile, err := packDirectory("", testModelFile)
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}
	if modelFile != testModelFile {
		t.Errorf("modelFile = %q, want app.py", modelFile)
	}
	if got := names(unpack(t, encoded)); len(got) != 2 {
		t.Errorf("archive = %v, want app.py and lib.py", got)
	}
}

// TestPackDirectory_ModelFileInSubdirectory proves modelFile is reported as the
// path inside the archive, which is what the builder looks up.
func TestPackDirectory_ModelFileInSubdirectory(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"src/app.py": testPySource})

	_, modelFile, err := packDirectory(dir, filepath.Join(dir, "src", testModelFile))
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}
	if modelFile != "src/app.py" {
		t.Errorf("modelFile = %q, want src/app.py", modelFile)
	}
}

// TestPackDirectory_ModelFileOutsideRoot fails locally rather than uploading an
// archive the builder will reject with a 422.
func TestPackDirectory_ModelFileOutsideRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	writeTree(t, root, map[string]string{"keep.py": ""})
	writeTree(t, base, map[string]string{"outside.py": ""})

	_, _, err := packDirectory(root, filepath.Join(base, "outside.py"))
	if err == nil {
		t.Fatal("expected an error for a model file outside the source directory")
	}
	if !strings.Contains(err.Error(), "--src-dir") {
		t.Errorf("error %q does not say how to fix it", err)
	}
}

// TestPackDirectory_DefaultExcludes proves the built-in list keeps the usual
// noise out without any ignore file present.
func TestPackDirectory_DefaultExcludes(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		testModelFile:             "",
		"__pycache__/app.pyc":     "",
		"lib/__pycache__/mod.pyc": "",
		"lib/mod.py":              "",
		".venv/lib/python/x.py":   "",
		"node_modules/pkg/i.js":   "",
		".git/config":             "",
		".DS_Store":               "",
	})

	encoded, _, err := packDirectory(dir, filepath.Join(dir, testModelFile))
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}

	got := names(unpack(t, encoded))
	want := []string{testModelFile, "lib/mod.py"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("archive = %v, want %v", got, want)
	}
}

// TestPackDirectory_RunwareIgnore proves the project's own rules apply, that a
// negation re-includes a file an earlier rule excluded, and that comments and
// blank lines are ignored.
func TestPackDirectory_RunwareIgnore(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		testModelFile:        "",
		"outputs/result.png": "",
		"notes.md":           "",
		"README.md":          "",
		"cached/x.pyc":       "",
		runwareIgnoreFile:    "outputs/\n*.md\n\n# a comment\n!README.md\n",
	})

	encoded, _, err := packDirectory(dir, filepath.Join(dir, testModelFile))
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}

	packed := unpack(t, encoded)
	for _, absent := range []string{"outputs/result.png", "notes.md", runwareIgnoreFile, "cached/x.pyc"} {
		if _, ok := packed[absent]; ok {
			t.Errorf("%q should have been excluded; archive = %v", absent, names(packed))
		}
	}
	// A later ! rule wins over the earlier *.md that excluded it.
	if _, ok := packed["README.md"]; !ok {
		t.Errorf("a ! rule did not re-include README.md; archive = %v", names(packed))
	}
}

// TestPackDirectory_NegationCannotReachIntoExcludedDirectory pins git's own rule:
// "It is not possible to re-include a file if a parent directory of that file is
// excluded." The packer prunes an excluded directory rather than walking it, so a
// negation inside one never applies -- the same result git gives, and the reason
// a .venv costs nothing to skip.
func TestPackDirectory_NegationCannotReachIntoExcludedDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		testModelFile:          "",
		"node_modules/keep.js": "",
		runwareIgnoreFile:      "!node_modules/keep.js\n",
	})

	encoded, _, err := packDirectory(dir, filepath.Join(dir, testModelFile))
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}
	if _, ok := unpack(t, encoded)["node_modules/keep.js"]; ok {
		t.Error("a negation reached inside an excluded directory; git does not allow that")
	}
}

// TestPackDirectory_GitignoreFallback proves .gitignore is honoured when there is
// no .runwareignore, and ignored when there is one — the two must not union, or
// the result is impossible to reason about.
func TestPackDirectory_GitignoreFallback(t *testing.T) {
	tree := map[string]string{
		testModelFile: "",
		"secret.txt":  "",
		"build/o.so":  "",
		".gitignore":  "secret.txt\nbuild/\n",
	}

	t.Run("used when no runwareignore", func(t *testing.T) {
		dir := t.TempDir()
		writeTree(t, dir, tree)

		encoded, _, err := packDirectory(dir, filepath.Join(dir, testModelFile))
		if err != nil {
			t.Fatalf("packDirectory: %v", err)
		}
		packed := unpack(t, encoded)
		if _, ok := packed["secret.txt"]; ok {
			t.Errorf(".gitignore was not honoured; archive = %v", names(packed))
		}
		if _, ok := packed["build/o.so"]; ok {
			t.Errorf(".gitignore directory rule was not honoured; archive = %v", names(packed))
		}
	})

	t.Run("ignored when runwareignore exists", func(t *testing.T) {
		dir := t.TempDir()
		writeTree(t, dir, tree)
		writeTree(t, dir, map[string]string{runwareIgnoreFile: "build/\n"})

		encoded, _, err := packDirectory(dir, filepath.Join(dir, testModelFile))
		if err != nil {
			t.Fatalf("packDirectory: %v", err)
		}
		packed := unpack(t, encoded)
		// .runwareignore says nothing about secret.txt, so it ships even though
		// .gitignore excludes it.
		if _, ok := packed["secret.txt"]; !ok {
			t.Errorf(".gitignore was still applied alongside .runwareignore; archive = %v", names(packed))
		}
		if _, ok := packed["build/o.so"]; ok {
			t.Errorf(".runwareignore rule was not honoured; archive = %v", names(packed))
		}
	})
}

// TestPackDirectory_NeverPacksEnvFiles is the rule that must not be overridable:
// .env files hold credentials and the build pod unpacks whatever it is sent, so
// no ignore rule — not even an explicit negation — may re-include one.
func TestPackDirectory_NeverPacksEnvFiles(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		testModelFile:       "",
		".env":              "SECRET=1",
		".env.local":        "SECRET=2",
		".env.production":   "SECRET=3",
		"config/.env":       "SECRET=4",
		"deep/nest/.env.ci": "SECRET=5",
		"envoy.py":          "not an env file",
		".runwareignore":    "!.env\n!.env.*\n!config/.env\n",
	})

	encoded, _, err := packDirectory(dir, filepath.Join(dir, testModelFile))
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}

	packed := unpack(t, encoded)
	for name, content := range packed {
		if strings.Contains(content, "SECRET=") {
			t.Errorf("%q carries an env secret into the archive", name)
		}
	}
	for _, absent := range []string{".env", ".env.local", ".env.production", "config/.env", "deep/nest/.env.ci"} {
		if _, ok := packed[absent]; ok {
			t.Errorf("%q was packed despite the absolute exclusion; archive = %v", absent, names(packed))
		}
	}
	// The rule matches env files, not every name that starts with "env".
	if _, ok := packed["envoy.py"]; !ok {
		t.Errorf("envoy.py was wrongly treated as an env file; archive = %v", names(packed))
	}
}

// TestPackDirectory_NeverPacksGitDirectory is the second half of the absolute
// exclusions: .git holds the whole history of everything the other rules exclude
// -- including any .env ever committed -- so no rule may re-include it, and a
// nested repo one directory down is the same problem.
func TestPackDirectory_NeverPacksGitDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		testModelFile:            "",
		".git/config":            "[core]",
		".git/objects/ab/cdef":   "history",
		"vendor/dep/.git/config": "[core]",
		"gitignore-notes.md":     "not a git directory",
		runwareIgnoreFile:        "!.git\n!.git/**\n!vendor/dep/.git/**\n",
	})

	encoded, _, err := packDirectory(dir, testModelFile)
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}

	packed := unpack(t, encoded)
	for name := range packed {
		if strings.Contains(name, ".git/") {
			t.Errorf("%q was packed despite the absolute exclusion; archive = %v", name, names(packed))
		}
	}
	// The rule matches the directory, not every name containing "git".
	if _, ok := packed["gitignore-notes.md"]; !ok {
		t.Errorf("gitignore-notes.md was wrongly treated as a git directory; archive = %v", names(packed))
	}
}

// TestPackDirectory_ModelFileAlwaysPacked proves an ignore rule covering the
// entry file cannot produce an archive the build is guaranteed to reject.
func TestPackDirectory_ModelFileAlwaysPacked(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		testModelFile:     "x = 1\n",
		runwareIgnoreFile: "*.py\n",
	})

	encoded, modelFile, err := packDirectory(dir, filepath.Join(dir, testModelFile))
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}
	packed := unpack(t, encoded)
	if _, ok := packed[modelFile]; !ok {
		t.Errorf("the model file was excluded by an ignore rule; archive = %v", names(packed))
	}
}

// TestPackDirectory_SkipsSymlinks: a symlink may point outside the tree, and the
// archive is a copy rather than a checkout.
func TestPackDirectory_SkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{testModelFile: ""})
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("elsewhere"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}

	encoded, _, err := packDirectory(dir, filepath.Join(dir, testModelFile))
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}
	if _, ok := unpack(t, encoded)["link.txt"]; ok {
		t.Error("a symlink was followed into the archive")
	}
}

// TestPackDirectory_Deterministic: the same tree must pack to the same bytes, so
// a redeploy of unchanged source is visibly unchanged.
func TestPackDirectory_Deterministic(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		testModelFile: "",
		"b/two.py":    "",
		"a/one.py":    "",
	})

	first, _, err := packDirectory(dir, filepath.Join(dir, testModelFile))
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}
	second, _, err := packDirectory(dir, filepath.Join(dir, testModelFile))
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}
	if first != second {
		t.Error("packing the same tree twice produced different archives")
	}
}

// TestPackDirectory_ModelFileRelativeToSrcDir is the invocation the --src-dir
// flag exists for: name the entry file as it sits inside the project, from
// wherever you happen to be standing.
func TestPackDirectory_ModelFileRelativeToSrcDir(t *testing.T) {
	project := t.TempDir()
	writeTree(t, project, map[string]string{testModelFile: testPySource})
	t.Chdir(t.TempDir()) // stand somewhere else entirely

	_, modelFile, err := packDirectory(project, testModelFile)
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}
	if modelFile != testModelFile {
		t.Errorf("modelFile = %q, want %q", modelFile, testModelFile)
	}
}

// TestPackDirectory_ModelFileAbsolute: an absolute path is taken as given, which
// is the other half of the rule and what shell completion produces when the
// project is somewhere else.
func TestPackDirectory_ModelFileAbsolute(t *testing.T) {
	project := t.TempDir()
	writeTree(t, project, map[string]string{"src/" + testModelFile: testPySource})
	t.Chdir(t.TempDir())

	_, modelFile, err := packDirectory(project, filepath.Join(project, "src", testModelFile))
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}
	if modelFile != "src/"+testModelFile {
		t.Errorf("modelFile = %q, want src/%s", modelFile, testModelFile)
	}
}

// TestPackDirectory_RelativeModelFileIsNotWorkingDirRelative pins the rule that
// makes the two above unambiguous: a relative path is resolved inside the source
// directory and nowhere else, so a file that exists in the working directory but
// not in the project is an error rather than a silently different app.
func TestPackDirectory_RelativeModelFileIsNotWorkingDirRelative(t *testing.T) {
	project := t.TempDir()
	writeTree(t, project, map[string]string{"real.py": testPySource})

	elsewhere := t.TempDir()
	writeTree(t, elsewhere, map[string]string{testModelFile: "the wrong file\n"})
	t.Chdir(elsewhere)

	_, _, err := packDirectory(project, testModelFile)
	if err == nil {
		t.Fatal("expected an error: the model file exists in the working directory, not the source directory")
	}
	if !strings.Contains(err.Error(), "source directory") {
		t.Errorf("error %q does not explain where relative paths resolve", err)
	}
}

func TestPackDirectory_MissingModelFile(t *testing.T) {
	dir := t.TempDir()
	_, _, err := packDirectory(dir, filepath.Join(dir, "missing.py"))
	if err == nil {
		t.Fatal("expected an error for a missing model file")
	}
}

func TestPackDirectory_MissingSourceDirectory(t *testing.T) {
	_, _, err := packDirectory(filepath.Join(t.TempDir(), "nope"), testModelFile)
	if err == nil {
		t.Fatal("expected an error for a missing source directory")
	}
}

func TestPackDirectory_ModelFileIsDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"pkg/app.py": ""})
	_, _, err := packDirectory(dir, filepath.Join(dir, "pkg"))
	if err == nil {
		t.Fatal("expected an error when the model file is a directory")
	}
}

func TestPackDirectory_FileTooLarge(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{testModelFile: ""})
	f, err := os.Create(filepath.Join(dir, "huge.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxPackEntryBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, _, err = packDirectory(dir, filepath.Join(dir, testModelFile))
	if err == nil {
		t.Fatal("expected an error for an oversized file")
	}
	// The message has to name the file and the remedy, or the caller is left
	// hunting through a deep tree for whichever file it meant.
	if !strings.Contains(err.Error(), "huge.bin") || !strings.Contains(err.Error(), runwareIgnoreFile) {
		t.Errorf("error %q does not name the file and the remedy", err)
	}
}

func TestPackDirectory_TotalTooLarge(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{testModelFile: ""})
	// Several files, each under the per-file cap, over the total.
	for _, name := range []string{"a.bin", "b.bin", "c.bin"} {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(maxPackEntryBytes); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}

	_, _, err := packDirectory(dir, filepath.Join(dir, testModelFile))
	if err == nil {
		t.Fatal("expected an error for an oversized archive")
	}
	if !strings.Contains(err.Error(), "Largest files") {
		t.Errorf("error %q does not say what filled the archive", err)
	}
}

// TestPackDirectory_ModelFileAlwaysPackedFromExcludedDirectory is the reviewer's
// case: the exemption for the model file has to survive an ignore rule that
// matches one of its ANCESTORS. The walk meets the directory first, and the
// directory's path is not the model file's, so a naive prune drops the one entry
// the build cannot proceed without.
func TestPackDirectory_ModelFileAlwaysPackedFromExcludedDirectory(t *testing.T) {
	const nestedModelFile = "dist/" + testModelFile

	cases := []struct {
		name  string
		files map[string]string
	}{
		{
			name: "gitignore directory rule",
			files: map[string]string{
				gitIgnoreFile:   "dist/\n",
				nestedModelFile: testPySource,
			},
		},
		{
			name: "runwareignore directory rule",
			files: map[string]string{
				runwareIgnoreFile: "dist/\n",
				nestedModelFile:   testPySource,
			},
		},
		{
			name: "built-in default directory rule",
			files: map[string]string{
				"node_modules/" + testModelFile: testPySource,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, tc.files)
			// The model file for the built-in case sits under node_modules.
			model := nestedModelFile
			if _, ok := tc.files[nestedModelFile]; !ok {
				model = "node_modules/" + testModelFile
			}

			encoded, modelFile, err := packDirectory(dir, model)
			if err != nil {
				t.Fatalf("packDirectory: %v", err)
			}
			if modelFile != model {
				t.Errorf("modelFile = %q, want %q", modelFile, model)
			}
			if _, ok := unpack(t, encoded)[model]; !ok {
				t.Errorf("the model file was pruned with its directory; archive = %v", names(unpack(t, encoded)))
			}
		})
	}
}

// Everything else in an excluded directory stays excluded -- descending for the
// model file must not turn the prune into a free pass for its siblings.
func TestPackDirectory_ExcludedDirectoryKeepsExcludingSiblings(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		runwareIgnoreFile:       "dist/\n",
		"dist/" + testModelFile: testPySource,
		"dist/junk.bin":         "junk",
		"dist/deep/more.bin":    "junk",
	})

	encoded, _, err := packDirectory(dir, "dist/"+testModelFile)
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}
	packed := unpack(t, encoded)
	for _, absent := range []string{"dist/junk.bin", "dist/deep/more.bin"} {
		if _, ok := packed[absent]; ok {
			t.Errorf("%q rode along with the model file; archive = %v", absent, names(packed))
		}
	}
}

// The executable bit has to survive the archive: a codebase can hold an
// entrypoint script, and one that arrives non-executable fails at run time with
// nothing about the archive to explain it.
func TestPackDirectory_PreservesFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no executable bit to preserve on Windows")
	}
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{testModelFile: testPySource, "entrypoint.sh": "#!/bin/sh\n"})
	// 0o755 is the point of the test: the executable bit is what has to survive
	// the archive, so gosec's 0600 ceiling cannot apply here.
	if err := os.Chmod(filepath.Join(dir, "entrypoint.sh"), 0o755); err != nil { //nolint:gosec
		t.Fatal(err)
	}

	encoded, _, err := packDirectory(dir, testModelFile)
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name != "entrypoint.sh" {
			continue
		}
		if f.Mode().Perm()&0o111 == 0 {
			t.Errorf("entrypoint.sh lost its executable bit: mode %v", f.Mode())
		}
		return
	}
	t.Fatal("entrypoint.sh missing from the archive")
}

// A symlink as the model file used to pass the local stat and then be skipped by
// the walk, uploading an archive whose declared entry point is absent.
func TestPackDirectory_SymlinkModelFileRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"real.py": testPySource})
	if err := os.Symlink(filepath.Join(dir, "real.py"), filepath.Join(dir, "link.py")); err != nil {
		t.Fatal(err)
	}

	_, _, err := packDirectory(dir, "link.py")
	if err == nil {
		t.Fatal("expected an error for a symlinked model file")
	}
	if !strings.Contains(err.Error(), "regular file") {
		t.Errorf("error %q does not explain the problem", err)
	}
}

// Ignore patterns are whitespace-significant, so the file's lines must reach the
// parser unmodified: a trailing space is escapable and a leading space is part of
// the pattern.
func TestPackDirectory_IgnorePatternsAreNotTrimmed(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		testModelFile:     testPySource,
		"keep me.txt":     "kept",
		"drop.txt":        "dropped",
		runwareIgnoreFile: "drop.txt\r\n",
	})

	encoded, _, err := packDirectory(dir, testModelFile)
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}
	packed := unpack(t, encoded)
	// A CRLF file must still work: only the CR is normalised.
	if _, ok := packed["drop.txt"]; ok {
		t.Errorf("a CRLF ignore line was not honoured; archive = %v", names(packed))
	}
	if _, ok := packed["keep me.txt"]; !ok {
		t.Errorf("an unrelated file with a space was dropped; archive = %v", names(packed))
	}
}
