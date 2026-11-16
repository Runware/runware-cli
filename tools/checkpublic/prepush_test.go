package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const (
	remoteOrigin = "origin"
	cleanText    = "clean text"
	headRef      = "HEAD"
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
	git("update-ref", "refs/remotes/origin/main", headRef)
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
	gitIn(t, dir, "tag", "v1.1.0", headRef)
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
	s := NewScanner()
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
	head, err := g.revParse(ctx, headRef)
	if err != nil {
		t.Fatal(err)
	}
	zero := strings.Repeat("0", 40)
	s := NewScanner()

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
	s := NewScanner()
	f, err := s.ScanRange(context.Background(), Git{Dir: dir}, Range{
		Base: "origin/main",
		Head: headRef,
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
	old := gitIn(t, dir, "rev-parse", headRef)
	gitIn(t, dir, "update-ref", "refs/remotes/origin/feature", old)
	gitIn(t, dir, "checkout", "-q", "-b", "newmain", "origin/main")
	if err := os.WriteFile(filepath.Join(dir, "m.txt"), []byte(leak+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: someone else's work")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/main", headRef)
	gitIn(t, dir, "checkout", "-q", "feature")
	gitIn(t, dir, "rebase", "-q", "newmain")
	head := gitIn(t, dir, "rev-parse", headRef)
	s := NewScanner()
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
	s := NewScanner()
	for _, dir := range []string{added, renamed} {
		f, err := s.ScanRange(context.Background(), Git{Dir: dir}, Range{
			Base: "origin/main",
			Head: headRef,
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
	s := NewScanner()
	g := Git{
		Dir: dir,
	}
	f, err := s.ScanRange(context.Background(), g, Range{
		Base:   "origin/main",
		Head:   headRef,
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

// mergeMain makes origin/main one commit newer, adding m.txt holding
// mainText, and merges it into the feature branch. Before the merge is
// committed, resolve, when set, edits the work tree as a merge resolution
// would. It returns the merge commit.
func mergeMain(t *testing.T, dir, mainText string, resolve func()) string {
	t.Helper()
	gitIn(t, dir, "checkout", "-q", "-b", "newmain", "origin/main")
	if err := os.WriteFile(filepath.Join(dir, "m.txt"), []byte(mainText+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: main work")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/main", headRef)
	gitIn(t, dir, "checkout", "-q", "feature")
	gitIn(t, dir, "merge", "-q", "--no-ff", "--no-commit", "origin/main")
	if resolve != nil {
		resolve()
		gitIn(t, dir, "add", "-A")
	}
	gitIn(t, dir, "commit", "-q", "-m", "Merge main into feature")
	return gitIn(t, dir, "rev-parse", headRef)
}

// scanToMain scans dir's feature branch from its merge-base with origin/main.
func scanToMain(t *testing.T, dir string) []Finding {
	t.Helper()
	g := Git{
		Dir: dir,
	}
	base, err := g.DefaultBase(context.Background(), "", headRef)
	if err != nil {
		t.Fatal(err)
	}
	f, err := NewScanner().ScanRange(context.Background(), g, Range{
		Base: base,
		Head: headRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestScanRangeMergeResolution checks that what a merge resolution adds is
// scanned like a commit: the merge commit is published even when a later
// commit removes the text again.
func TestScanRangeMergeResolution(t *testing.T) {
	dir := gitRepo(t, cleanText, "feat: add b")
	merge := mergeMain(t, dir, cleanText, func() {
		if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte(cleanText+"\n"+leak+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte(cleanText+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-q", "-am", "fix: remove it")
	f := scanToMain(t, dir)
	if len(f) != 1 || f[0].Where != "commit "+short(merge)+" b.txt:2" {
		t.Fatalf("findings = %+v, want one in the merge commit", f)
	}
}

// TestScanRangeMergeAddedPath checks that the path of a file added only by
// a merge resolution is scanned, even when a later commit deletes it.
func TestScanRangeMergeAddedPath(t *testing.T) {
	dir := gitRepo(t, cleanText, "feat: add b")
	name := "notes-" + clusterHost + ".md"
	mergeMain(t, dir, cleanText, func() {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(cleanText+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	gitIn(t, dir, "rm", "-q", name)
	gitIn(t, dir, "commit", "-q", "-m", "chore: remove notes")
	if f := scanToMain(t, dir); len(f) != 1 || f[0].Where != "path of an added file" {
		t.Fatalf("findings = %+v, want one in the path", f)
	}
}

// TestScanRangeCleanMerge checks that merging main adds no findings of its
// own: main's commits are not the branch's to fix, and a clean merge
// resolves nothing.
func TestScanRangeCleanMerge(t *testing.T) {
	dir := gitRepo(t, cleanText, "feat: add b")
	mergeMain(t, dir, leak, nil)
	if f := scanToMain(t, dir); len(f) != 0 {
		t.Fatalf("findings = %+v, want none", f)
	}
}

// TestScanRangeIgnoresTextconv checks that the scan runs no textconv
// filter: a branch can configure one in .gitattributes, and it would run
// as a command on the pusher's machine.
func TestScanRangeIgnoresTextconv(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("the filter is a POSIX shell script")
	}
	dir := gitRepo(t, cleanText, "feat: add b")
	sentinel := filepath.Join(t.TempDir(), "textconv-ran")
	script := filepath.Join(t.TempDir(), "conv.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+sentinel+"'\ncat \"$1\"\n"), 0o700); err != nil { //nolint:gosec // a filter must be executable
		t.Fatal(err)
	}
	gitIn(t, dir, "config", "diff.conv.textconv", script)
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.txt diff=conv\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte(cleanText+"\nmore\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "chore: use a diff driver")
	if f := scanToMain(t, dir); len(f) != 0 {
		t.Fatalf("findings = %+v, want none", f)
	}
	if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the scan ran the textconv filter: %v", err)
	}
}

// TestPrePushToPublicRepo checks that a push to this public repository, by
// SSH or HTTPS URL, is scanned.
func TestPrePushToPublicRepo(t *testing.T) {
	dir := gitRepo(t, leak, "feat: add b")
	g := Git{
		Dir: dir,
	}
	head := gitIn(t, dir, "rev-parse", headRef)
	line := "refs/heads/feature " + head + " refs/heads/feature " + strings.Repeat("0", 40) + "\n"
	for _, url := range []string{
		"git@github.com:" + shortRepo + repoCLI + ".git",
		"https://" + ghRepo + repoCLI + ".git",
	} {
		f, err := NewScanner().prePush(context.Background(), g, []string{remoteOrigin, url}, strings.NewReader(line))
		if err != nil {
			t.Fatal(err)
		}
		if len(f) != 1 || f[0].Where != "b.txt:1" {
			t.Errorf("push to %s: findings = %+v, want one at b.txt:1", url, f)
		}
	}
}

// TestScanRangeIgnoresDiffAttribute checks that a branch's .gitattributes
// cannot hide a file from the scan by marking it -diff: a leak is found
// at the tip and in an earlier commit that a later one removes it from.
func TestScanRangeIgnoresDiffAttribute(t *testing.T) {
	commit := func(dir, notes, message string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.md -diff\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte(notes), 0o600); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "add", ".")
		gitIn(t, dir, "commit", "-q", "-m", message)
	}
	tip := gitRepo(t, cleanText, "feat: add b")
	commit(tip, cleanText+"\n"+leak+"\n", "docs: add notes")
	if f := scanToMain(t, tip); len(f) != 1 || f[0].Where != "notes.md:2" {
		t.Fatalf("at the tip: findings = %+v, want one at notes.md:2", f)
	}

	earlier := gitRepo(t, cleanText, "feat: add b")
	commit(earlier, cleanText+"\n"+leak+"\n", "docs: add notes")
	added := gitIn(t, earlier, "rev-parse", headRef)
	commit(earlier, cleanText+"\n", "docs: trim notes")
	if f := scanToMain(t, earlier); len(f) != 1 || f[0].Where != "commit "+short(added)+" notes.md:2" {
		t.Fatalf("in an earlier commit: findings = %+v, want one in %s", f, short(added))
	}
}

// TestScanRangeRunsNoMergeDriver checks that re-merging a merge for the
// scan does not run a merge driver that the branch's .gitattributes
// selects: it would run a command on the pusher's machine.
func TestScanRangeRunsNoMergeDriver(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("the driver is a POSIX shell script")
	}
	dir := gitRepo(t, cleanText, "feat: add b")
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitattributes", "*.txt merge=evil\n")
	write("a.txt", "feature\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: edit a")
	gitIn(t, dir, "checkout", "-q", "-b", "newmain", "origin/main")
	write("a.txt", "main\n")
	gitIn(t, dir, "commit", "-q", "-am", "feat: main edits a")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/main", headRef)
	gitIn(t, dir, "checkout", "-q", "feature")
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "merge", "-q", "--no-commit", "origin/main") //nolint:gosec // test arguments
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	_ = cmd.Run() // conflicts, resolved below
	write("a.txt", "feature and main\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "Merge main into feature")

	sentinel := filepath.Join(t.TempDir(), "driver-ran")
	script := filepath.Join(dir, "driver.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch '"+sentinel+"'\nexit 0\n"), 0o700); err != nil { //nolint:gosec // a driver must be executable
		t.Fatal(err)
	}
	gitIn(t, dir, "config", "merge.evil.driver", script+" %A %O %B")
	scanToMain(t, dir)
	if _, err := os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the scan ran the merge driver: %v", err)
	}
}

// TestScanRangeTextWithNUL checks that a text file holding a NUL byte is
// scanned as text, and that a file binary by its extension is skipped.
func TestScanRangeTextWithNUL(t *testing.T) {
	dir := gitRepo(t, cleanText, "feat: add b")
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("a\x00b\n"+leak+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), []byte("\x89PNG\x00\n"+leak+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "docs: add notes and a logo")
	if f := scanToMain(t, dir); len(f) != 1 || f[0].Where != "notes.txt:2" {
		t.Fatalf("findings = %+v, want one at notes.txt:2", f)
	}
}

// TestScanRangeOctopusMerge checks that what an octopus merge adds, its
// content and the paths of its files, is scanned even when a later commit
// removes it: git's remerge diff covers two parents only.
func TestScanRangeOctopusMerge(t *testing.T) {
	dir := gitRepo(t, cleanText, "feat: add b")
	for _, name := range []string{"one", "two"} {
		gitIn(t, dir, "checkout", "-q", "-b", name, "feature")
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(cleanText+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "add", ".")
		gitIn(t, dir, "commit", "-q", "-m", "feat: add "+name)
	}
	gitIn(t, dir, "checkout", "-q", "feature")
	gitIn(t, dir, "merge", "-q", "--no-ff", "--no-commit", "one", "two")
	name := "notes-" + clusterHost + ".md"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(leak+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "Merge one and two")
	merge := gitIn(t, dir, "rev-parse", headRef)
	gitIn(t, dir, "rm", "-q", name)
	gitIn(t, dir, "commit", "-q", "-m", "chore: remove notes")
	f := scanToMain(t, dir)
	var where []string
	for _, x := range f {
		where = append(where, x.Where)
	}
	slices.Sort(where)
	want := []string{
		"commit " + short(merge) + " " + name + ":1",
		"path of an added file",
	}
	if !slices.Equal(where, want) {
		t.Fatalf("findings = %+v, want %q", f, want)
	}
}

// TestPrePushNestedTag checks that a pushed tag pointing to another tag
// object has the inner tag's name and message scanned too.
func TestPrePushNestedTag(t *testing.T) {
	dir := gitRepo(t, cleanText, "feat: add b")
	gitIn(t, dir, "tag", "-a", "inner", "-m", "release\n\n"+leak, "origin/main")
	gitIn(t, dir, "tag", "-a", "outer", "-m", "release", "inner")
	line := "refs/tags/outer " + gitIn(t, dir, "rev-parse", "refs/tags/outer") + " refs/tags/outer " + strings.Repeat("0", 40) + "\n"
	f, err := NewScanner().prePush(context.Background(), Git{Dir: dir}, []string{remoteOrigin}, strings.NewReader(line))
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Where != "tag inner message line 3" {
		t.Fatalf("findings = %+v, want one in the inner tag's message", f)
	}
}

// TestGitVersionAtLeast checks the git version the scan requires.
func TestGitVersionAtLeast(t *testing.T) {
	for out, ok := range map[string]bool{
		"git version 2.54.0 (Apple Git-157)\n": true,
		"git version 2.40.0":                   true,
		"git version 3.0.1":                    true,
		"git version 2.39.5 (Apple Git-154)":   false,
		"git version 1.9":                      false,
		"not git":                              false,
	} {
		if err := gitVersionAtLeast(out, minGitVersion); (err == nil) != ok {
			t.Errorf("gitVersionAtLeast(%q) = %v, want ok=%v", out, err, ok)
		}
	}
}
