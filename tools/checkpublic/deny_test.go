package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMain keeps every test from calling gh: run() loads the deny list
// through newDenySource.
func TestMain(m *testing.M) {
	newDenySource = func(_ bool, _ string, stderr io.Writer) denySource {
		return denySource{
			inCI:   true,
			stderr: stderr,
		}
	}
	os.Exit(m.Run())
}

const testRepo = "owner/repo"

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
		out: "fetched\n",
	}
	d := newTestDeny(t, gh, &stderr)
	writeTestCache(t, d, "# comment\ncached\n\n", time.Hour)
	if got := d.patterns(context.Background()); !slices.Equal(got, []string{"cached"}) {
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
		out: "fetched\n",
	}
	d := newTestDeny(t, gh, &stderr)
	writeTestCache(t, d, "cached\n", 25*time.Hour)
	if got := d.patterns(context.Background()); !slices.Equal(got, []string{"fetched"}) {
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
	if got := d.patterns(context.Background()); !slices.Equal(got, []string{"fetched"}) || gh.calls != 1 {
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
	if got := d.patterns(context.Background()); !slices.Equal(got, []string{"cached"}) {
		t.Errorf("patterns = %q, want the stale list", got)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", stderr.String())
	}
}

func TestDenyFetchFailureWithoutCacheWarns(t *testing.T) {
	var stderr bytes.Buffer
	gh := &fakeGH{
		err: errors.New("gh: not logged in"),
	}
	d := newTestDeny(t, gh, &stderr)
	d.extra = "from-env"
	if got := d.patterns(context.Background()); !slices.Equal(got, []string{"from-env"}) {
		t.Errorf("patterns = %q, want only the environment's", got)
	}
	if !strings.Contains(stderr.String(), "deny list is unavailable") || strings.Count(stderr.String(), "\n") != 1 {
		t.Errorf("stderr = %q, want one warning line", stderr.String())
	}
	if _, err := os.Stat(d.cachePath()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a failed fetch wrote a cache: %v", err)
	}

	// The scan goes on with every other rule.
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	s.loadDeny = func() []string {
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
	if got := d.patterns(context.Background()); !slices.Equal(got, []string{"ok"}) {
		t.Errorf("patterns = %q, want the valid one", got)
	}
	if strings.Contains(stderr.String(), "secret-name") || !strings.Contains(stderr.String(), "skipped 1") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestDenyCISkips(t *testing.T) {
	var stderr bytes.Buffer
	gh := &fakeGH{
		out: "fetched\n",
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

// TestDenyNotLoadedWithoutScan checks that a hook call for a command that
// publishes nothing never loads the deny list.
func TestDenyNotLoadedWithoutScan(t *testing.T) {
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	s.loadDeny = func() []string {
		t.Error("the deny list was loaded")
		return nil
	}
	payload := []byte(`{"tool_name":"Bash","tool_input":{"command":"git log --grep push"},"cwd":"` + filepath.ToSlash(t.TempDir()) + `"}`)
	if f, err := s.runHook(context.Background(), payload, gitRepoOf); err != nil || len(f) != 0 {
		t.Fatalf("runHook = %+v, %v", f, err)
	}
}
