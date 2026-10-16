package serverless

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	serverlessLibrary = "runware-serverless"
	pyprojectFile     = "pyproject.toml"
	starterAppFile    = "app.py"
	toolchainUV       = "uv"
	goosWindows       = "windows"

	// defaultProjectName names a project whose directory name has no letters or
	// digits to derive one from.
	defaultProjectName = "app"
)

// uvProjectOverrides are the uv variables that point uv away from the App directory.
var uvProjectOverrides = map[string]struct{}{
	"UV_PROJECT":             {},
	"UV_PROJECT_ENVIRONMENT": {},
	"UV_WORKING_DIR":         {},
	"UV_WORKING_DIRECTORY":   {},
}

// appIDPattern is the API's AppId rule. A directory name that does not fit
// leaves a placeholder in the printed deploy command rather than an ID the
// create would reject.
var appIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)

// posixSafePattern and windowsSafePattern match arguments that need no quoting
// in the printed next steps, in a POSIX shell and in PowerShell or cmd.
var (
	posixSafePattern   = regexp.MustCompile(`^[A-Za-z0-9_./~+,:@%=-]+$`)
	windowsSafePattern = regexp.MustCompile(`^[A-Za-z0-9_.\\/:~+,@=-]+$`)
)

const starterApp = `from runware_serverless import endpoint, serve


@serve
class App:
    def load(self) -> None:
        # Load models and other heavy state here. It runs before the first
        # request, and it can run more than once, so keep it safe to repeat.
        pass

    @endpoint
    def hello(self, name: str = "world") -> dict:
        return {"message": f"Hello, {name}!"}
`

// initResult is the App directory init prepared. Created lists only what this
// run wrote, so a re-run on a ready App directory reports nothing.
type initResult struct {
	AppDirectory string   `json:"appDirectory" yaml:"appDirectory"`
	Toolchain    string   `json:"toolchain"    yaml:"toolchain"`
	Created      []string `json:"created"      yaml:"created"`
}

func (r initResult) Headers() []string {
	return []string{colField, colValue}
}

func (r initResult) Rows() [][]any {
	created := "nothing"
	if len(r.Created) > 0 {
		created = strings.Join(r.Created, ", ")
	}
	return [][]any{
		{"App directory", r.AppDirectory},
		{"Toolchain", r.Toolchain},
		{"Created", created},
	}
}

func newInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Create an App directory with the Serverless library installed",
		Long: `Create an App directory: the folder that holds one App, its pyproject.toml and
its own .venv. dir defaults to the current directory, and is created when it
does not exist.

init writes what is missing and nothing else:

  pyproject.toml  a minimal project named after the directory, requiring
                  Python 3.10 or newer, with no runtime dependencies
  app.py          a starter App with @serve, a load method and one @endpoint,
                  written only when the directory holds no .py files
  .venv           the directory's own environment, with runware-serverless
                  added as a dev dependency

The toolchain is uv when it is on PATH. init runs
'uv add --dev runware-serverless' inside the App directory, which creates .venv
and uv.lock and records the dependency in pyproject.toml.

Without uv, init uses the first Python 3.10 or newer it finds: python3, then
python, and on Windows also 'py -3'. That Python creates .venv with
'-m venv .venv'. Then the interpreter inside .venv runs
'-m pip install runware-serverless', and init adds runware-serverless to the
dev list under [dependency-groups] in pyproject.toml. An older Python does not
count, and the error names its version.

With neither uv nor a suitable Python, init keeps the files it wrote, prints
the command that finishes the setup, and exits non-zero. Run init again after
you install either toolchain.

init never installs into a global, user or tool environment, so PEP 668 does
not apply.

A directory that already has a pyproject.toml is an App directory: init keeps
that file and adds only what is missing, so running it again is safe. A
non-empty directory without a pyproject.toml is refused unless you pass --force.

The Serverless library is an editor aid. A deploy strips it, supplies the
platform's own copy, and does not upload .venv.`,
		Example: `  # create ./my-app and install the Serverless library into its .venv
  runware serverless init my-app

  # make the current directory an App directory
  runware serverless init

  # use a directory that already holds files but no pyproject.toml
  runware serverless init --force ./existing-code`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var dirArg string
			if len(args) == 1 {
				dirArg = args[0]
			}
			return runInit(cmd, dirArg, force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Use a non-empty directory that has no pyproject.toml")

	return cmd
}

