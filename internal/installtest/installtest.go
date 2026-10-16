// Package installtest runs the install scripts in public/ as a black box. It
// serves a fake CLI release over HTTP, runs a script with sh or pwsh against
// it, and exposes what a test can observe afterwards: the script's output and
// exit status, the requests the release received and the argv each stub CLI
// recorded.
package installtest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// DefaultTag is the version a fake release reports as latest when the test
// names none.
const DefaultTag = "v9.9.9"

// StubConfig shapes a stub runware binary. The zero value is a CLI that has
// serverless init and whose init succeeds.
type StubConfig struct {
	// Name labels the calls this stub records, so that a test with more than
	// one stub can tell them apart.
	Name string
	// WithoutInit makes the stub act as a CLI from before serverless init.
	// Then "serverless init --help" prints the help of the serverless group
	// and exits 0, as Cobra does for an unknown subcommand.
	WithoutInit bool
	// InitExit is the exit status of a serverless init call that does not ask
	// for help.
	InitExit int
}

// stubTemplate is the body of a stub CLI. Each call appends one line to the
// file named by RUNWARE_STUB_LOG: the stub's name and then its argv, with
// every field terminated by the ASCII unit separator. Its verbs take the
// name, already quoted for sh, whether the stub has init, and the exit status
// of init.
const stubTemplate = `#!/bin/sh
{
  printf '%%s\037' %s
  for arg do
    printf '%%s\037' "$arg"
  done
  printf '\n'
} >> "${RUNWARE_STUB_LOG:-/dev/null}"
has_init=%t
if [ "$1" = serverless ] && [ "$2" = init ]; then
  if [ "$has_init" = false ]; then
    printf 'Usage:\n  runware serverless [command]\n'
    exit 0
  fi
  shift 2
  if [ "$1" = --help ]; then
    printf 'Usage:\n  runware serverless init [dir] [flags]\n'
    exit 0
  fi
  if [ "$1" = -- ]; then
    shift
  fi
  echo "stub serverless init $1"
  exit %d
fi
`

// Stub returns the content of a stub runware binary shaped by cfg.
func Stub(cfg StubConfig) string {
	return fmt.Sprintf(stubTemplate, shellQuote(cfg.Name), !cfg.WithoutInit, cfg.InitExit)
}

// WriteStub writes a stub runware binary into a new directory and returns
// the directory, for a test to put on PATH.
func WriteStub(t testing.TB, cfg StubConfig) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "runware"), []byte(Stub(cfg)), 0o700); err != nil { //nolint:gosec // test executable
		t.Fatalf("write stub CLI: %v", err)
	}
	return dir
}

// InitArgs is the argv of a serverless init call for the App directory dir.
// The "--" ends flag parsing, so that a directory such as --force stays a
// directory.
func InitArgs(dir string) []string {
	return []string{
		"serverless",
		"init",
		"--",
		dir,
	}
}

// ReleaseStubName is the name of the stub in a fake release when the test
// gives no stub of its own.
const ReleaseStubName = "release"

// systemPath holds the standard tools an install script needs (curl, tar,
// shasum or sha256sum) and leaves out the directories Homebrew installs into,
// so a script run on macOS takes the direct-download path.
const systemPath = "/usr/bin:/bin:/usr/sbin:/sbin"

// defaultTimeout bounds one script run so that a hung download fails the
// test.
const defaultTimeout = 30 * time.Second

// releaseTargets are the OS and architecture names the install scripts put in
// an archive name. The fake release has a checksum entry for each of them, so
// a script finds its archive on any machine that runs the tests.
var releaseTargets = []string{
	"darwin_amd64",
	"darwin_arm64",
	"linux_amd64",
	"linux_arm64",
}

