package serverless

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	toolchainPython = "python"
	python3Command  = "python3"
	pythonCommand   = "python"
	venvDir         = ".venv"
	devGroup        = "dev"
	dependencyGroup = "dependency-groups"

	// minPythonMinor is the lowest Python 3 minor version an App supports.
	minPythonMinor = 10

	// pythonVersionProbe prints the interpreter's version. It runs on Python 2
	// as well, so an old python on PATH reports its version instead of failing.
	pythonVersionProbe = `import sys; sys.stdout.write("%d.%d.%d" % sys.version_info[:3])`

	// pythonIsolated is Python's isolated mode: no working directory, user
	// site or PYTHON* variables on the import path. The version probe runs
	// without it, because Python 2 does not know -I.
	pythonIsolated = "-I"

	// libraryImportName is the module the Serverless library installs.
	libraryImportName = "runware_serverless"

	// libraryImportCheck exits non-zero when the interpreter cannot find the
	// Serverless library. find_spec looks without running the library.
	libraryImportCheck = `import importlib.util, sys; sys.exit(importlib.util.find_spec("` + libraryImportName + `") is None)`
)

// minPythonVersion is minPythonMinor as the requires-python that init writes
// and its errors spell it.
var minPythonVersion = "3." + strconv.Itoa(minPythonMinor)

// pipLocationOverrides are the pip variables that send an install somewhere
// other than the environment of the interpreter that runs pip. PIP_PYTHON is
// pip's --python, which re-runs pip under another interpreter.
var pipLocationOverrides = map[string]struct{}{
	"PIP_PREFIX": {},
	"PIP_PYTHON": {},
	"PIP_ROOT":   {},
	"PIP_TARGET": {},
	"PIP_USER":   {},
}

var (
	pythonVersionPattern = regexp.MustCompile(`^(\d+)\.(\d+)`)
	requirementName      = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9._-]*)`)
	nameSeparators       = regexp.MustCompile(`[-_.]+`)
	dependencyGroupsHead = regexp.MustCompile(`(?m)^[ \t]*\[[ \t]*dependency-groups[ \t]*\][ \t]*(#[^\n]*)?\r?$`)
	tableHead            = regexp.MustCompile(`(?m)^[ \t]*\[`)
	devKey               = regexp.MustCompile(`(?m)^[ \t]*(dev|"dev"|'dev')[ \t]*=[ \t]*\[`)
)

// pythonCandidate is one command that can start a Python interpreter.
type pythonCandidate struct {
	name string
	args []string
}

func (c pythonCandidate) String() string {
	return strings.Join(append([]string{c.name}, c.args...), " ")
}

// pythonCandidates lists the commands init tries, in order, when uv is not on
// PATH. Windows also has the py launcher.
func pythonCandidates(goos string) []pythonCandidate {
	candidates := []pythonCandidate{
		{
			name: python3Command,
		},
		{
			name: pythonCommand,
		},
	}
	if goos == goosWindows {
		candidates = append(candidates, pythonCandidate{
			name: "py",
			args: []string{"-3"},
		})
	}
	return candidates
}

// pythonInterpreter is a Python on PATH that is new enough for an App.
type pythonInterpreter struct {
	candidate pythonCandidate
	path      string
	version   string
}

func (p pythonInterpreter) command(ctx context.Context, args ...string) *exec.Cmd {
	//nolint:gosec // the path is a fixed candidate name resolved on PATH
	return exec.CommandContext(ctx, p.path, append(slices.Clone(p.candidate.args), args...)...)
}

