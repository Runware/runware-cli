package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	remoteOrigin = "origin"
	cleanText    = "clean text"
	headRef      = "HEAD"
	originMain   = "origin/main"
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
	gitIn(t, dir, "tag", "-a", "v1.0.0", "-m", "release\n\n"+leak, originMain)
	gitIn(t, dir, "tag", "v0.9.0", originMain)
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

	hostTag := "v1-" + clusterHost
	gitIn(t, dir, "tag", hostTag, originMain)
	f, err = s.prePush(ctx, g, []string{remoteOrigin}, strings.NewReader(line(hostTag)))
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Where != "tag name" {
		t.Fatalf("findings for a tag named with an internal host = %+v, want one in the name", f)
	}
}

// TestPrePushBranchName checks that the name a branch is pushed to is
// scanned, even when its content is clean.
func TestPrePushBranchName(t *testing.T) {
	dir := cleanRepo(t)
	head := gitIn(t, dir, "rev-parse", headRef)
	line := "refs/heads/feature " + head + " refs/heads/fix/" + clusterHost + " " + strings.Repeat("0", 40) + "\n"
	f, err := NewScanner().prePush(context.Background(), Git{Dir: dir}, []string{remoteOrigin}, strings.NewReader(line))
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Where != "branch name" {
		t.Fatalf("findings = %+v, want one in the branch name", f)
	}
}