// ReleaseConfig shapes a fake release. The zero value serves DefaultTag with
// correct checksums and a stub named ReleaseStubName that has init.
type ReleaseConfig struct {
	// Tag is the version the latest-version lookup reports.
	Tag string
	// Stub is the content of the runware binary in the archive, usually
	// from Stub.
	Stub string
	// BadChecksum makes checksums.txt list a digest that does not match the
	// archive, as a tampered download would.
	BadChecksum bool
}

// Release is a fake CLI release served over HTTP. Its URL is the value a
// script reads from RUNWARE_RELEASE_BASE_URL: the latest-version lookup is
// at URL/latest and the release files are at URL/download/<tag>/<file>.
type Release struct {
	URL string

	mu       sync.Mutex
	requests []string
}

// NewRelease starts a fake release that stops when the test ends.
func NewRelease(t testing.TB, cfg ReleaseConfig) *Release {
	t.Helper()

	if cfg.Tag == "" {
		cfg.Tag = DefaultTag
	}
	if cfg.Stub == "" {
		cfg.Stub = Stub(StubConfig{
			Name: ReleaseStubName,
		})
	}

	archive := releaseArchive(t, cfg.Stub)
	digest := sha256.Sum256(archive)
	if cfg.BadChecksum {
		digest = sha256.Sum256([]byte("tampered"))
	}
	var checksums strings.Builder
	for _, target := range releaseTargets {
		fmt.Fprintf(&checksums, "%s  runware_%s.tar.gz\n", hex.EncodeToString(digest[:]), target)
	}

	// GitHub pretty-prints this response; the scripts read tag_name from its
	// own line, so the fake must match that layout.
	latest, err := json.MarshalIndent(latestRelease{
		TagName: cfg.Tag,
	}, "", "  ")
	if err != nil {
		t.Fatalf("encode latest release: %v", err)
	}

	r := &Release{}
	download := "/download/" + cfg.Tag + "/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.record(req.URL.Path)
		switch name, ok := strings.CutPrefix(req.URL.Path, download); {
		case req.URL.Path == "/latest":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(latest)
		case ok && name == "checksums.txt":
			_, _ = w.Write([]byte(checksums.String()))
		case ok && strings.HasPrefix(name, "runware_") && strings.HasSuffix(name, ".tar.gz"):
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	r.URL = srv.URL
	return r
}

// latestRelease is the part of GitHub's latest-release response the scripts
// read.
type latestRelease struct {
	TagName string `json:"tag_name"`
}

func (r *Release) record(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, path)
}

// Requests returns the path of every request the release received, in order.
func (r *Release) Requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.requests)
}

// releaseArchive builds a tarball shaped like a GoReleaser archive, with the
// runware binary at its root.
func releaseArchive(t testing.TB, stub string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "runware",
		Mode:     0o755,
		Size:     int64(len(stub)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("write archive header: %v", err)
	}
	if _, err := tw.Write([]byte(stub)); err != nil {
		t.Fatalf("write archive body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

// Invocation describes one run of an install script.
type Invocation struct {
	// Script is the path of the script to run.
	Script string
	// Args are the script's positional arguments.
	Args []string
	// Env sets environment variables for the script. It can override HOME
	// and TMPDIR, which otherwise point at fresh temporary directories.
	Env map[string]string
	// PathDirs come before the standard tool directories on PATH.
	PathDirs []string
	// Timeout bounds the run. Zero means 30 seconds.
	Timeout time.Duration
}

// onPath reports whether sh would find an executable file named name in the
// directories of path.
func onPath(name, path string) bool {
	for _, dir := range filepath.SplitList(path) {
		info, err := os.Stat(filepath.Join(dir, name))
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return true
		}
	}
	return false
}

// Result is what a script run left behind.
type Result struct {
	ExitCode int
	// Output is stdout and stderr combined.
	Output string

	stubLog string
}

// RunSh runs a POSIX install script with sh. The script sees only the
// environment the invocation gives it, so the developer's own RUNWARE_*
// variables and Homebrew never leak in. The test skips where sh is missing,
// where the script's PATH has no curl, and on Windows, which the POSIX
// scripts do not support.
func RunSh(t testing.TB, inv Invocation) Result {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the POSIX install scripts support only Linux and macOS")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not found")
	}
	path := scriptPath(inv)
	if !onPath("curl", path) {
		t.Skip("curl not found on the script's PATH")
	}

	return runInterpreter(t, inv, path, nil, sh, append([]string{inv.Script}, inv.Args...)...)
}