func runInit(cmd *cobra.Command, dirArg string, force bool) error {
	dir, err := filepath.Abs(dirArg)
	if err != nil {
		return fmt.Errorf("resolve App directory: %w", err)
	}

	created, err := prepareAppDirectory(dir, force)
	if err != nil {
		return err
	}

	toolchain, err := addLibrary(cmd.Context(), dir, cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	if err := output.PrintTo(cmd.OutOrStdout(), cmdutil.FormatFor(cmd), initResult{
		AppDirectory: dir,
		Toolchain:    toolchain,
		Created:      created,
	}); err != nil {
		return err
	}
	reportInitNextSteps(cmd.ErrOrStderr(), dir, cdTarget(dir, dirArg))
	return nil
}

// cdTarget is the directory argument to print in the next steps, or "" when
// the App directory is already the working directory.
func cdTarget(dir, dirArg string) string {
	if wd, err := os.Getwd(); err == nil && wd == dir {
		return ""
	}
	return dirArg
}

// prepareAppDirectory creates dir when it is missing and writes the
// pyproject.toml and starter App it lacks. It returns the files it wrote.
func prepareAppDirectory(dir string, force bool) ([]string, error) {
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create App directory: %w", err)
		}
	case err != nil:
		return nil, fmt.Errorf("read App directory: %w", err)
	case !info.IsDir():
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read App directory: %w", err)
	}
	isAppDir := slices.ContainsFunc(entries, func(e os.DirEntry) bool {
		return e.Name() == pyprojectFile
	})
	if !isAppDir && len(entries) > 0 && !force {
		return nil, fmt.Errorf(
			"%s is not empty and has no %s, so it is not an App directory. Choose a new or empty directory, or pass --force to use this one",
			dir, pyprojectFile,
		)
	}

	created := []string{}
	if !isAppDir {
		if err := writeNewFile(filepath.Join(dir, pyprojectFile), pyprojectFor(dir)); err != nil {
			return nil, err
		}
		created = append(created, pyprojectFile)
	}
	hasPython := slices.ContainsFunc(entries, func(e os.DirEntry) bool {
		return !e.IsDir() && filepath.Ext(e.Name()) == ".py"
	})
	if !hasPython {
		if err := writeNewFile(filepath.Join(dir, starterAppFile), starterApp); err != nil {
			return nil, err
		}
		created = append(created, starterAppFile)
	}
	return created, nil
}

// writeNewFile refuses to replace a file, so a race with another writer can
// never cost the customer their own copy. It removes what it created when the
// write fails: a truncated file left behind would stay for good, because a
// re-run keeps an existing pyproject.toml and writes no app.py next to another
// .py file.
func writeNewFile(path, content string) error {
	//nolint:gosec // customer source files, readable like any other in the project
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	_, err = f.WriteString(content)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func pyprojectFor(dir string) string {
	return fmt.Sprintf(`[project]
name = %q
version = "0.1.0"
requires-python = ">=3.10"
dependencies = []
`, projectName(dir))
}

// projectName derives a normalized Python project name from the directory
// name: lowercase alphanumerics, with every other run of characters turned
// into one hyphen.
func projectName(dir string) string {
	var b strings.Builder
	pendingHyphen := false
	for _, r := range strings.ToLower(filepath.Base(dir)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(r)
			continue
		}
		pendingHyphen = true
	}
	if b.Len() == 0 {
		return defaultProjectName
	}
	return b.String()
}