// fromPublishedBranch makes a repository whose feature branch, with a leak
// in b.txt and in its commit message, is on origin already, and whose next
// branch adds one commit to it, with text in c.txt. origin is a configured
// remote, as in a clone.
func fromPublishedBranch(t *testing.T, text string) string {
	t.Helper()
	dir := gitRepo(t, leak, "feat: add b\n\n"+leak)
	gitIn(t, dir, "remote", "add", remoteOrigin, "https://example.com/owner/repo.git")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/feature", headRef)
	gitIn(t, dir, "checkout", "-q", "-b", "next")
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte(text+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: add c")
	return dir
}

// publishedCase is the text a commit on a published branch adds, and where
// a push of it should have findings.
type publishedCase struct {
	text string
	want []string
}

// TestPrePushFromPublishedBranch checks a branch and a tag on a commit made
// on top of a branch the remote has: what that branch published is not
// the push's to fix, and what the new commit adds is still found.
func TestPrePushFromPublishedBranch(t *testing.T) {
	zero := strings.Repeat("0", 40)
	for _, tc := range []publishedCase{
		{
			text: cleanText,
			want: nil,
		},
		{
			text: leak,
			want: []string{"c.txt:1"},
		},
	} {
		dir := fromPublishedBranch(t, tc.text)
		gitIn(t, dir, "tag", "v2.0.0", headRef)
		head := gitIn(t, dir, "rev-parse", headRef)
		for _, line := range []string{
			"refs/heads/next " + head + " refs/heads/next " + zero + "\n",
			"refs/tags/v2.0.0 " + head + " refs/tags/v2.0.0 " + zero + "\n",
		} {
			f, err := NewScanner().prePush(context.Background(), Git{Dir: dir}, []string{remoteOrigin}, strings.NewReader(line))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, fd := range f {
				got = append(got, fd.Where)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("push %q with c.txt %q: findings = %+v, want %v", strings.Fields(line)[0], tc.text, f, tc.want)
			}
		}
	}
}

// TestScanRangeMessageSeparators checks that a commit message holding the
// control characters a record format might use cannot hide text after them.
func TestScanRangeMessageSeparators(t *testing.T) {
	for _, sep := range []string{"\x1e", "\x1f", "\x1f\x1e"} {
		dir := gitRepo(t, cleanText, "feat: add b\n\nnote "+sep+leak)
		f, err := NewScanner().ScanRange(context.Background(), Git{Dir: dir}, Range{
			Base: originMain,
			Head: headRef,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(f) != 1 || !strings.HasSuffix(f[0].Where, " message line 3") {
			t.Errorf("separator %q: findings = %+v, want one in the message", sep, f)
		}
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
		Base: originMain,
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
	gitIn(t, dir, "checkout", "-q", "-b", "newmain", originMain)
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
			Base: originMain,
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
		Base:   originMain,
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
	gitIn(t, dir, "checkout", "-q", "-b", "newmain", originMain)
	if err := os.WriteFile(filepath.Join(dir, "m.txt"), []byte(mainText+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: main work")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/main", headRef)
	gitIn(t, dir, "checkout", "-q", "feature")
	gitIn(t, dir, "merge", "-q", "--no-ff", "--no-commit", originMain)
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

// TestScanFailsOnTooLongLine checks that a line of git output longer than
// the scanner reads fails the run, rather than leave what follows it
// unscanned.
func TestScanFailsOnTooLongLine(t *testing.T) {
	old := maxLineBytes
	maxLineBytes = 1 << 10
	t.Cleanup(func() {
		maxLineBytes = old
	})
	dir := gitRepo(t, strings.Repeat("x", 2<<10)+"\n"+leak, "feat: add b")
	var stdout, stderr strings.Builder
	code := run(context.Background(), []string{modeRange, "-C", dir}, strings.NewReader(""), &stdout, &stderr)
	if code != exitError || !strings.Contains(stderr.String(), "too long to scan") {
		t.Fatalf("exit %d, want %d with a clear message:\n%s%s", code, exitError, stdout.String(), stderr.String())
	}
}

// orphanBranch adds a branch with no history in common with main, whose one
// commit holds text in o.txt.
func orphanBranch(t *testing.T, dir, name, text string) string {
	t.Helper()
	gitIn(t, dir, "checkout", "-q", "--orphan", name)
	gitIn(t, dir, "rm", "-rqf", ".")
	if err := os.WriteFile(filepath.Join(dir, "o.txt"), []byte(text+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "o.txt")
	gitIn(t, dir, "commit", "-q", "-m", "docs: start "+name)
	return gitIn(t, dir, "rev-parse", headRef)
}

// TestPrePushUnrelatedHistory checks that a pushed branch with no history
// in common with main, such as an orphan branch, has every commit the
// remote does not have scanned, instead of failing.
func TestPrePushUnrelatedHistory(t *testing.T) {
	dir := cleanRepo(t)
	zero := strings.Repeat("0", 40)
	s := NewScanner()
	g := Git{
		Dir: dir,
	}
	leaky := orphanBranch(t, dir, "leaky", leak)
	f, err := s.prePush(context.Background(), g, []string{remoteOrigin}, strings.NewReader("refs/heads/leaky "+leaky+" refs/heads/leaky "+zero+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Where != "o.txt:1" {
		t.Fatalf("findings = %+v, want one at o.txt:1", f)
	}
	gitIn(t, dir, "checkout", "-q", "feature")
	clean := orphanBranch(t, dir, "clean", cleanText)
	if f, err := s.prePush(context.Background(), g, []string{remoteOrigin}, strings.NewReader("refs/heads/clean "+clean+" refs/heads/clean "+zero+"\n")); err != nil || len(f) != 0 {
		t.Fatalf("findings for a clean orphan branch = %+v, %v", f, err)
	}
}

// TestPrePushTagOnBlobFails checks that a tag that does not lead to a commit
// fails the push rather than publish content the scan did not read.
func TestPrePushTagOnBlobFails(t *testing.T) {
	dir := cleanRepo(t)
	gitIn(t, dir, "tag", "blob-tag", "HEAD:b.txt")
	line := "refs/tags/blob-tag " + gitIn(t, dir, "rev-parse", "refs/tags/blob-tag") + " refs/tags/blob-tag " + strings.Repeat("0", 40) + "\n"
	if _, err := NewScanner().prePush(context.Background(), Git{Dir: dir}, []string{remoteOrigin}, strings.NewReader(line)); err == nil {
		t.Fatal("prePush of a tag on a blob succeeded, want an error")
	}
}

// TestScanTagNested checks that a tag pointing to another annotated tag fails
// the scan instead of leaving the inner tag's message unchecked, while a
// plain annotated tag still passes.
func TestScanTagNested(t *testing.T) {
	dir := cleanRepo(t)
	gitIn(t, dir, "tag", "-a", "inner", "-m", "release\n\n"+leak, headRef)
	gitIn(t, dir, "tag", "-a", "outer", "-m", "release", "inner")
	gitIn(t, dir, "tag", "-a", "plain", "-m", "release", headRef)
	g := Git{
		Dir: dir,
	}
	ctx := context.Background()
	s := NewScanner()
	outer := gitIn(t, dir, "rev-parse", "refs/tags/outer")
	if _, err := s.ScanTag(ctx, g, remoteOrigin, "outer", outer); err == nil || !strings.Contains(err.Error(), "nested annotated tags") {
		t.Fatalf("ScanTag of a nested tag: err = %v, want nested annotated tags", err)
	}
	plain := gitIn(t, dir, "rev-parse", "refs/tags/plain")
	if f, err := s.ScanTag(ctx, g, remoteOrigin, "plain", plain); err != nil || len(f) != 0 {
		t.Fatalf("ScanTag of a plain annotated tag = %+v, %v", f, err)
	}
}

// TestScanRangeOctopusMerge checks that a merge of more than two parents
// fails the scan, and that a merge of two does not.
func TestScanRangeOctopusMerge(t *testing.T) {
	dir := cleanRepo(t)
	for _, side := range []string{"side1", "side2"} {
		gitIn(t, dir, "checkout", "-q", "-b", side, originMain)
		if err := os.WriteFile(filepath.Join(dir, side+".txt"), []byte(cleanText+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "add", ".")
		gitIn(t, dir, "commit", "-q", "-m", "feat: "+side)
	}
	gitIn(t, dir, "checkout", "-q", "feature")
	gitIn(t, dir, "merge", "-q", "--no-ff", "-m", "Merge side1", "side1")
	g := Git{
		Dir: dir,
	}
	r := Range{
		Base: originMain,
		Head: headRef,
	}
	ctx := context.Background()
	if f, err := NewScanner().ScanRange(ctx, g, r); err != nil || len(f) != 0 {
		t.Fatalf("ScanRange with a two-parent merge = %+v, %v", f, err)
	}
	gitIn(t, dir, "reset", "-q", "--hard", "HEAD^")
	gitIn(t, dir, "merge", "-q", "--no-ff", "-m", "Merge sides", "side1", "side2")
	if _, err := NewScanner().ScanRange(ctx, g, r); err == nil || !strings.Contains(err.Error(), "more than two parents") {
		t.Fatalf("ScanRange with an octopus merge: err = %v, want more than two parents", err)
	}
}

// TestScanRangeOrphanWithShowRootOff checks that a user's log.showRoot=false
// does not hide the patch of a root commit: an orphan branch whose first
// commit adds a leak that the next commit removes is still found.
func TestScanRangeOrphanWithShowRootOff(t *testing.T) {
	dir := cleanRepo(t)
	gitIn(t, dir, "config", "log.showRoot", "false")
	gitIn(t, dir, "checkout", "-q", "--orphan", "fresh")
	gitIn(t, dir, "rm", "-q", "-rf", ".")
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte(leak+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: start over")
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte(cleanText+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-q", "-am", "fix: clean up")
	f, err := NewScanner().ScanRange(context.Background(), Git{Dir: dir}, Range{
		Head: headRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || !strings.HasPrefix(f[0].Where, "commit ") || !strings.HasSuffix(f[0].Where, " c.txt:1") {
		t.Fatalf("findings = %+v, want one in the root commit", f)
	}
}

// TestScanRangeSuppressBlankEmpty checks that a user's
// diff.suppressBlankEmpty and diff.interHunkContext, which put an empty
// context line inside a -U0 hunk, do not shift the lines read after it.
func TestScanRangeSuppressBlankEmpty(t *testing.T) {
	dir := cleanRepo(t)
	gitIn(t, dir, "config", "diff.suppressBlankEmpty", "true")
	gitIn(t, dir, "config", "diff.interHunkContext", "3")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-q", "-am", "feat: more lines")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/main", headRef)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("uno\n\nthree\n"+leak+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-q", "-am", "feat: change lines")
	f, err := NewScanner().ScanRange(context.Background(), Git{Dir: dir}, Range{
		Base: originMain,
		Head: headRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Where != "a.txt:4" {
		t.Fatalf("findings = %+v, want one on a.txt:4", f)
	}
}
