package serverless

import (
	"bytes"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
)

// fakeUVScript stands in for uv. It records its working directory, argv and
// the uv variables that move it to another project as one log line. Then it
// does what `uv add --dev` does: it creates .venv, records the dev dependency
// in pyproject.toml once, and writes uv.lock when there is none. A non-empty
// failMessage makes it print that to stderr and exit 2 instead.
const fakeUVScript = `#!/bin/sh
echo "cwd=$(pwd -P) argv=$* overrides=${UV_PROJECT-}${UV_PROJECT_ENVIRONMENT-}${UV_WORKING_DIR-}${UV_WORKING_DIRECTORY-}" >> '%LOG%'
if [ -n '%FAIL%' ]; then
  echo '%FAIL%' >&2
  exit 2
fi
echo 'Resolved 2 packages'
/bin/mkdir -p .venv
echo 'home = /fake' > .venv/pyvenv.cfg
if ! /usr/bin/grep -q runware-serverless pyproject.toml; then
  printf '\n[dependency-groups]\ndev = [\n    "runware-serverless>=0.1.0",\n]\n' >> pyproject.toml
fi
[ -f uv.lock ] || echo 'version = 1' > uv.lock
`

// fakePythonScript stands in for a system Python and, copied into
// .venv/bin/python, for the venv's own interpreter. It records the file that
// ran, its working directory, argv and the pip variables that move an install
// elsewhere as one log line. It answers the version probe with its version,
// creates a venv by copying itself in, and pretends to pip install. A venv's
// interpreter has pip only when the venv has a pip marker file, which venv
// creation and ensurepip write. A pip install writes a library marker that
// the import check reads, unless the install goes elsewhere. A non-empty
// venv or pip failure makes that step print it and exit 1.
const fakePythonScript = `#!/bin/sh
echo "exe=$0 cwd=$(pwd -P) argv=$* pip=${PIP_USER-}${PIP_TARGET-}${PIP_PREFIX-}${PIP_ROOT-}${PIP_PYTHON-}" >> '%LOG%'
if [ "$1" = -I ]; then
  if [ -n '%NOISOLATED%' ]; then
    echo 'Unknown option: -I' >&2
    exit 2
  fi
  shift
fi
pip_marker="${0%/bin/python}/pip"
library_marker="${0%/bin/python}/runware_serverless"
case "$1 $2 $3" in
'-c '*find_spec*)
  [ -f "$library_marker" ]
  ;;
'-c '*)
  echo '%VERSION%'
  ;;
'-m venv '*)
  /bin/mkdir -p "$3/bin"
  if [ -n '%VENVFAIL%' ]; then
    echo '%VENVFAIL%' >&2
    exit 1
  fi
  echo 'home = /fake' > "$3/pyvenv.cfg"
  /bin/cp "$0" "$3/bin/python"
  : > "$3/pip"
  ;;
'-m ensurepip '*)
  : > "$pip_marker"
  echo 'Successfully installed pip'
  ;;
'-m pip --version')
  [ -f "$pip_marker" ] || exit 1
  echo 'pip 24.0'
  ;;
'-m pip install')
  if [ ! -f "$pip_marker" ]; then
    echo 'No module named pip' >&2
    exit 1
  fi
  if [ -n '%PIPFAIL%' ]; then
    echo '%PIPFAIL%' >&2
    exit 1
  fi
  [ -n '%PIPELSEWHERE%' ] || : > "$library_marker"
  echo "Successfully installed $4"
  ;;
*)
  echo "fake python: unexpected argv $*" >&2
  exit 64
  ;;
esac
`

// minimalPyproject is a customer pyproject.toml with no dependency groups.
const minimalPyproject = "[project]\nname = \"mine\"\n"

// fakePythonVersion is the version of a fake Python new enough for an App.
const fakePythonVersion = "3.12.4"

// fakePython is one fake Python interpreter on PATH.
type fakePython struct {
	name     string
	version  string
	venvFail string
	pipFail  string
	// pipElsewhere makes pip report success without installing into the
	// venv, as a pip.conf target setting does.
	pipElsewhere string
	// noIsolatedMode makes -I an unknown option, as on Python 2.
	noIsolatedMode string
}

