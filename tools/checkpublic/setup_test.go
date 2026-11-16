package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const goosWindows = "windows"

// setupRun runs name with args in dir, with the user's git configuration
// ignored, and returns its combined output and whether it succeeded.
func setupRun(t *testing.T, dir, name string, args ...string) (string, bool) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), name, args...) //nolint:gosec // test commands
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// mustRun is setupRun that fails the test when the command fails.
func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	if out, ok := setupRun(t, dir, name, args...); !ok {
		t.Fatalf("%s %v failed:\n%s", name, args, out)
	}
}

// copyFile copies src to dst, creating dst's directory.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src) //nolint:gosec // a file of this repository
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil { //nolint:gosec // a path under the test's temporary directory
		t.Fatal(err)
	}
}

// setupClone makes a clone holding this repository's Makefile, module files
// and checker source, whose main is pushed to a local bare origin.
func setupClone(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	dir := filepath.Join(root, "clone")
	mustRun(t, root, "git", "init", "-q", "--bare", "-b", "main", origin)
	mustRun(t, root, "git", "init", "-q", "-b", "main", dir)
	for _, kv := range [][2]string{
		{"user.email", "test@example.com"},
		{"user.name", "test"},
		{"commit.gpgsign", "false"},
	} {
		mustRun(t, dir, "git", "config", kv[0], kv[1])
	}
	for _, name := range []string{"Makefile", "go.mod", "go.sum"} {
		copyFile(t, filepath.Join("..", "..", name), filepath.Join(dir, name))
	}
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, "public-repos.txt")
	for _, name := range sources {
		if !strings.HasSuffix(name, "_test.go") {
			copyFile(t, name, filepath.Join(dir, "tools", "checkpublic", name))
		}
	}
	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "commit", "-q", "-m", "base")
	mustRun(t, dir, "git", "remote", "add", "origin", origin)
	mustRun(t, dir, "git", "push", "-q", "origin", "main")
	mustRun(t, dir, "git", "fetch", "-q", "origin")
	return dir
}

// TestMakeSetup checks what make setup installs: the checker built into the
// common git directory and a pre-push hook there that runs it, so a linked
// worktree is covered and a push runs no code from the pushed branch. It
// can run again.
func TestMakeSetup(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("make setup writes a POSIX shell hook")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	dir := setupClone(t)
	worktree := filepath.Join(filepath.Dir(dir), "worktree")
	mustRun(t, dir, "git", "worktree", "add", "-q", "-b", "feature", worktree)

	mustRun(t, worktree, "make", "setup")
	common := filepath.Join(dir, ".git")
	hook := filepath.Join(common, "hooks", "pre-push")
	script, err := os.ReadFile(hook) //nolint:gosec // the hook make setup wrote
	if err != nil {
		t.Fatalf("no pre-push hook in the common git directory: %v", err)
	}
	if !strings.Contains(string(script), "/checkpublic/bin/checkpublic") || !strings.Contains(string(script), `prepush "$@"`) {
		t.Fatalf("the hook does not run the installed checker:\n%s", script)
	}
	if info, err := os.Stat(filepath.Join(common, "checkpublic", "bin", "checkpublic")); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("installed checker missing or not executable: %v", err)
	}
	// Running it again is fine and leaves the same hook.
	mustRun(t, dir, "make", "setup")
	if again, err := os.ReadFile(hook); err != nil || !bytes.Equal(again, script) { //nolint:gosec // the hook make setup wrote
		t.Fatalf("second run changed the hook (%v):\n%s", err, again)
	}

	// The checker the hook runs is the installed copy: a branch that breaks
	// the source does not change what checks its push.
	broken := filepath.Join(worktree, "tools", "checkpublic", "main.go")
	if err := os.WriteFile(broken, []byte("package main\n\nfunc main() {"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "notes.md"), []byte(leak+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, worktree, "git", "add", ".")
	mustRun(t, worktree, "git", "commit", "-q", "-m", "docs: add notes")
	out, ok := setupRun(t, worktree, "git", "push", "origin", "feature")
	if ok {
		t.Fatalf("a push with a finding went through:\n%s", out)
	}
	if !strings.Contains(out, "["+ruleRepo+"]") {
		t.Fatalf("the push was not blocked by the finding:\n%s", out)
	}

	// A clean branch pushes.
	mustRun(t, dir, "git", "checkout", "-q", "-b", "clean")
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte(cleanText+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "commit", "-q", "-m", "docs: add notes")
	mustRun(t, dir, "git", "push", "-q", "origin", "clean")
}