// addLibrary installs the Serverless library into the App directory's .venv
// and records it as a dev dependency. It uses uv, or Python when uv is not on
// PATH, and returns the toolchain it used. Toolchain output goes to w, which
// is stderr.
func addLibrary(ctx context.Context, dir string, w io.Writer) (string, error) {
	if uv, err := exec.LookPath(toolchainUV); err == nil {
		return toolchainUV, uvAddLibrary(ctx, uv, dir, w)
	}
	python, tooOld, ok := findPython(ctx, runtime.GOOS)
	if !ok {
		return "", noToolchainError(dir, tooOld)
	}
	_, _ = fmt.Fprintf(w, "uv is not on PATH, so init uses Python %s (%s) and pip.\n", python.version, python.path)
	return toolchainPython, pythonAddLibrary(ctx, python, dir, runtime.GOOS, w)
}

// noToolchainError says how to finish an App directory whose files init wrote
// but whose library it could not install. tooOld names each Python that was
// found but is older than 3.10.
func noToolchainError(dir string, tooOld []string) error {
	found := ""
	if len(tooOld) > 0 {
		found = fmt.Sprintf(" (%s, which is too old)", strings.Join(tooOld, ", "))
	}
	return fmt.Errorf(
		"uv was not found on PATH, and no Python 3.10 or newer was found either%s. %s has its files, but the Serverless library is not installed. To finish, install uv from https://docs.astral.sh/uv/ and run %q in that directory, or install Python 3.10 or newer and run: runware serverless init %s",
		found, dir, uvAddCommand(), quoteArg(dir, runtime.GOOS),
	)
}

// uvAddLibrary adds the Serverless library as a dev dependency of the App
// directory. uv's own output goes to w, which is stderr: stdout carries the
// result --format json promises.
func uvAddLibrary(ctx context.Context, uv, dir string, w io.Writer) error {
	env := slices.DeleteFunc(os.Environ(), isUVProjectOverride)
	if err := runIn(exec.CommandContext(ctx, uv, uvAddArgs...), dir, env, w); err != nil {
		return fmt.Errorf("%s failed in %s: %w (uv's output is above)", uvAddCommand(), dir, err)
	}
	return nil
}

// runIn runs c in dir with env, with both of its output streams on w.
func runIn(c *exec.Cmd, dir string, env []string, w io.Writer) error {
	c.Dir = dir
	c.Stdout = w
	c.Stderr = w
	c.Env = env
	return c.Run()
}

// uvAddArgs adds the Serverless library as a dev dependency.
var uvAddArgs = []string{
	"add",
	"--dev",
	serverlessLibrary,
}

func uvAddCommand() string {
	return toolchainUV + " " + strings.Join(uvAddArgs, " ")
}

// isUVProjectOverride compares names case-insensitively, as Windows does.
func isUVProjectOverride(kv string) bool {
	key, _, _ := strings.Cut(kv, "=")
	_, ok := uvProjectOverrides[strings.ToUpper(key)]
	return ok
}

// reportInitNextSteps prints the commands from a ready App directory to a
// first deploy. w must be stderr, for the same reason as uvAddLibrary's.
func reportInitNextSteps(w io.Writer, dir, cdArg string) {
	entry := "<entry-file>.py"
	if _, err := os.Stat(filepath.Join(dir, starterAppFile)); err == nil {
		entry = starterAppFile
	}
	appID := projectName(dir)
	if !appIDPattern.MatchString(appID) {
		appID = "<app-id>"
	}

	_, _ = fmt.Fprintln(w, "Next steps:")
	if cdArg != "" {
		_, _ = fmt.Fprintf(w, "  cd %s\n", quoteArg(cdArg, runtime.GOOS))
	}
	_, _ = fmt.Fprintln(w, "  runware auth login")
	_, _ = fmt.Fprintf(w, "  runware serverless deploy %s --id %s --gpu-type <gpu>\n", entry, appID)
	_, _ = fmt.Fprintln(w, "List GPU types with 'runware serverless gpus'.")
}

// quoteArg quotes s for the shell the customer most likely runs on goos.
// PowerShell and cmd both read a double-quoted path, and a Windows path cannot
// hold a double quote.
func quoteArg(s, goos string) string {
	if goos == goosWindows {
		if windowsSafePattern.MatchString(s) {
			return s
		}
		return `"` + s + `"`
	}
	if posixSafePattern.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