// script is fakePythonScript for p, logging to log.
func (p fakePython) script(log string) string {
	return strings.NewReplacer(
		"%LOG%", log,
		"%VERSION%", p.version,
		"%VENVFAIL%", p.venvFail,
		"%PIPFAIL%", p.pipFail,
		"%PIPELSEWHERE%", p.pipElsewhere,
		"%NOISOLATED%", p.noIsolatedMode,
	).Replace(fakePythonScript)
}

// initHarness runs `serverless init` in-process. PATH holds only the fake
// executables the test adds, so a uv or Python on the host never answers.
type initHarness struct {
	t         *testing.T
	bin       string
	uvLog     string
	pythonLog string
	stdout    bytes.Buffer
	stderr    bytes.Buffer
}

// newBareInitHarness gives init a PATH with no toolchain on it. The tests skip
// on Windows, where the fakes cannot be shell scripts.
func newBareInitHarness(t *testing.T) *initHarness {
	t.Helper()
	if runtime.GOOS == goosWindows {
		t.Skip("the fake toolchains are shell scripts")
	}
	logs := t.TempDir()
	h := &initHarness{
		t:         t,
		bin:       t.TempDir(),
		uvLog:     filepath.Join(logs, "uv.log"),
		pythonLog: filepath.Join(logs, "python.log"),
	}
	t.Setenv("PATH", h.bin)
	return h
}

// newInitHarness puts a fake uv on PATH.
func newInitHarness(t *testing.T, failMessage string) *initHarness {
	t.Helper()
	h := newBareInitHarness(t)
	h.addUV(failMessage)
	return h
}

// newPythonInitHarness puts a fake Python 3.12 named python3 on PATH, and no uv.
func newPythonInitHarness(t *testing.T) *initHarness {
	t.Helper()
	h := newBareInitHarness(t)
	h.addPython(fakePython{
		name:    python3Command,
		version: fakePythonVersion,
	})
	return h
}

func (h *initHarness) addUV(failMessage string) {
	h.t.Helper()
	h.writeExecutable("uv", strings.NewReplacer(
		"%LOG%", h.uvLog,
		"%FAIL%", failMessage,
	).Replace(fakeUVScript))
}

func (h *initHarness) addPython(p fakePython) {
	h.t.Helper()
	h.writeExecutable(p.name, p.script(h.pythonLog))
}

func (h *initHarness) writeExecutable(name, script string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.bin, name), []byte(script), 0o700); err != nil { //nolint:gosec // test executable
		h.t.Fatal(err)
	}
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
	return h.readLog(h.uvLog)
}

// pythonCalls returns one log line per run of any fake Python, the venv's own
// interpreter included.
func (h *initHarness) pythonCalls() []string {
	h.t.Helper()
	return h.readLog(h.pythonLog)
}

func (h *initHarness) readLog(path string) []string {
	h.t.Helper()
	raw, err := os.ReadFile(path)
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

// pythonSetupCalls drops the version, pip and import probes from pythonCalls,
// which leaves the runs that create the venv and install into it.
func (h *initHarness) pythonSetupCalls() []string {
	h.t.Helper()
	return slices.DeleteFunc(h.pythonCalls(), func(call string) bool {
		return strings.Contains(call, "argv=-c ") || strings.Contains(call, "argv=-I -c ") || strings.Contains(call, " argv=-I -m pip --version ")
	})
}

// writePipLessVenv gives dir a .venv whose interpreter is p and has no pip,
// as `uv add` makes it. p.name is not used.
func (h *initHarness) writePipLessVenv(dir string, p fakePython) {
	h.t.Helper()
	bin := filepath.Join(dir, ".venv", "bin")
	if err := os.MkdirAll(bin, 0o750); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "python"), []byte(p.script(h.pythonLog)), 0o700); err != nil { //nolint:gosec // test executable
		h.t.Fatal(err)
	}
}

// ensurePipCall is the log line for the venv's own interpreter bootstrapping
// pip in dir.
func ensurePipCall(t *testing.T, dir string) string {
	t.Helper()
	return "exe=" + filepath.Join(dir, ".venv", "bin", "python") + " cwd=" + physicalPath(t, dir) +
		" argv=-I -m ensurepip pip="
}

