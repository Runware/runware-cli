package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	remoteOrigin = "origin"
	cleanText    = "clean text"
)

// leak is text with one finding, built at run time (see rules_test.go).
var leak = "see https://" + ghRepo + "some-private-repo/pull/1"

// gitRepo makes a repository whose origin/main is one commit and whose
// feature branch adds b.txt holding text, committed with message.
func gitRepo(t *testing.T, text, message string) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test arguments
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "test")
	git("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "base")
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	git("checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte(text+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", message)
	return dir
}

// gitIn runs git in dir with the user's configuration ignored.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test arguments
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// tagRepo is gitRepo with an annotated tag v1.0.0, whose message holds a
// leak, and a lightweight tag v0.9.0, both on the commit origin/main has
// already, and a lightweight tag v1.1.0 on the new commit.
func tagRepo(t *testing.T, text string) string {
	t.Helper()
	dir := gitRepo(t, text, "feat: add b")
	gitIn(t, dir, "tag", "-a", "v1.0.0", "-m", "release\n\n"+leak, "origin/main")
	gitIn(t, dir, "tag", "v0.9.0", "origin/main")
	gitIn(t, dir, "tag", "v1.1.0", "HEAD")
	return dir
}

// TestPrePushTag checks a pushed tag: its name, the message of an annotated
// tag, and the commits it points to that the remote does not have.
func TestPrePushTag(t *testing.T) {
	dir := tagRepo(t, leak)
	g := Git{
		Dir: dir,
	}
	ctx := context.Background()
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	zero := strings.Repeat("0", 40)
	line := func(tag string) string {
		return "refs/tags/" + tag + " " + gitIn(t, dir, "rev-parse", "refs/tags/"+tag) + " refs/tags/" + tag + " " + zero + "\n"
	}

	f, err := s.prePush(ctx, g, []string{remoteOrigin}, strings.NewReader(line("v1.0.0")))
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Where != "tag v1.0.0 message line 3" {
		t.Fatalf("findings for an annotated tag = %+v, want one in its message", f)
	}

	if f, err := s.prePush(ctx, g, []string{remoteOrigin}, strings.NewReader(line("v0.9.0"))); err != nil || len(f) != 0 {
		t.Fatalf("findings for a tag on a published commit = %+v, %v", f, err)
	}

	f, err = s.prePush(ctx, g, []string{remoteOrigin}, strings.NewReader(line("v1.1.0")))
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Where != "b.txt:1" {
		t.Fatalf("findings for a tag on a new commit = %+v, want one at b.txt:1", f)
	}

	if f := s.ScanText("tag name", "v1-"+clusterHost); len(f) != 1 {
		t.Fatalf("tag name findings = %+v", f)
	}
}

// TestPrePush checks a new branch, a push to a private repository, a push
// with nothing new and a deletion.
func TestPrePush(t *testing.T) {
	dir := gitRepo(t, leak, "feat: add b")
	g := Git{
		Dir: dir,
	}
	ctx := context.Background()
	head, err := g.revParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	zero := strings.Repeat("0", 40)
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}

	newBranch := "refs/heads/feature " + head + " refs/heads/feature " + zero + "\n"
	f, err := s.prePush(ctx, g, []string{remoteOrigin}, strings.NewReader(newBranch))
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Where != "b.txt:1" {
		t.Fatalf("findings = %+v, want one at b.txt:1", f)
	}

	privateURL := "git@github.com:" + shortRepo + "some-private-repo.git"
	if f, err := s.prePush(ctx, g, []string{"other", privateURL}, strings.NewReader(newBranch)); err != nil || len(f) != 0 {
		t.Fatalf("findings for a push to a private repository = %+v, %v", f, err)
	}

	upToDate := "refs/heads/feature " + head + " refs/heads/feature " + head + "\n"
	if f, err := s.prePush(ctx, g, []string{remoteOrigin}, strings.NewReader(upToDate)); err != nil || len(f) != 0 {
		t.Fatalf("findings for a push with nothing new = %+v, %v", f, err)
	}

	deletion := "(delete) " + zero + " refs/heads/feature " + head + "\n"
	if f, err := s.prePush(ctx, g, []string{remoteOrigin}, strings.NewReader(deletion)); err != nil || len(f) != 0 {
		t.Fatalf("findings for a deletion = %+v, %v", f, err)
	}
}

