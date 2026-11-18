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
	// remote of the scanned repository, or "" when that is not a GitHub
	// remote.
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
	return denySource{
		inCI:     inCI,
		repo:     gitRepoOf(dir),
		extra:    os.Getenv(denyVariable),
		run:      execRunner,
		now:      time.Now,
		cacheDir: userCacheDir(),
		stderr:   stderr,
	}
}

// userCacheDir returns the directory the deny list is cached in, or "".
// XDG_CACHE_HOME applies on every system, not only where os.UserCacheDir
// reads it, so a test run keeps the hook it runs out of the user's cache.
func userCacheDir() string {
	d := os.Getenv("XDG_CACHE_HOME")
	if !filepath.IsAbs(d) {
		var err error
		if d, err = os.UserCacheDir(); err != nil {
			return ""
		}
	}
	return filepath.Join(d, "checkpublic")
}

// patterns returns the deny list. It never fails: when the list cannot be
// read the other rules still apply, with a warning. It never prints the
// patterns.
func (d denySource) patterns(ctx context.Context) []*regexp.Regexp {
	if d.inCI {
		return nil
	}
	text, age, ok := d.variable(ctx)
	switch {
	case ok && age >= denyCacheTTL:
		logf(d.stderr, "checkpublic: warning: could not refresh the repository deny list with gh; using the copy saved %s ago, which misses names added since.\n", age.Round(time.Hour))
	case ok:
	case d.repo == "":
		logf(d.stderr, "checkpublic: warning: the repository deny list is unavailable: could not tell the GitHub repository from the origin remote, so the %s variable was not read; the other rules still apply.\n", denyVariable)
	default:
		logf(d.stderr, "checkpublic: warning: the repository deny list is unavailable (could not read the %s variable with gh); the other rules still apply.\n", denyVariable)
	}
	return d.parse(text + "\n" + d.extra)
}

// variable returns the variable's value from a fresh cache, from gh, or from a
// stale cache when gh fails, with the age of the cache it used.
func (d denySource) variable(ctx context.Context) (string, time.Duration, bool) {
	cache := d.cachePath()
	cached, age, haveCache := d.readCache(cache)
	if haveCache && age < denyCacheTTL {
		return cached, age, true
	}
	if d.repo != "" {
		ctx, cancel := context.WithTimeout(ctx, denyFetchTimeout)
		defer cancel()
		if out, err := d.run(ctx, "gh", "variable", "get", denyVariable, "-R", d.repo); err == nil {
			d.writeCache(cache, out)
			return string(out), 0, true
		}
	}
	return cached, age, haveCache
}

// cachePath returns the cache file of the repository, or "" when the
// repository is unknown or there is no cache directory.
func (d denySource) cachePath() string {
	if d.cacheDir == "" || d.repo == "" {
		return ""
	}
	return filepath.Join(d.cacheDir, "deny-"+unsafePathChars.ReplaceAllString(strings.ToLower(d.repo), "_"))
}

var unsafePathChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// readCache returns the cached list and its age.
func (d denySource) readCache(path string) (text string, age time.Duration, ok bool) {
	if path == "" {
		return "", 0, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, false
	}
	b, err := os.ReadFile(path) //nolint:gosec // this tool's own cache file
	if err != nil {
		return "", 0, false
	}
	return string(b), d.now().Sub(info.ModTime()), true
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

// parse returns the patterns in text, dropping comments, blank lines,
// patterns that do not compile and patterns that match the empty string,
// which would match every line (with a warning that does not show them).
func (d denySource) parse(text string) []*regexp.Regexp {
	var out []*regexp.Regexp
	bad := 0
	for _, line := range listLines(text) {
		re, err := regexp.Compile(line)
		if err != nil || re.MatchString("") {
			bad++
			continue
		}
		out = append(out, re)
	}
	if bad > 0 {
		logf(d.stderr, "checkpublic: warning: skipped %d deny list pattern(s) that are not valid regular expressions or match empty text.\n", bad)
	}
	return out
}