// venvCall is the log line for `<interpreter> -m venv .venv` run in dir by the
// fake Python on PATH named interpreter.
func (h *initHarness) venvCall(interpreter, dir string) string {
	h.t.Helper()
	return "exe=" + filepath.Join(h.bin, interpreter) + " cwd=" + physicalPath(h.t, dir) + " argv=-I -m venv .venv pip="
}

// pipInstallCall is the log line for the venv's own interpreter installing the
// Serverless library in dir.
func pipInstallCall(t *testing.T, dir string) string {
	t.Helper()
	return "exe=" + filepath.Join(dir, ".venv", "bin", "python") + " cwd=" + physicalPath(t, dir) +
		" argv=-I -m pip install runware-serverless pip="
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
	if got := readInitFile(t, filepath.Join(dir, pyprojectFile)); !strings.HasPrefix(got, ownPyproject) {
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
		pyprojectFile: minimalPyproject,
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

func TestInit_NoToolchainKeepsFilesAndSaysHowToFinish(t *testing.T) {
	h := newBareInitHarness(t)
	dir := filepath.Join(t.TempDir(), "my-app")

	err := h.run(dir)
	if err == nil {
		t.Fatal("init succeeded with no toolchain on PATH")
	}
	for _, want := range []string{
		"uv was not found",
		"no Python 3.10 or newer",
		"uv add --dev runware-serverless",
		"runware serverless init " + dir,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v\nwant it to contain %q", err, want)
		}
	}
	for _, name := range []string{pyprojectFile, starterAppFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not kept: %v", name, err)
		}
	}
}

// TestInit_RerunAfterInstallingPythonFinishes: the files from a run with no
// toolchain stay, and a re-run once Python is on PATH completes the setup.
func TestInit_RerunAfterInstallingPythonFinishes(t *testing.T) {
	h := newBareInitHarness(t)
	dir := filepath.Join(t.TempDir(), "my-app")
	if err := h.run(dir); err == nil {
		t.Fatal("init succeeded with no toolchain on PATH")
	}

	h.addPython(fakePython{
		name:    python3Command,
		version: "3.13.0",
	})
	if err := h.run(dir); err != nil {
		t.Fatalf("re-run: %v\nstderr: %s", err, h.stderr.String())
	}
	want := []string{h.venvCall(python3Command, dir), pipInstallCall(t, dir)}
	if got := h.pythonSetupCalls(); !slices.Equal(got, want) {
		t.Errorf("python calls = %q, want %q", got, want)
	}
	if !strings.Contains(readInitFile(t, filepath.Join(dir, pyprojectFile)), `"runware-serverless",`) {
		t.Error("re-run did not record the dev dependency")
	}
}

