package serverless

import (
	"bytes"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
)

const goosWindows = "windows"

// fakeUVScript stands in for uv. It records its working directory, argv and
// the uv variables that move it to another project as one log line, and
// creates .venv as `uv add` does. A non-empty failMessage makes it print that
// to stderr and exit 2 instead.
const fakeUVScript = `#!/bin/sh
echo "cwd=$(pwd -P) argv=$* overrides=${UV_PROJECT-}${UV_PROJECT_ENVIRONMENT-}${UV_WORKING_DIR-}${UV_WORKING_DIRECTORY-}" >> '%LOG%'
if [ -n '%FAIL%' ]; then
  echo '%FAIL%' >&2
  exit 2
fi
echo 'Resolved 2 packages'
/bin/mkdir -p .venv
echo 'home = /fake' > .venv/pyvenv.cfg
`

// initHarness runs `serverless init` in-process with a fake uv first on PATH.
type initHarness struct {
	t      *testing.T
	uvLog  string
	stdout bytes.Buffer
	stderr bytes.Buffer
}

// newInitHarness puts a fake uv first on PATH. The tests skip on Windows,
// where the fake cannot be a shell script.
func newInitHarness(t *testing.T, failMessage string) *initHarness {
	t.Helper()
	if runtime.GOOS == goosWindows {
		t.Skip("the fake uv is a shell script")
	}
	bin := t.TempDir()
	h := &initHarness{
		t:     t,
		uvLog: filepath.Join(t.TempDir(), "uv.log"),
	}
	script := strings.NewReplacer("%LOG%", h.uvLog, "%FAIL%", failMessage).Replace(fakeUVScript)
	if err := os.WriteFile(filepath.Join(bin, "uv"), []byte(script), 0o700); err != nil { //nolint:gosec // test executable
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return h
}

func (h *initHarness) run(args ...string) error {
	h.t.Helper()
	h.stdout.Reset()
	h.stderr.Reset()
	cmd := NewCmd(log.New(io.Discard))
	cmd.SetOut(&h.stdout)
	cmd.SetErr(&h.stderr)
	cmd.SetArgs(append([]string{"init"}, args...))
	return cmd.Execute()
}

// uvCalls returns one log line per uv invocation.
func (h *initHarness) uvCalls() []string {
	h.t.Helper()
	raw, err := os.ReadFile(h.uvLog)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// uvAddCall is the log line for `uv add --dev runware-serverless` run in dir.
func uvAddCall(t *testing.T, dir string) string {
	t.Helper()
	return "cwd=" + physicalPath(t, dir) + " argv=add --dev runware-serverless overrides="
}

func readInitFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func physicalPath(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// snapshotAppDir reads every file under dir except .venv, which the fake uv
// rewrites on each run.
func snapshotAppDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".venv" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = readInitFile(t, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInit_MissingDirectoryWithUV(t *testing.T) {
	h := newInitHarness(t, "")
	dir := filepath.Join(t.TempDir(), "my-app")

	if err := h.run(dir); err != nil {
		t.Fatalf("init: %v\nstderr: %s", err, h.stderr.String())
	}

	pyproject := readInitFile(t, filepath.Join(dir, pyprojectFile))
	for _, want := range []string{`name = "my-app"`, `requires-python = ">=3.10"`, `dependencies = []`} {
		if !strings.Contains(pyproject, want) {
			t.Errorf("pyproject.toml missing %q:\n%s", want, pyproject)
		}
	}

	if got, want := h.uvCalls(), []string{uvAddCall(t, dir)}; !slices.Equal(got, want) {
		t.Errorf("uv calls = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, ".venv")); err != nil {
		t.Errorf(".venv: %v", err)
	}

	if out := h.stdout.String(); !strings.Contains(out, dir) || !strings.Contains(out, "uv") {
		t.Errorf("stdout should name the App directory and uv:\n%s", out)
	}
	next := h.stderr.String()
	for _, want := range []string{
		"cd " + dir,
		"runware auth login",
		"runware serverless deploy app.py --id my-app --gpu-type <gpu>",
	} {
		if !strings.Contains(next, want) {
			t.Errorf("stderr missing next step %q:\n%s", want, next)
		}
	}
	if strings.Contains(h.stdout.String(), "Resolved 2 packages") || !strings.Contains(next, "Resolved 2 packages") {
		t.Errorf("uv output belongs on stderr, not stdout:\nstdout: %s\nstderr: %s", h.stdout.String(), next)
	}
}

func TestInit_StarterApp(t *testing.T) {
	h := newInitHarness(t, "")
	dir := t.TempDir()

	if err := h.run(dir); err != nil {
		t.Fatalf("init: %v", err)
	}

	app := readInitFile(t, filepath.Join(dir, starterAppFile))
	for _, want := range []string{"@serve\nclass ", "    def load(self)", "    @endpoint\n    def "} {
		if !strings.Contains(app, want) {
			t.Errorf("app.py missing %q:\n%s", want, app)
		}
	}
	for line := range strings.SplitSeq(app, "\n") {
		trimmed := strings.TrimSpace(line)
		if (strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "from ")) &&
			!strings.HasPrefix(trimmed, "from runware_serverless import ") {
			t.Errorf("app.py imports beyond runware_serverless: %q", line)
		}
	}
}

func TestInit_DefaultsToWorkingDirectory(t *testing.T) {
	h := newInitHarness(t, "")
	dir := filepath.Join(t.TempDir(), "hello-world")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if err := h.run(); err != nil {
		t.Fatalf("init: %v\nstderr: %s", err, h.stderr.String())
	}

	if !strings.Contains(readInitFile(t, filepath.Join(dir, pyprojectFile)), `name = "hello-world"`) {
		t.Error("pyproject.toml not written to the working directory")
	}
	if got, want := h.uvCalls(), []string{uvAddCall(t, dir)}; !slices.Equal(got, want) {
		t.Errorf("uv calls = %q, want %q", got, want)
	}
	if strings.Contains(h.stderr.String(), "  cd ") {
		t.Errorf("no cd step when the App directory is the working directory:\n%s", h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "--id hello-world") {
		t.Errorf("deploy step should suggest the directory name as --id:\n%s", h.stderr.String())
	}
}

func TestInit_DotPrintsNoCdStep(t *testing.T) {
	h := newInitHarness(t, "")
	t.Chdir(t.TempDir())

	if err := h.run("."); err != nil {
		t.Fatalf("init .: %v", err)
	}
	if strings.Contains(h.stderr.String(), "  cd ") {
		t.Errorf("no cd step when the App directory is the working directory:\n%s", h.stderr.String())
	}
}

func TestInit_RefusesNonEmptyDirectoryWithoutPyproject(t *testing.T) {
	h := newInitHarness(t, "")
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"notes.txt": "mine"})

	err := h.run(dir)
	if err == nil || !strings.Contains(err.Error(), "not empty") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want a refusal that names --force", err)
	}
	for _, name := range []string{pyprojectFile, starterAppFile, ".venv"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s written into a refused directory", name)
		}
	}
	if calls := h.uvCalls(); len(calls) != 0 {
		t.Errorf("uv ran on a refused directory: %q", calls)
	}
}

func TestInit_ForceUsesNonEmptyDirectory(t *testing.T) {
	h := newInitHarness(t, "")
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"notes.txt": "mine"})

	if err := h.run("--force", dir); err != nil {
		t.Fatalf("init --force: %v", err)
	}
	for _, name := range []string{pyprojectFile, starterAppFile, ".venv"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := readInitFile(t, filepath.Join(dir, "notes.txt")); got != "mine" {
		t.Errorf("notes.txt = %q, want it untouched", got)
	}
}