// findPython returns the first candidate on PATH that is Python 3.10 or newer.
// When there is none, it returns false and a description of each older Python
// it found, so the error can say why it did not count.
func findPython(ctx context.Context, goos string) (pythonInterpreter, []string, bool) {
	var tooOld []string
	for _, candidate := range pythonCandidates(goos) {
		path, err := exec.LookPath(candidate.name)
		if err != nil {
			continue
		}
		p := pythonInterpreter{
			candidate: candidate,
			path:      path,
		}
		// A command that is not a working Python, such as the Windows store
		// stub, fails the probe and counts as not found.
		p.version, err = pythonVersion(p.command(ctx, "-c", pythonVersionProbe))
		if err != nil {
			continue
		}
		if !supportedPython(p.version) {
			tooOld = append(tooOld, fmt.Sprintf("%s is Python %s", candidate, p.version))
			continue
		}
		return p, tooOld, true
	}
	return pythonInterpreter{}, tooOld, false
}

// pythonVersion runs probe, a Python command that runs pythonVersionProbe,
// and returns the version it prints.
func pythonVersion(probe *exec.Cmd) (string, error) {
	out, err := probe.Output()
	return strings.TrimSpace(string(out)), err
}

func supportedPython(version string) bool {
	m := pythonVersionPattern.FindStringSubmatch(version)
	if m == nil {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major > 3 || (major == 3 && minor >= minPythonMinor)
}

// venvPython is the interpreter inside the App directory's .venv.
func venvPython(dir, goos string) string {
	if goos == goosWindows {
		return filepath.Join(dir, venvDir, "Scripts", "python.exe")
	}
	return filepath.Join(dir, venvDir, "bin", "python")
}

// pythonAddLibrary creates the App directory's .venv when it has no
// interpreter, installs the Serverless library with that interpreter, and
// records the library as a dev dependency in pyproject.toml. The system
// Python only creates the venv: a venv is not externally managed, so PEP 668
// does not block the install. Python's output goes to w, which is stderr.
func pythonAddLibrary(ctx context.Context, python pythonInterpreter, dir, goos string, w io.Writer) error {
	// A pyproject.toml init cannot edit stops it before it touches .venv.
	pyproject := filepath.Join(dir, pyprojectFile)
	updated, changed, err := devDependencyEdit(pyproject)
	if err != nil {
		return err
	}

	// venv and ensurepip bootstrap pip with pip itself, so every step drops
	// the overrides.
	env := slices.DeleteFunc(os.Environ(), isPipLocationOverride)
	interpreter := venvPython(dir, goos)
	if _, err := os.Stat(interpreter); err != nil {
		if err := createVenv(ctx, python, dir, env, w); err != nil {
			return err
		}
	} else {
		if err := checkVenvPython(ctx, interpreter); err != nil {
			return err
		}
		if err := ensureVenvPip(ctx, interpreter, dir, env, w); err != nil {
			return err
		}
	}

	if err := runIn(venvCommand(ctx, interpreter, pipInstallArgs...), dir, env, w); err != nil {
		return fmt.Errorf("%s %s failed in %s: %w (pip's output is above)", interpreter, strings.Join(pipInstallArgs, " "), dir, err)
	}
	// Settings in pip's configuration files, such as target or user, can send
	// the install out of .venv while pip reports success.
	if runIn(venvCommand(ctx, interpreter, "-c", libraryImportCheck), dir, env, io.Discard) != nil {
		return fmt.Errorf(
			"pip reported success, but %s cannot import %s, so the library is not in %s. A pip configuration setting such as target or user can cause this: find it with '%s -m pip config list', remove it, and run init again",
			interpreter, libraryImportName, venvDir, interpreter,
		)
	}

	if !changed {
		return nil
	}
	return replaceFile(pyproject, updated)
}

// venvCommand runs the .venv interpreter in isolated mode. The tools run with
// the App directory as their working directory, and -I keeps the customer's
// own pip.py or venv.py there, and PYTHONPATH, from shadowing them.
func venvCommand(ctx context.Context, interpreter string, args ...string) *exec.Cmd {
	//nolint:gosec // the interpreter of the App directory's own .venv
	return exec.CommandContext(ctx, interpreter, append([]string{pythonIsolated}, args...)...)
}

func createVenv(ctx context.Context, python pythonInterpreter, dir string, env []string, w io.Writer) error {
	venv := filepath.Join(dir, venvDir)
	_, statErr := os.Stat(venv)
	venvExisted := statErr == nil
	if err := runIn(python.command(ctx, pythonIsolated, "-m", "venv", venvDir), dir, env, w); err != nil {
		// Debian's python3 without its venv package leaves an interpreter
		// with no pip and no ensurepip. A re-run must create the venv again
		// rather than try to repair that one.
		if !venvExisted {
			_ = os.RemoveAll(venv)
		}
		return fmt.Errorf("%s -m venv %s failed in %s: %w (Python's output is above)", python.candidate, venvDir, dir, err)
	}
	return nil
}

// checkVenvPython refuses an existing .venv whose Python is older than an App
// supports. The Python on PATH can be new enough while the .venv is not.
func checkVenvPython(ctx context.Context, interpreter string) error {
	// Without -I, like the probe in findPython, so an old Python can answer.
	//nolint:gosec // the interpreter of the App directory's own .venv
	version, err := pythonVersion(exec.CommandContext(ctx, interpreter, "-c", pythonVersionProbe))
	if err != nil {
		return fmt.Errorf("init could not run %s: %w. Remove %s and run init again", interpreter, err, venvDir)
	}
	if !supportedPython(version) {
		return fmt.Errorf(
			"%s uses Python %s, and an App needs Python %s or newer. Remove %s and run init again",
			venvDir, version, minPythonVersion, venvDir,
		)
	}
	return nil
}

// ensureVenvPip gives an existing .venv pip when it has none: uv makes its
// venvs without pip.
func ensureVenvPip(ctx context.Context, interpreter, dir string, env []string, w io.Writer) error {
	if runIn(venvCommand(ctx, interpreter, "-m", "pip", "--version"), dir, env, io.Discard) == nil {
		return nil
	}
	if err := runIn(venvCommand(ctx, interpreter, "-m", "ensurepip"), dir, env, w); err != nil {
		return fmt.Errorf(
			"%s in %s has no pip, and %s -m ensurepip failed: %w (Python's output is above). Remove %s and run init again",
			venvDir, dir, interpreter, err, venvDir,
		)
	}
	return nil
}

// pipInstallArgs installs the Serverless library into the running interpreter's
// environment.
var pipInstallArgs = []string{
	"-m",
	"pip",
	"install",
	serverlessLibrary,
}

// isPipLocationOverride compares names case-insensitively, as Windows does.
func isPipLocationOverride(kv string) bool {
	key, _, _ := strings.Cut(kv, "=")
	_, ok := pipLocationOverrides[strings.ToUpper(key)]
	return ok
}

// devDependencyEdit returns the pyproject.toml at path with the Serverless
// library under [dependency-groups] dev, as `uv add --dev` records it, so uv
// and the editor see the same project. It also returns whether that differs
// from the file.
func devDependencyEdit(path string) (string, bool, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the App directory's own pyproject.toml
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", pyprojectFile, err)
	}
	updated, changed, err := withDevDependency(string(raw))
	if err != nil {
		return "", false, fmt.Errorf(
			"init cannot record the Serverless library in %s: %w. Add %q to the dev list under [dependency-groups] yourself, then run init again",
			pyprojectFile, err, serverlessLibrary,
		)
	}
	return updated, changed, nil
}