// TestInit_IgnoresUVProjectOverrides: an inherited uv variable that names
// another project, directory or environment would make uv install somewhere
// other than the App directory's .venv.
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
	if want := []string{starterAppFile, pyprojectFile, "uv.lock"}; !slices.Equal(got, want) {
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

func TestInit_PythonWithoutUV(t *testing.T) {
	h := newPythonInitHarness(t)
	dir := filepath.Join(t.TempDir(), "my-app")

	if err := h.run(dir); err != nil {
		t.Fatalf("init: %v\nstderr: %s", err, h.stderr.String())
	}

	want := []string{h.venvCall(python3Command, dir), pipInstallCall(t, dir)}
	if got := h.pythonSetupCalls(); !slices.Equal(got, want) {
		t.Errorf("python calls = %q, want %q", got, want)
	}
	const wantDevGroup = "\n[dependency-groups]\ndev = [\n    \"runware-serverless\",\n]\n"
	if got := readInitFile(t, filepath.Join(dir, pyprojectFile)); !strings.HasSuffix(got, wantDevGroup) {
		t.Errorf("pyproject.toml should end with the dev group %q:\n%s", devGroup, got)
	}

	if out := h.stdout.String(); !regexp.MustCompile(`Toolchain\W+python`).MatchString(out) {
		t.Errorf("stdout should say Python was the toolchain:\n%s", out)
	}
	next := h.stderr.String()
	for _, want := range []string{
		"Python 3.12.4",
		"cd " + dir,
		"runware auth login",
		"runware serverless deploy app.py --id my-app --gpu-type <gpu>",
	} {
		if !strings.Contains(next, want) {
			t.Errorf("stderr missing %q:\n%s", want, next)
		}
	}
	if strings.Contains(h.stdout.String(), "Successfully installed") || !strings.Contains(next, "Successfully installed") {
		t.Errorf("pip output belongs on stderr, not stdout:\nstdout: %s\nstderr: %s", h.stdout.String(), next)
	}
}

func TestInit_PrefersUVOverPython(t *testing.T) {
	h := newPythonInitHarness(t)
	h.addUV("")
	dir := t.TempDir()

	if err := h.run(dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	if got, want := h.uvCalls(), []string{uvAddCall(t, dir)}; !slices.Equal(got, want) {
		t.Errorf("uv calls = %q, want %q", got, want)
	}
	if calls := h.pythonCalls(); len(calls) != 0 {
		t.Errorf("Python ran although uv is on PATH: %q", calls)
	}
}

// pythonSearchCase is the fake Pythons on PATH and the one init must pick.
type pythonSearchCase struct {
	pythons []fakePython
	want    string
}

// TestInit_PythonSearchOrder: python3 comes before python, and an interpreter
// older than 3.10 is passed over for the next one.
func TestInit_PythonSearchOrder(t *testing.T) {
	cases := map[string]pythonSearchCase{
		"python3 first": {
			pythons: []fakePython{
				{
					name:    python3Command,
					version: fakePythonVersion,
				},
				{
					name:    pythonCommand,
					version: "3.11.9",
				},
			},
			want: python3Command,
		},
		"python when there is no python3": {
			pythons: []fakePython{
				{
					name:    pythonCommand,
					version: "3.11.9",
				},
			},
			want: pythonCommand,
		},
		"python when python3 is too old": {
			pythons: []fakePython{
				{
					name:    python3Command,
					version: "3.9.18",
				},
				{
					name:    pythonCommand,
					version: "3.10.0",
				},
			},
			want: pythonCommand,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newBareInitHarness(t)
			for _, p := range tc.pythons {
				h.addPython(p)
			}
			dir := t.TempDir()

			if err := h.run(dir); err != nil {
				t.Fatalf("init: %v\nstderr: %s", err, h.stderr.String())
			}
			want := []string{h.venvCall(tc.want, dir), pipInstallCall(t, dir)}
			if got := h.pythonSetupCalls(); !slices.Equal(got, want) {
				t.Errorf("python calls = %q, want %q", got, want)
			}
		})
	}
}

func TestInit_RejectsOldPythonAndNamesItsVersion(t *testing.T) {
	h := newBareInitHarness(t)
	h.addPython(fakePython{
		name:    python3Command,
		version: "3.9.18",
	})
	dir := filepath.Join(t.TempDir(), "my-app")

	err := h.run(dir)
	if err == nil || !strings.Contains(err.Error(), "python3 is Python 3.9.18") {
		t.Fatalf("err = %v, want it to name the Python found and its version", err)
	}
	if calls := h.pythonSetupCalls(); len(calls) != 0 {
		t.Errorf("a too-old Python created or installed something: %q", calls)
	}
	if _, err := os.Stat(filepath.Join(dir, ".venv")); !os.IsNotExist(err) {
		t.Error(".venv created with a too-old Python")
	}
}

// pyprojectEditCase is a pyproject.toml before init and the text init must
// leave.
type pyprojectEditCase struct {
	pyproject string
	want      string
}

// TestInit_PythonRecordsDevDependencyAndKeepsPyproject: the library joins the
// dev group of an existing pyproject.toml, and every other line stays as the
// customer wrote it.
func TestInit_PythonRecordsDevDependencyAndKeepsPyproject(t *testing.T) {
	const project = "# my project\n[project]\nname = \"mine\"  # keep\nversion = \"2.0.0\"\n"
	cases := map[string]pyprojectEditCase{
		"no dependency-groups table": {
			pyproject: project,
			want:      project + "\n[dependency-groups]\ndev = [\n    \"runware-serverless\",\n]\n",
		},
		"no trailing newline": {
			pyproject: strings.TrimSuffix(project, "\n"),
			want:      project + "\n[dependency-groups]\ndev = [\n    \"runware-serverless\",\n]\n",
		},
		"table without a dev group": {
			pyproject: project + "\n[dependency-groups]\nlint = [\"ruff\"]\n\n[tool.ruff]\nline-length = 100\n",
			want: project + "\n[dependency-groups]\ndev = [\n    \"runware-serverless\",\n]\n" +
				"lint = [\"ruff\"]\n\n[tool.ruff]\nline-length = 100\n",
		},
		"empty dev group": {
			pyproject: project + "\n[dependency-groups]\ndev = []\n",
			want:      project + "\n[dependency-groups]\ndev = [\"runware-serverless\"]\n",
		},
		"one-line dev group": {
			pyproject: project + "\n[dependency-groups]\ndev = [\"pytest>=8\"]\n",
			want:      project + "\n[dependency-groups]\ndev = [\"runware-serverless\", \"pytest>=8\"]\n",
		},
		"multi-line dev group": {
			pyproject: project + "\n[dependency-groups]\nlint = [\n    \"ruff\",\n]\ndev = [\n    \"pytest>=8\",\n]\n",
			want: project + "\n[dependency-groups]\nlint = [\n    \"ruff\",\n]\n" +
				"dev = [\n    \"runware-serverless\",\n    \"pytest>=8\",\n]\n",
		},
		"already listed in another spelling": {
			pyproject: project + "\n[dependency-groups]\ndev = [\"Runware_Serverless>=0.1\"]\n",
			want:      project + "\n[dependency-groups]\ndev = [\"Runware_Serverless>=0.1\"]\n",
		},
		"windows line endings": {
			pyproject: "[project]\r\nname = \"mine\"\r\n\r\n[dependency-groups]\r\nlint = [\"ruff\"]\r\n",
			want:      "[project]\r\nname = \"mine\"\r\n\r\n[dependency-groups]\r\ndev = [\r\n    \"runware-serverless\",\r\n]\r\nlint = [\"ruff\"]\r\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newPythonInitHarness(t)
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{
				pyprojectFile: tc.pyproject,
			})

			if err := h.run(dir); err != nil {
				t.Fatalf("init: %v\nstderr: %s", err, h.stderr.String())
			}
			if got := readInitFile(t, filepath.Join(dir, pyprojectFile)); got != tc.want {
				t.Errorf("pyproject.toml =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// TestInit_PythonRefusesPyprojectItCannotEditSafely: init edits only a
// [dependency-groups] table header. It leaves dotted keys and inline tables
// alone, says what to add, and stops before it creates .venv or runs pip.
func TestInit_PythonRefusesPyprojectItCannotEditSafely(t *testing.T) {
	cases := map[string]string{
		"dotted keys":  "dependency-groups.dev = [\"pytest\"]\n\n[project]\nname = \"mine\"\n",
		"inline table": "dependency-groups = { dev = [\"pytest\"] }\n\n[project]\nname = \"mine\"\n",
	}
	for name, pyproject := range cases {
		t.Run(name, func(t *testing.T) {
			h := newPythonInitHarness(t)
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{
				pyprojectFile: pyproject,
			})

			err := h.run(dir)
			if err == nil || !strings.Contains(err.Error(), `Add "runware-serverless" to the dev list`) {
				t.Fatalf("err = %v, want it to say what to add by hand", err)
			}
			if got := readInitFile(t, filepath.Join(dir, pyprojectFile)); got != pyproject {
				t.Errorf("pyproject.toml changed:\n%s", got)
			}
			if calls := h.pythonSetupCalls(); len(calls) != 0 {
				t.Errorf("init set up .venv for a pyproject.toml it cannot edit: %q", calls)
			}
		})
	}
}

func TestInit_PythonRerunChangesNothing(t *testing.T) {
	h := newPythonInitHarness(t)
	dir := t.TempDir()
	if err := h.run(dir); err != nil {
		t.Fatalf("first init: %v", err)
	}
	before := snapshotAppDir(t, dir)

	if err := h.run(dir); err != nil {
		t.Fatalf("second init: %v\nstderr: %s", err, h.stderr.String())
	}
	if after := snapshotAppDir(t, dir); !maps.Equal(before, after) {
		t.Errorf("re-run changed files:\nbefore %q\nafter  %q", before, after)
	}
	want := []string{h.venvCall(python3Command, dir), pipInstallCall(t, dir), pipInstallCall(t, dir)}
	if got := h.pythonSetupCalls(); !slices.Equal(got, want) {
		t.Errorf("python calls = %q, want the venv made once and %q", got, want)
	}
}

// TestInit_UVAfterPythonKeepsOneDevEntry: an App directory set up with Python
// is still an App directory uv can finish, with no second entry.
func TestInit_UVAfterPythonKeepsOneDevEntry(t *testing.T) {
	h := newPythonInitHarness(t)
	dir := t.TempDir()
	if err := h.run(dir); err != nil {
		t.Fatalf("init with Python: %v", err)
	}

	h.addUV("")
	if err := h.run(dir); err != nil {
		t.Fatalf("init with uv: %v", err)
	}
	if got := strings.Count(readInitFile(t, filepath.Join(dir, pyprojectFile)), "runware-serverless"); got != 1 {
		t.Errorf("pyproject.toml lists runware-serverless %d times, want 1", got)
	}
}

// TestInit_PythonAddsPipToUVVenv: uv makes .venv without pip. When a later
// run has only Python, init gives that venv pip before it installs, and it
// keeps the venv.
func TestInit_PythonAddsPipToUVVenv(t *testing.T) {
	h := newPythonInitHarness(t)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		pyprojectFile: minimalPyproject,
	})
	h.writePipLessVenv(dir, fakePython{
		version: fakePythonVersion,
	})

	if err := h.run(dir); err != nil {
		t.Fatalf("init: %v\nstderr: %s", err, h.stderr.String())
	}
	want := []string{ensurePipCall(t, dir), pipInstallCall(t, dir)}
	if got := h.pythonSetupCalls(); !slices.Equal(got, want) {
		t.Errorf("python calls = %q, want %q", got, want)
	}
}

// TestInit_PythonRefusesOldVenv: an existing .venv on an old Python stays as
// it is, even with a new enough Python on PATH. The error names the version,
// also for a Python 2 that does not know -I, and says to recreate the venv.
func TestInit_PythonRefusesOldVenv(t *testing.T) {
	cases := map[string]fakePython{
		"python 3.9": {
			version: "3.9.18",
		},
		"python 2 without -I": {
			version:        "2.7.18",
			noIsolatedMode: "1",
		},
	}
	for name, venvPython := range cases {
		t.Run(name, func(t *testing.T) {
			h := newPythonInitHarness(t)
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{
				pyprojectFile: minimalPyproject,
			})
			h.writePipLessVenv(dir, venvPython)

			err := h.run(dir)
			if err == nil {
				t.Fatalf("init succeeded with a Python %s .venv", venvPython.version)
			}
			for _, want := range []string{"Python " + venvPython.version, "Remove .venv"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err = %v\nwant it to contain %q", err, want)
				}
			}
			if calls := h.pythonSetupCalls(); len(calls) != 0 {
				t.Errorf("init changed an old .venv: %q", calls)
			}
		})
	}
}

