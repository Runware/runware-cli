package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMain keeps every test from calling gh: run() loads the deny list
// through newDenySource. It also runs the tests logged out of gh and outside
// GitHub Actions, so a test that reaches a real gh by mistake fails to
// authenticate instead of acting as the developer, and a CI environment
// does not change the defaults the tests expect.
func TestMain(m *testing.M) {
	newDenySource = func(_ bool, _ string, stderr io.Writer) denySource {
		return denySource{
			inCI:   true,
			stderr: stderr,
		}
	}
	os.Exit(runLoggedOut(m))
}

func runLoggedOut(m *testing.M) int {
	dir, err := os.MkdirTemp("", "checkpublic-gh-config-")
	if err != nil {
		logln(os.Stderr, "TestMain:", err)
		return 1
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()
	for _, k := range []string{
		"GH_TOKEN",
		"GITHUB_TOKEN",
		"GH_ENTERPRISE_TOKEN",
		"GITHUB_ENTERPRISE_TOKEN",
		"GH_REPO",
		"GITHUB_ACTIONS",
		"GITHUB_STEP_SUMMARY",
		"GITHUB_EVENT_PATH",
		denyVariable,
	} {
		_ = os.Unsetenv(k)
	}
	_ = os.Setenv("GH_CONFIG_DIR", dir)
	_ = os.Setenv("GH_PROMPT_DISABLED", "1")
	// git reads no configuration of the user or the system, so a test
	// sees the same git everywhere, and nothing writes the user's cache.
	_ = os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	_ = os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	cache := filepath.Join(dir, "cache")
	if os.Getenv("GOCACHE") == "" {
		// Keep go's build cache, which make setup's go build uses, where
		// it was: it moves with XDG_CACHE_HOME otherwise.
		if out, err := exec.CommandContext(context.Background(), "go", "env", "GOCACHE").Output(); err == nil {
			_ = os.Setenv("GOCACHE", strings.TrimSpace(string(out)))
		}
	}
	_ = os.Setenv("XDG_CACHE_HOME", cache)
	return m.Run()
}

const testRepo = "owner/repo"

// TestHermeticEnvironment checks that TestMain keeps the tests away from
// the user's git configuration and cache.
func TestHermeticEnvironment(t *testing.T) {
	if got := os.Getenv("GIT_CONFIG_GLOBAL"); got != os.DevNull {
		t.Errorf("GIT_CONFIG_GLOBAL = %q, want %q", got, os.DevNull)
	}
	if got := os.Getenv("GIT_CONFIG_NOSYSTEM"); got != "1" {
		t.Errorf("GIT_CONFIG_NOSYSTEM = %q, want 1", got)
	}
	if got := os.Getenv("XDG_CACHE_HOME"); !strings.HasPrefix(got, os.Getenv("GH_CONFIG_DIR")) || os.Getenv("GH_CONFIG_DIR") == "" {
		t.Errorf("XDG_CACHE_HOME = %q, want it under the tests' temporary directory", got)
	}
	// The hook a setup test pushes with runs the real deny source, which
	// must cache under XDG_CACHE_HOME on every system.
	if got, want := userCacheDir(), filepath.Join(os.Getenv("XDG_CACHE_HOME"), "checkpublic"); got != want {
		t.Errorf("userCacheDir() = %q, want %q", got, want)
	}
}

// patternStrings returns the source text of each pattern.
func patternStrings(res []*regexp.Regexp) []string {
	var out []string
	for _, re := range res {
		out = append(out, re.String())
	}
	return out
}

// fetchedList is the deny list the fake gh returns.
const (
	fetchedPattern = "fetched"
	fetchedList    = fetchedPattern + "\n"
)

// fakeGH records gh calls and answers with out or err.
type fakeGH struct {
	calls int
	out   string
	err   error
}

func (f *fakeGH) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls++
	want := []string{"variable", "get", denyVariable, "-R", testRepo}
	if name != "gh" || !slices.Equal(args, want) {
		return nil, errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
	}
	if f.err != nil {
		return nil, f.err
	}
	return []byte(f.out), nil
}

var testNow = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

func newTestDeny(t *testing.T, gh *fakeGH, stderr *bytes.Buffer) denySource {
	t.Helper()
	return denySource{
		repo:     testRepo,
		run:      gh.run,
		now:      func() time.Time { return testNow },
		cacheDir: t.TempDir(),
		stderr:   stderr,
	}
}