func TestInit_KeepsExistingPyprojectAndRerunChangesNothing(t *testing.T) {
	h := newInitHarness(t, "")
	dir := t.TempDir()
	const ownPyproject = "[project]\nname = \"mine\"\nversion = \"2.0.0\"\n"
	writeTree(t, dir, map[string]string{pyprojectFile: ownPyproject})

	if err := h.run(dir); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if got := readInitFile(t, filepath.Join(dir, pyprojectFile)); got != ownPyproject {
		t.Errorf("pyproject.toml overwritten:\n%s", got)
	}
	before := snapshotAppDir(t, dir)

	if err := h.run(dir); err != nil {
		t.Fatalf("second init: %v\nstderr: %s", err, h.stderr.String())
	}
	if after := snapshotAppDir(t, dir); !maps.Equal(before, after) {
		t.Errorf("re-run changed files:\nbefore %q\nafter  %q", before, after)
	}
	if !strings.Contains(h.stdout.String(), "nothing") {
		t.Errorf("re-run should report nothing created:\n%s", h.stdout.String())
	}
}

func TestInit_NoStarterAppWhenPythonFilesExist(t *testing.T) {
	h := newInitHarness(t, "")
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		pyprojectFile: "[project]\nname = \"mine\"\n",
		"model.py":    testPySource,
	})

	if err := h.run(dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, starterAppFile)); !os.IsNotExist(err) {
		t.Error("app.py written next to the customer's own .py files")
	}
	if !strings.Contains(h.stderr.String(), "deploy <entry-file>.py") {
		t.Errorf("deploy step should not name a starter app.py that does not exist:\n%s", h.stderr.String())
	}
}