// TestInit_PipInstallOutsideVenvIsAnError: a pip configuration file can send
// an install somewhere other than .venv while pip reports success. init
// checks that the .venv interpreter can import the library, and records
// nothing when it cannot.
func TestInit_PipInstallOutsideVenvIsAnError(t *testing.T) {
	h := newBareInitHarness(t)
	h.addPython(fakePython{
		name:         python3Command,
		version:      fakePythonVersion,
		pipElsewhere: "1",
	})
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		pyprojectFile: minimalPyproject,
	})

	err := h.run(dir)
	if err == nil || !strings.Contains(err.Error(), "cannot import runware_serverless") ||
		!strings.Contains(err.Error(), "pip config list") {
		t.Fatalf("err = %v, want the failed import check and how to find the pip setting", err)
	}
	if got := readInitFile(t, filepath.Join(dir, pyprojectFile)); strings.Contains(got, "dependency-groups") {
		t.Errorf("dev dependency recorded although the library is not in .venv:\n%s", got)
	}
}

func TestInit_PipFailureIsSurfaced(t *testing.T) {
	h := newBareInitHarness(t)
	h.addPython(fakePython{
		name:    python3Command,
		version: fakePythonVersion,
		pipFail: "ERROR: No matching distribution found for runware-serverless",
	})
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		pyprojectFile: minimalPyproject,
	})

	err := h.run(dir)
	if err == nil || !strings.Contains(err.Error(), "-m pip install runware-serverless failed") {
		t.Fatalf("err = %v, want the pip failure", err)
	}
	if !strings.Contains(h.stderr.String(), "No matching distribution") {
		t.Errorf("stderr should carry pip's own error:\n%s", h.stderr.String())
	}
	if strings.Contains(h.stderr.String(), "Next steps") {
		t.Errorf("no next steps after a failure:\n%s", h.stderr.String())
	}
	if got := readInitFile(t, filepath.Join(dir, pyprojectFile)); strings.Contains(got, "dependency-groups") {
		t.Errorf("dev dependency recorded although pip failed:\n%s", got)
	}
}