// TestMakeSetupKeepsOtherHook checks that make setup refuses to replace a
// pre-push hook it did not install.
func TestMakeSetupKeepsOtherHook(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("make setup writes a POSIX shell hook")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	dir := setupClone(t)
	hook := filepath.Join(dir, ".git", "hooks", "pre-push")
	own := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(hook, []byte(own), 0o700); err != nil { //nolint:gosec // a hook must be executable
		t.Fatal(err)
	}
	if out, ok := setupRun(t, dir, "make", "setup"); ok || !strings.Contains(out, "was not installed by make setup") {
		t.Fatalf("make setup over another hook: ok=%v\n%s", ok, out)
	}
	if b, err := os.ReadFile(hook); err != nil || string(b) != own { //nolint:gosec // the hook this test wrote
		t.Fatalf("the other hook changed (%v): %q", err, b)
	}
}

// TestMakeSetupWarnsAboutHooksPath checks that make setup warns when
// core.hooksPath is set, since git then does not run the hook it installs,
// and leaves the setting to the user.
func TestMakeSetupWarnsAboutHooksPath(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("make setup writes a POSIX shell hook")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	dir := setupClone(t)
	mustRun(t, dir, "git", "config", "core.hooksPath", ".hooks")
	out, ok := setupRun(t, dir, "make", "setup")
	if !ok || !strings.Contains(out, "while it is set") || !strings.Contains(out, "git config --unset core.hooksPath") {
		t.Fatalf("make setup with core.hooksPath set: ok=%v\n%s", ok, out)
	}
	if got, ok := setupRun(t, dir, "git", "config", "--get", "core.hooksPath"); !ok || strings.TrimSpace(got) != ".hooks" {
		t.Fatalf("core.hooksPath = %q, want it left as set", got)
	}
}

// TestMakeSetupRefusesDowngrade checks that make setup records the commit
// the installed checker was built from and does not replace it with a
// build from an older commit, unless FORCE=1.
func TestMakeSetupRefusesDowngrade(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("make setup writes a POSIX shell hook")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	dir := setupClone(t)
	recorded := filepath.Join(dir, ".git", "checkpublic", "bin", "checkpublic.commit")
	installedFrom := func() string {
		t.Helper()
		b, err := os.ReadFile(recorded) //nolint:gosec // the file make setup wrote
		if err != nil {
			t.Fatalf("no recorded commit: %v", err)
		}
		return strings.TrimSpace(string(b))
	}
	old := gitIn(t, dir, "rev-parse", headRef)
	mustRun(t, dir, "make", "setup")
	if got := installedFrom(); got != old {
		t.Fatalf("recorded commit = %q, want %q", got, old)
	}
	mustRun(t, dir, "git", "commit", "-q", "--allow-empty", "-m", "chore: newer checker")
	newer := gitIn(t, dir, "rev-parse", headRef)
	mustRun(t, dir, "make", "setup")
	if got := installedFrom(); got != newer {
		t.Fatalf("recorded commit = %q, want %q", got, newer)
	}

	mustRun(t, dir, "git", "checkout", "-q", old)
	out, ok := setupRun(t, dir, "make", "setup")
	if ok || !strings.Contains(out, "newer than this checkout") || !strings.Contains(out, "FORCE=1") {
		t.Fatalf("make setup from an older commit: ok=%v\n%s", ok, out)
	}
	if got := installedFrom(); got != newer {
		t.Fatalf("a refused setup changed the recorded commit to %q", got)
	}
	mustRun(t, dir, "make", "setup", "FORCE=1")
	if got := installedFrom(); got != old {
		t.Fatalf("FORCE=1: recorded commit = %q, want %q", got, old)
	}
}