// replaceFile writes content next to path and renames it into place, so a
// failed write never leaves the customer's file truncated.
func replaceFile(path, content string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	_, err = tmp.WriteString(content)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), info.Mode().Perm())
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

// withDevDependency returns pyproject with the Serverless library added to
// [dependency-groups] dev, and whether it changed. It edits the text, so the
// customer's comments and layout stay, and then parses the result to prove
// the edit added the library and changed nothing else.
func withDevDependency(pyproject string) (string, bool, error) {
	before := map[string]any{}
	if err := toml.Unmarshal([]byte(pyproject), &before); err != nil {
		return "", false, fmt.Errorf("%s is not valid TOML: %w", pyprojectFile, err)
	}
	if listsServerlessLibrary(before) {
		return pyproject, false, nil
	}

	updated, err := insertDevDependency(pyproject, before)
	if err != nil {
		return "", false, err
	}

	after := map[string]any{}
	if err := toml.Unmarshal([]byte(updated), &after); err != nil ||
		!reflect.DeepEqual(after, addServerlessLibrary(before)) {
		return "", false, errors.New("its [dependency-groups] layout is not one init can edit safely")
	}
	return updated, true, nil
}

// addServerlessLibrary changes doc into the parsed document that
// insertDevDependency should produce: the library first in the dev group, and
// nothing else changed. It returns doc.
func addServerlessLibrary(doc map[string]any) map[string]any {
	groups, ok := doc[dependencyGroup].(map[string]any)
	if !ok {
		groups = map[string]any{}
		doc[dependencyGroup] = groups
	}
	groups[devGroup] = append([]any{serverlessLibrary}, devDependencies(doc)...)
	return doc
}