// writeTestCache writes a cached list as of age ago.
func writeTestCache(t *testing.T, d denySource, text string, age time.Duration) {
	t.Helper()
	path := d.cachePath()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	at := testNow.Add(-age)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestDenyFreshCache(t *testing.T) {
	var stderr bytes.Buffer
	gh := &fakeGH{
		out: fetchedList,
	}
	d := newTestDeny(t, gh, &stderr)
	writeTestCache(t, d, "# comment\ncached\n\n", time.Hour)
	if got := patternStrings(d.patterns(context.Background())); !slices.Equal(got, []string{"cached"}) {
		t.Errorf("patterns = %q, want the cached list", got)
	}
	if gh.calls != 0 {
		t.Errorf("gh called %d times with a fresh cache", gh.calls)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", stderr.String())
	}
}

func TestDenyExpiredCacheRefetches(t *testing.T) {
	var stderr bytes.Buffer
	gh := &fakeGH{
		out: fetchedList,
	}
	d := newTestDeny(t, gh, &stderr)
	writeTestCache(t, d, "cached\n", 25*time.Hour)
	if got := patternStrings(d.patterns(context.Background())); !slices.Equal(got, []string{fetchedPattern}) {
		t.Errorf("patterns = %q, want the fetched list", got)
	}
	if gh.calls != 1 {
		t.Errorf("gh called %d times, want 1", gh.calls)
	}
	info, err := os.Stat(d.cachePath())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache mode = %v, want 0600", perm)
	}
	if !info.ModTime().Equal(testNow) {
		t.Errorf("cache time = %v, want %v", info.ModTime(), testNow)
	}
	// The refreshed cache is fresh: the next run does not call gh.
	if got := patternStrings(d.patterns(context.Background())); !slices.Equal(got, []string{fetchedPattern}) || gh.calls != 1 {
		t.Errorf("second run: patterns = %q, gh calls = %d", got, gh.calls)
	}
}

func TestDenyFetchFailureUsesStaleCache(t *testing.T) {
	var stderr bytes.Buffer
	gh := &fakeGH{
		err: errors.New("HTTP 404"),
	}
	d := newTestDeny(t, gh, &stderr)
	writeTestCache(t, d, "cached\n", 72*time.Hour)
	if got := patternStrings(d.patterns(context.Background())); !slices.Equal(got, []string{"cached"}) {
		t.Errorf("patterns = %q, want the stale list", got)
	}
	if !strings.Contains(stderr.String(), "72h0m0s ago") || strings.Contains(stderr.String(), "cached") {
		t.Errorf("stderr = %q, want a warning with the cache's age and not the list", stderr.String())
	}
}

func TestDenyFetchFailureWithoutCacheWarns(t *testing.T) {
	var stderr bytes.Buffer
	gh := &fakeGH{
		err: errors.New("gh: not logged in"),
	}
	d := newTestDeny(t, gh, &stderr)
	d.extra = "from-env"
	if got := patternStrings(d.patterns(context.Background())); !slices.Equal(got, []string{"from-env"}) {
		t.Errorf("patterns = %q, want only the environment's", got)
	}
	if !strings.Contains(stderr.String(), "deny list is unavailable") || strings.Count(stderr.String(), "\n") != 1 {
		t.Errorf("stderr = %q, want one warning line", stderr.String())
	}
	if _, err := os.Stat(d.cachePath()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a failed fetch wrote a cache: %v", err)
	}

	// The scan goes on with every other rule.
	s := NewScanner()
	s.loadDeny = func() []*regexp.Regexp {
		return d.patterns(context.Background())
	}
	if f := s.ScanText("PR title", "fix: "+clusterHost); len(f) != 1 || f[0].Rule != ruleInternalHost {
		t.Errorf("findings = %+v, want the built-in rule", f)
	}
}