// TestScanRangeHistory checks that a leak added by one commit and removed by
// the next is still found: the history is published, not only the tree.
func TestScanRangeHistory(t *testing.T) {
	dir := gitRepo(t, leak, "feat: add b")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("clean text\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-q", "-am", "fix: remove it")
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.ScanRange(context.Background(), Git{Dir: dir}, Range{
		Base: "origin/main",
		Head: "HEAD",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || !strings.HasPrefix(f[0].Where, "commit ") {
		t.Fatalf("findings = %+v, want one in a commit", f)
	}
}

// TestPrePushAfterRebase checks a force-push of a branch rebased onto a newer
// main: the main commits in between are on the remote already and are not
// the pusher's to fix, so a leak in one of them does not block the push.
func TestPrePushAfterRebase(t *testing.T) {
	dir := gitRepo(t, cleanText, "feat: add b")
	old := gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/feature", old)
	gitIn(t, dir, "checkout", "-q", "-b", "newmain", "origin/main")
	if err := os.WriteFile(filepath.Join(dir, "m.txt"), []byte(leak+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: someone else's work")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitIn(t, dir, "checkout", "-q", "feature")
	gitIn(t, dir, "rebase", "-q", "newmain")
	head := gitIn(t, dir, "rev-parse", "HEAD")
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	line := "refs/heads/feature " + head + " refs/heads/feature " + old + "\n"
	f, err := s.prePush(context.Background(), Git{Dir: dir}, []string{remoteOrigin}, strings.NewReader(line))
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 0 {
		t.Fatalf("findings = %+v, want none from main's commits", f)
	}
}

// TestAddedPathIsScanned checks that the path of a file added or renamed in
// the range is scanned once, however many commits touch it.
func TestAddedPathIsScanned(t *testing.T) {
	name := "notes-" + clusterHost + ".md"
	added := gitRepo(t, cleanText, "feat: add b")
	if err := os.WriteFile(filepath.Join(added, name), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, added, "add", ".")
	gitIn(t, added, "commit", "-q", "-m", "docs: add notes")
	if err := os.WriteFile(filepath.Join(added, name), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, added, "commit", "-q", "-am", "docs: edit notes")
	renamed := gitRepo(t, cleanText, "feat: add b")
	gitIn(t, renamed, "mv", "a.txt", name)
	gitIn(t, renamed, "commit", "-q", "-m", "chore: rename a")
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{added, renamed} {
		f, err := s.ScanRange(context.Background(), Git{Dir: dir}, Range{
			Base: "origin/main",
			Head: "HEAD",
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(f) != 1 || f[0].Where != "path of an added file" {
			t.Fatalf("findings = %+v, want one in the path", f)
		}
	}
}

// TestBranchNameIsScanned checks that the pushed branch name is scanned.
func TestBranchNameIsScanned(t *testing.T) {
	dir := gitRepo(t, "clean", "feat: add b")
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	g := Git{
		Dir: dir,
	}
	f, err := s.ScanRange(context.Background(), g, Range{
		Base:   "origin/main",
		Head:   "HEAD",
		Branch: "fix/" + clusterHost,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Where != "branch name" {
		t.Fatalf("findings = %+v", f)
	}
}

// leakyCommit makes a repository whose new commit has a leak in its message.
func leakyCommit(t *testing.T) string {
	return gitRepo(t, cleanText, "feat: add b\n\n"+leak)
}

// cleanRepo makes a repository whose new commit holds nothing to find.
func cleanRepo(t *testing.T) string {
	return gitRepo(t, cleanText, "feat: add b")
}