// iexCommand runs the script the way 'irm <url> | iex' does: as one string
// given to Invoke-Expression, not as a script file. A script that only works
// as a file, for example one that reads $args or calls exit, fails here as
// it would for a user.
const iexCommand = `Get-Content -Raw -LiteralPath $env:RUNWARE_TEST_SCRIPT | Invoke-Expression`

// RunPwsh runs a PowerShell install script with pwsh, in the same clean
// environment as RunSh. Such a script takes no arguments, so inv.Args must be
// empty. The test skips where pwsh is missing, and on Windows: there the stub
// CLI, a sh script, cannot run as runware.exe.
func RunPwsh(t testing.TB, inv Invocation) Result {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the stub CLI is a sh script, which Windows cannot run")
	}
	if len(inv.Args) > 0 {
		t.Fatalf("a PowerShell install script takes no arguments, got %q", inv.Args)
	}
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh not found")
	}
	script, err := filepath.Abs(inv.Script)
	if err != nil {
		t.Fatalf("resolve %s: %v", inv.Script, err)
	}

	env := map[string]string{
		"RUNWARE_TEST_SCRIPT":         script,
		"POWERSHELL_TELEMETRY_OPTOUT": "1",
		"POWERSHELL_UPDATECHECK":      "Off",
	}
	return runInterpreter(t, inv, scriptPath(inv), env, pwsh, "-NoProfile", "-NonInteractive", "-Command", iexCommand)
}

// scriptPath is the PATH a script gets: the invocation's directories, then
// the standard tool directories.
func scriptPath(inv Invocation) string {
	return strings.Join(append(slices.Clone(inv.PathDirs), systemPath), string(os.PathListSeparator))
}

// runInterpreter runs name with args as the interpreter of inv.Script. The environment
// holds fresh HOME and TMPDIR directories, path, the stub log, then extra,
// then inv.Env.
func runInterpreter(t testing.TB, inv Invocation, path string, extra map[string]string, name string, args ...string) Result {
	t.Helper()

	stubLog := filepath.Join(t.TempDir(), "runware-calls")
	env := map[string]string{
		"HOME":             t.TempDir(),
		"TMPDIR":           t.TempDir(),
		"PATH":             path,
		"RUNWARE_STUB_LOG": stubLog,
	}
	maps.Copy(env, extra)
	maps.Copy(env, inv.Env)

	timeout := inv.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // the test names the script it runs
	killGroupOnCancel(cmd)
	// A child that left the process group can still hold the output pipe
	// open, so stop waiting for output soon after the kill.
	cmd.WaitDelay = time.Second
	cmd.Env = make([]string, 0, len(env))
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()

	res := Result{
		Output:  string(out),
		stubLog: stubLog,
	}
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() != nil:
		t.Fatalf("%s did not finish within %s\n%s", inv.Script, timeout, out)
	case err == nil:
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		t.Fatalf("run %s: %v\n%s", inv.Script, err, out)
	}
	return res
}

// StubCall is one call to a stub CLI.
type StubCall struct {
	// Stub is the StubConfig.Name of the stub that ran.
	Stub string
	Args []string
}

// StubCalls returns every call to a stub CLI during the run, in order. It is
// empty when no stub ran.
func (r Result) StubCalls(t testing.TB) []StubCall {
	t.Helper()

	data, err := os.ReadFile(r.stubLog)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read stub log: %v", err)
	}
	var calls []StubCall
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSuffix(line, "\n")
		fields := strings.Split(line, "\x1f")
		// Every field ends with a separator, so the last one is empty.
		fields = fields[:len(fields)-1]
		calls = append(calls, StubCall{
			Stub: fields[0],
			Args: fields[1:],
		})
	}
	return calls
}