func TestDenyInvalidPatternWarnsWithoutShowingIt(t *testing.T) {
	var stderr bytes.Buffer
	gh := &fakeGH{
		out: "secret-name(\nok\n",
	}
	d := newTestDeny(t, gh, &stderr)
	if got := patternStrings(d.patterns(context.Background())); !slices.Equal(got, []string{"ok"}) {
		t.Errorf("patterns = %q, want the valid one", got)
	}
	if strings.Contains(stderr.String(), "secret-name") || !strings.Contains(stderr.String(), "skipped 1") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

// TestDenyControlCharacterSkipped checks that a pattern holding a backspace,
// as echo writes \b, is skipped with a warning rather than matching nothing
// in silence.
func TestDenyControlCharacterSkipped(t *testing.T) {
	var stderr bytes.Buffer
	gh := &fakeGH{
		out: "\bname #[0-9]+\nok\n",
	}
	d := newTestDeny(t, gh, &stderr)
	if got := patternStrings(d.patterns(context.Background())); !slices.Equal(got, []string{"ok"}) {
		t.Errorf("patterns = %q, want the one without a control character", got)
	}
	if !strings.Contains(stderr.String(), "skipped 1") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestDenyEmptyMatchPatternSkipped(t *testing.T) {
	var stderr bytes.Buffer
	gh := &fakeGH{
		out: "hidden-name|\n(other)?\nok\n",
	}
	d := newTestDeny(t, gh, &stderr)
	if got := patternStrings(d.patterns(context.Background())); !slices.Equal(got, []string{"ok"}) {
		t.Errorf("patterns = %q, want only the one that cannot match empty text", got)
	}
	if strings.Contains(stderr.String(), "hidden-name") || strings.Contains(stderr.String(), "other") || !strings.Contains(stderr.String(), "skipped 2") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestDenyCISkips(t *testing.T) {
	var stderr bytes.Buffer
	gh := &fakeGH{
		out: fetchedList,
	}
	d := newTestDeny(t, gh, &stderr)
	d.inCI = true
	d.extra = "from-env"
	writeTestCache(t, d, "cached\n", time.Hour)
	if got := d.patterns(context.Background()); got != nil {
		t.Errorf("patterns = %q in CI, want none", got)
	}
	if gh.calls != 0 || stderr.Len() != 0 {
		t.Errorf("CI: gh calls = %d, stderr = %q", gh.calls, stderr.String())
	}
}

// TestDenyNotLoadedWithoutScan checks that a push that publishes nothing
// here, a deletion or a push to a private repository, never loads the deny
// list, so it never waits on gh.
func TestDenyNotLoadedWithoutScan(t *testing.T) {
	s := NewScanner()
	s.loadDeny = func() []*regexp.Regexp {
		t.Error("the deny list was loaded")
		return nil
	}
	zero := strings.Repeat("0", 40)
	g := Git{
		Dir: t.TempDir(),
	}
	deletion := "(delete) " + zero + " refs/heads/feature " + strings.Repeat("1", 40) + "\n"
	if f, err := s.prePush(context.Background(), g, []string{remoteOrigin}, strings.NewReader(deletion)); err != nil || len(f) != 0 {
		t.Fatalf("prePush of a deletion = %+v, %v", f, err)
	}
	private := "https://github.com/" + shortRepo + "some-private-repo.git"
	if f, err := s.prePush(context.Background(), g, []string{remoteOrigin, private}, strings.NewReader("refs/heads/a b refs/heads/a "+zero+"\n")); err != nil || len(f) != 0 {
		t.Fatalf("prePush to a private repository = %+v, %v", f, err)
	}
}

// TestGitHubRepoHostAlias checks that an SSH host alias for GitHub, such as
// a Host entry in ~/.ssh/config, still gives the repository, so the deny
// list is read from it.
func TestGitHubRepoHostAlias(t *testing.T) {
	for url, want := range map[string]string{
		"git@github-work:owner/repo.git":         testRepo,
		"ssh://git@github.com:22/owner/repo.git": testRepo,
		"https://github.com/owner/repo":          testRepo,
		"git@gitlab.example.com:owner/repo.git":  "",
		"/srv/git/repo.git":                      "",
	} {
		if got := githubRepo(url); got != want {
			t.Errorf("githubRepo(%q) = %q, want %q", url, got, want)
		}
	}

	dir := cleanRepo(t)
	gitIn(t, dir, "remote", "add", "origin", "git@github-work:owner/repo.git")
	var stderr bytes.Buffer
	gh := &fakeGH{
		out: fetchedList,
	}
	d := newTestDeny(t, gh, &stderr)
	d.repo = gitRepoOf(dir)
	if got := patternStrings(d.patterns(context.Background())); !slices.Equal(got, []string{fetchedPattern}) || gh.calls != 1 {
		t.Errorf("patterns = %q, gh calls = %d, want the list fetched with -R %s", got, gh.calls, testRepo)
	}
}

// TestDenyUnknownRepoWarns checks the warning when the repository cannot be
// told from the origin remote: it says so, and gh is not called.
func TestDenyUnknownRepoWarns(t *testing.T) {
	var stderr bytes.Buffer
	gh := &fakeGH{}
	d := newTestDeny(t, gh, &stderr)
	d.repo = ""
	d.patterns(context.Background())
	if !strings.Contains(stderr.String(), "could not tell the GitHub repository") || strings.Count(stderr.String(), "\n") != 1 || gh.calls != 0 {
		t.Fatalf("stderr = %q, gh calls = %d, want one warning that the repository is unknown", stderr.String(), gh.calls)
	}
}