func insertDevDependency(pyproject string, doc map[string]any) (string, error) {
	newline := "\n"
	if strings.Contains(pyproject, "\r\n") {
		newline = "\r\n"
	}
	entry := strconv.Quote(serverlessLibrary)
	devList := "dev = [" + newline + "    " + entry + "," + newline + "]" + newline

	head := dependencyGroupsHead.FindStringIndex(pyproject)
	if head == nil {
		if _, ok := doc[dependencyGroup]; ok {
			return "", errors.New("it defines dependency-groups without a [dependency-groups] table")
		}
		if pyproject != "" && !strings.HasSuffix(pyproject, "\n") {
			pyproject += newline
		}
		return pyproject + newline + "[dependency-groups]" + newline + devList, nil
	}

	// The header match stops before its line's "\n".
	bodyStart := head[1] + 1
	if bodyStart > len(pyproject) {
		return pyproject + newline + devList, nil
	}
	bodyEnd := len(pyproject)
	if next := tableHead.FindStringIndex(pyproject[bodyStart:]); next != nil {
		bodyEnd = bodyStart + next[0]
	}
	key := devKey.FindStringIndex(pyproject[bodyStart:bodyEnd])
	if key == nil {
		return pyproject[:bodyStart] + devList + pyproject[bodyStart:], nil
	}

	// Put the library first in the existing list, matching the list's style.
	open := bodyStart + key[1]
	rest := strings.TrimLeft(pyproject[open:], " \t")
	switch {
	case strings.HasPrefix(rest, "]"):
		return pyproject[:open] + entry + pyproject[open:], nil
	case strings.HasPrefix(rest, "\n"), strings.HasPrefix(rest, "\r\n"):
		return pyproject[:open] + newline + "    " + entry + "," + pyproject[open:], nil
	default:
		return pyproject[:open] + entry + ", " + pyproject[open:], nil
	}
}

// listsServerlessLibrary reports whether the dev group already requires the
// Serverless library, in any spelling of its name.
func listsServerlessLibrary(doc map[string]any) bool {
	return slices.ContainsFunc(devDependencies(doc), isServerlessRequirement)
}

func devDependencies(doc map[string]any) []any {
	groups, _ := doc[dependencyGroup].(map[string]any)
	dev, _ := groups[devGroup].([]any)
	return dev
}

func isServerlessRequirement(entry any) bool {
	requirement, ok := entry.(string)
	if !ok {
		return false
	}
	m := requirementName.FindStringSubmatch(requirement)
	return len(m) > 1 && normalizedName(m[1]) == serverlessLibrary
}

// normalizedName is the PEP 503 form of a Python package name.
func normalizedName(name string) string {
	return nameSeparators.ReplaceAllString(strings.ToLower(name), "-")
}
