package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// denyVariable is the GitHub Actions repository variable that holds the deny
// list: names that must not be published but cannot be written down in this
// repository either. One regular expression per line, # starts a comment.
// Only collaborators can read a repository variable.
const denyVariable = "CHECKPUBLIC_DENY"

const (
	// denyCacheTTL is how long a fetched deny list is used before it is
	// fetched again.
	denyCacheTTL = 24 * time.Hour
	// denyFetchTimeout keeps a push from hanging on a slow network.
	denyFetchTimeout = 5 * time.Second
)

// commandRunner runs a program and returns its standard output.
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // gh with arguments built by this tool, no shell
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1")
	return cmd.Output()
}

// denySource loads the deny list. Its fields are injected so tests control
// the gh call, the clock and the cache directory.
type denySource struct {
	// inCI is true in GitHub Actions. CI never reads the deny list: a
	// variable passed to a workflow of this public repository would print
	// in its public log, and the matched text in a finding would too.
	inCI bool
	// repo is the owner/name the variable belongs to, from the origin
	// remote of the scanned repository.
	repo string
	// extra is more patterns from the CHECKPUBLIC_DENY environment variable.
	extra    string
	run      commandRunner
	now      func() time.Time
	cacheDir string
	stderr   io.Writer
}

// newDenySource reads the environment of a real run. Tests replace it so
// they never call gh.
var newDenySource = func(inCI bool, dir string, stderr io.Writer) denySource {
	cacheDir := ""
	if d, err := os.UserCacheDir(); err == nil {
		cacheDir = filepath.Join(d, "checkpublic")
	}
	return denySource{
		inCI:     inCI,
		repo:     gitRepoOf(dir),
		extra:    os.Getenv(denyVariable),
		run:      execRunner,
		now:      time.Now,
		cacheDir: cacheDir,
		stderr:   stderr,
	}
}

// patterns returns the deny list. It never fails: when the list cannot be
// read it warns once and the other rules still apply. It never prints the
// patterns.
func (d denySource) patterns(ctx context.Context) []string {
	if d.inCI {
		return nil
	}
	text, ok := d.variable(ctx)
	if !ok {
		logf(d.stderr, "checkpublic: warning: the repository deny list is unavailable (could not read the %s variable with gh); the other rules still apply.\n", denyVariable)
	}
	return d.parse(text + "\n" + d.extra)
}

// variable returns the variable's value from a fresh cache, from gh, or from a
// stale cache when gh fails.
func (d denySource) variable(ctx context.Context) (string, bool) {
	cache := d.cachePath()
	cached, fresh, haveCache := d.readCache(cache)
	if haveCache && fresh {
		return cached, true
	}
	if d.repo != "" {
		ctx, cancel := context.WithTimeout(ctx, denyFetchTimeout)
		defer cancel()
		out, err := d.run(ctx, "gh", "variable", "get", denyVariable, "-R", d.repo)
		if err == nil {
			d.writeCache(cache, out)
			return string(out), true
		}
	}
	return cached, haveCache
}

var unsafePathChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func (d denySource) cachePath() string {
	if d.cacheDir == "" || d.repo == "" {
		return ""
	}
	return filepath.Join(d.cacheDir, "deny-"+unsafePathChars.ReplaceAllString(strings.ToLower(d.repo), "_"))
}

func (d denySource) readCache(path string) (text string, fresh, ok bool) {
	if path == "" {
		return "", false, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", false, false
	}
	b, err := os.ReadFile(path) //nolint:gosec // this tool's own cache file
	if err != nil {
		return "", false, false
	}
	return string(b), d.now().Sub(info.ModTime()) < denyCacheTTL, true
}

// writeCache stores the list readable only by the user. A failed write only
// means the next run fetches again.
func (d denySource) writeCache(path string, b []byte) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".deny-*")
	if err != nil {
		return
	}
	_, err = tmp.Write(b)
	err = errors.Join(err, tmp.Chmod(0o600), tmp.Close())
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	// The cache's age is its modification time, on the injected clock.
	now := d.now()
	_ = os.Chtimes(path, now, now)
}

// parse returns the patterns in text, dropping comments, blank lines and
// patterns that do not compile (with a warning that does not show them).
func (d denySource) parse(text string) []string {
	var out []string
	bad := 0
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, err := regexp.Compile(line); err != nil {
			bad++
			continue
		}
		out = append(out, line)
	}
	if bad > 0 {
		logf(d.stderr, "checkpublic: warning: skipped %d deny list pattern(s) that are not valid regular expressions.\n", bad)
	}
	return out
}