func TestInit_UVFailureIsSurfaced(t *testing.T) {
	h := newInitHarness(t, "error: Failed to fetch: https://pypi.org/simple/runware-serverless/")
	dir := t.TempDir()

	err := h.run(dir)
	if err == nil || !strings.Contains(err.Error(), "uv add --dev runware-serverless failed") {
		t.Fatalf("err = %v, want the uv failure", err)
	}
	if !strings.Contains(h.stderr.String(), "Failed to fetch") {
		t.Errorf("stderr should carry uv's own error:\n%s", h.stderr.String())
	}
	if strings.Contains(h.stderr.String(), "Next steps") {
		t.Errorf("no next steps after a failure:\n%s", h.stderr.String())
	}
}

func TestInit_NoUVKeepsFilesAndSaysHowToFinish(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("PATH lookup differs on Windows")
	}
	t.Setenv("PATH", t.TempDir())
	h := &initHarness{t: t}
	dir := filepath.Join(t.TempDir(), "my-app")

	err := h.run(dir)
	if err == nil || !strings.Contains(err.Error(), "uv was not found") ||
		!strings.Contains(err.Error(), "uv add --dev runware-serverless") {
		t.Fatalf("err = %v, want the missing-uv message and the command to finish", err)
	}
	for _, name := range []string{pyprojectFile, starterAppFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not kept: %v", name, err)
		}
	}
}

// TestInit_IgnoresUVProjectOverrides: an inherited uv variable that names
// another project, directory or environment would make uv install somewhere
// other than the App directory's .venv (ADR 0001).
func TestInit_IgnoresUVProjectOverrides(t *testing.T) {
	h := newInitHarness(t, "")
	other := t.TempDir()
	t.Setenv("UV_PROJECT", other)
	t.Setenv("UV_PROJECT_ENVIRONMENT", "/usr/local")
	t.Setenv("UV_WORKING_DIR", other)
	t.Setenv("UV_WORKING_DIRECTORY", other)
	dir := t.TempDir()

	if err := h.run(dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	if got, want := h.uvCalls(), []string{uvAddCall(t, dir)}; !slices.Equal(got, want) {
		t.Errorf("uv calls = %q, want %q", got, want)
	}
}

func TestInit_RefusesFile(t *testing.T) {
	h := newInitHarness(t, "")
	path := filepath.Join(t.TempDir(), "not-a-dir")
	writeTree(t, filepath.Dir(path), map[string]string{"not-a-dir": ""})

	if err := h.run(path); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("err = %v, want not a directory", err)
	}
}

func TestInit_DeployArchiveExcludesVenv(t *testing.T) {
	h := newInitHarness(t, "")
	dir := t.TempDir()
	if err := h.run(dir); err != nil {
		t.Fatalf("init: %v", err)
	}

	raw, _, err := packDirectory(dir, starterAppFile)
	if err != nil {
		t.Fatalf("packDirectory: %v", err)
	}
	got := names(unpack(t, raw))
	if want := []string{starterAppFile, pyprojectFile}; !slices.Equal(got, want) {
		t.Errorf("archive = %q, want %q", got, want)
	}
}

// TestInit_ShortNameLeavesAppIDPlaceholder: an App ID needs 6 to 30
// characters, so a shorter directory name is not suggested as --id.
func TestInit_ShortNameLeavesAppIDPlaceholder(t *testing.T) {
	h := newInitHarness(t, "")
	dir := filepath.Join(t.TempDir(), "demo")

	if err := h.run(dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	if !strings.Contains(h.stderr.String(), "--id <app-id> ") {
		t.Errorf("deploy step should leave --id as a placeholder:\n%s", h.stderr.String())
	}
}

func TestProjectName(t *testing.T) {
	cases := map[string]string{
		"/x/my-app":     "my-app",
		"/x/My App":     "my-app",
		"/x/__init__":   "init",
		"/x/Foo_Bar.v2": "foo-bar-v2",
		"/x/---":        defaultProjectName,
	}
	for dir, want := range cases {
		if got := projectName(dir); got != want {
			t.Errorf("projectName(%q) = %q, want %q", dir, got, want)
		}
	}
}

func TestIsUVProjectOverride(t *testing.T) {
	cases := map[string]bool{
		"UV_PROJECT=/x":            true,
		"uv_project_environment=x": true,
		"Uv_Working_Dir=/x":        true,
		"UV_WORKING_DIRECTORY=/x":  true,
		"UV_PROJECTS=/x":           false,
		"UV_CACHE_DIR=/x":          false,
		"PATH=/bin":                false,
	}
	for kv, want := range cases {
		if got := isUVProjectOverride(kv); got != want {
			t.Errorf("isUVProjectOverride(%q) = %v, want %v", kv, got, want)
		}
	}
}