// Tripwires is a directory of tools that a script must never run. Each one
// logs its command line and exits 1. Put Dir first on PATH, and check Calls
// after the run.
type Tripwires struct {
	Dir string

	log string
}

// forbiddenToolTemplate logs the command line of a tool that should not have run.
// Its verbs take the tool's name and the log path, each already quoted for
// sh.
const forbiddenToolTemplate = `#!/bin/sh
{
  printf '%%s' %s
  [ "$#" -eq 0 ] || printf ' %%s' "$*"
  printf '\n'
} >> %s
echo "tripwire: $0 must not run" >&2
exit 1
`

// NewTripwires writes a tripwire for each of the named tools.
func NewTripwires(t testing.TB, names ...string) Tripwires {
	t.Helper()

	tw := Tripwires{
		Dir: t.TempDir(),
		log: filepath.Join(t.TempDir(), "tripwire-calls"),
	}
	for _, name := range names {
		script := fmt.Sprintf(forbiddenToolTemplate, shellQuote(name), shellQuote(tw.log))
		if err := os.WriteFile(filepath.Join(tw.Dir, name), []byte(script), 0o700); err != nil { //nolint:gosec // test executable
			t.Fatalf("write tripwire %s: %v", name, err)
		}
	}
	return tw
}

// Calls returns the command line of every tripwire that ran, in order.
func (tw Tripwires) Calls(t testing.TB) []string {
	t.Helper()

	data, err := os.ReadFile(tw.log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read tripwire log: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// GitHubShim is a directory holding a curl that stands in for the real one.
// It sends the default GitHub release URLs to a fake release and logs every
// URL it was asked for. Put Dir first on PATH to run a script with no
// RUNWARE_RELEASE_BASE_URL and still stay offline.
type GitHubShim struct {
	Dir string

	log string
}

// githubShimScript rewrites the URLs in curl's arguments and then runs the
// real curl. Its %s verbs take the base URL of the fake release, the log path
// and the real curl, each already quoted for sh.
const githubShimScript = `#!/bin/sh
base=%s
log=%s
n=$#
for arg do
  case $arg in
    https://*)
      printf '%%s\n' "$arg" >> "$log"
      case $arg in
        https://api.github.com/repos/Runware/runware-cli/releases/latest)
          arg="$base/latest" ;;
        https://github.com/Runware/runware-cli/releases/*)
          arg="$base/${arg#https://github.com/Runware/runware-cli/releases/}" ;;
        *)
          arg="$base/unexpected" ;;
      esac
      ;;
  esac
  set -- "$@" "$arg"
done
shift "$n"
exec %s "$@"
`

// GitHubShim writes a curl shim that routes the GitHub release URLs to r. The
// test skips where there is no curl to run behind the shim.
func (r *Release) GitHubShim(t testing.TB) GitHubShim {
	t.Helper()

	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl not found")
	}
	shim := GitHubShim{
		Dir: t.TempDir(),
		log: filepath.Join(t.TempDir(), "curl-urls"),
	}
	script := fmt.Sprintf(githubShimScript, shellQuote(r.URL), shellQuote(shim.log), shellQuote(curl))
	if err := os.WriteFile(filepath.Join(shim.Dir, "curl"), []byte(script), 0o700); err != nil { //nolint:gosec // test executable
		t.Fatalf("write curl shim: %v", err)
	}
	return shim
}

// URLs returns every URL the shim was asked for, in order, before any
// rewrite.
func (s GitHubShim) URLs(t testing.TB) []string {
	t.Helper()

	data, err := os.ReadFile(s.log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read curl shim log: %v", err)
	}
	return strings.Fields(string(data))
}

// shellQuote quotes s as one sh word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