// TestInit_VenvFailureRemovesHalfMadeVenv: Debian's python3 without the venv
// package fails after it starts .venv. A re-run must try the venv again rather
// than find an interpreter with no pip.
func TestInit_VenvFailureRemovesHalfMadeVenv(t *testing.T) {
	h := newBareInitHarness(t)
	h.addPython(fakePython{
		name:     python3Command,
		version:  fakePythonVersion,
		venvFail: "The virtual environment was not created successfully because ensurepip is not available.",
	})
	dir := t.TempDir()

	err := h.run(dir)
	if err == nil || !strings.Contains(err.Error(), "python3 -m venv .venv failed") {
		t.Fatalf("err = %v, want the venv failure", err)
	}
	if !strings.Contains(h.stderr.String(), "ensurepip is not available") {
		t.Errorf("stderr should carry Python's own error:\n%s", h.stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".venv")); !os.IsNotExist(err) {
		t.Error("half-made .venv left behind")
	}
}

// TestInit_IgnoresPipLocationOverrides: an inherited pip variable that sends
// installs to the user site, a prefix or a target directory would move the
// library out of the App directory's .venv.
func TestInit_IgnoresPipLocationOverrides(t *testing.T) {
	h := newPythonInitHarness(t)
	t.Setenv("PIP_USER", "1")
	t.Setenv("PIP_TARGET", t.TempDir())
	t.Setenv("PIP_PREFIX", "/usr/local")
	t.Setenv("PIP_ROOT", "/")
	t.Setenv("PIP_PYTHON", "/usr/bin/python3")
	dir := t.TempDir()

	if err := h.run(dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	want := []string{h.venvCall(python3Command, dir), pipInstallCall(t, dir)}
	if got := h.pythonSetupCalls(); !slices.Equal(got, want) {
		t.Errorf("python calls = %q, want %q", got, want)
	}
}

// TestPythonCandidates covers the Windows order, which the fake executables
// cannot run.
func TestPythonCandidates(t *testing.T) {
	cases := map[string][]string{
		"linux":     {python3Command, pythonCommand},
		"darwin":    {python3Command, pythonCommand},
		goosWindows: {python3Command, pythonCommand, "py -3"},
	}
	for goos, want := range cases {
		var got []string
		for _, c := range pythonCandidates(goos) {
			got = append(got, c.String())
		}
		if !slices.Equal(got, want) {
			t.Errorf("pythonCandidates(%s) = %q, want %q", goos, got, want)
		}
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

func TestQuoteArg(t *testing.T) {
	cases := []struct {
		arg  string
		goos string
		want string
	}{
		{arg: "demo", goos: "linux", want: "demo"},
		{arg: "my app", goos: "linux", want: "'my app'"},
		{arg: "it's", goos: "darwin", want: `'it'\''s'`},
		{arg: `C:\code\demo`, goos: goosWindows, want: `C:\code\demo`},
		{arg: `C:\My Apps\demo`, goos: goosWindows, want: `"C:\My Apps\demo"`},
	}
	for _, tc := range cases {
		if got := quoteArg(tc.arg, tc.goos); got != tc.want {
			t.Errorf("quoteArg(%q, %s) = %s, want %s", tc.arg, tc.goos, got, tc.want)
		}
	}
}
