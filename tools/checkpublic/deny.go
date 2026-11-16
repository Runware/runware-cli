package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	// denyRetryAfter is how long after a failed fetch the next run skips
	// gh, so that pushes without access or offline do not each wait on it.
	denyRetryAfter = 10 * time.Minute
	// denyWarnEvery is how often the same warning about the deny list is
	// printed for a repository: once a day is enough to notice it.
	denyWarnEvery = 24 * time.Hour
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
	// gitDir is the clone's common git directory. It keys the warning
	// marker when repo is "".
	gitDir string
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
		gitDir:   gitCommonDir(dir),
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
// read the other rules still apply, with a warning at most once a day for
// the repository. It never prints the patterns.
func (d denySource) patterns(ctx context.Context) []*regexp.Regexp {
	if d.inCI {
		return nil
	}
	text, age, ok := d.variable(ctx)
	stale := ok && age >= denyCacheTTL
	if (!ok || stale) && d.shouldWarn() {
		switch {
		case ok:
			logf(d.stderr, "checkpublic: warning: could not refresh the repository deny list with gh; using the copy saved %s ago, which misses names added since.\n", age.Round(time.Hour))
		case d.repo == "":
			logf(d.stderr, "checkpublic: warning: the repository deny list is unavailable: could not tell the GitHub repository from the origin remote, so the %s variable was not read; the other rules still apply.\n", denyVariable)
		default:
			logf(d.stderr, "checkpublic: warning: the repository deny list is unavailable (could not read the %s variable with gh); the other rules still apply.\n", denyVariable)
		}
	}
	return d.parse(text + "\n" + d.extra)
}

// variable returns the variable's value from a fresh cache, from gh, or from a
// stale cache when gh fails, with the age of the cache it used. After a
// failed fetch it does not call gh again for denyRetryAfter.
func (d denySource) variable(ctx context.Context) (string, time.Duration, bool) {
	cache := d.cachePath()
	cached, age, haveCache := d.readCache(cache)
	if haveCache && age < denyCacheTTL {
		return cached, age, true
	}
	failed := d.markerPath("failed")
	if d.repo != "" && !d.markedWithin(failed, denyRetryAfter) {
		ctx, cancel := context.WithTimeout(ctx, denyFetchTimeout)
		defer cancel()
		out, err := d.run(ctx, "gh", "variable", "get", denyVariable, "-R", d.repo)
		if err == nil {
			d.writeCache(cache, out)
			if failed != "" {
				_ = os.Remove(failed)
			}
			return string(out), 0, true
		}
		d.mark(failed)
	}
	return cached, age, haveCache
}

// shouldWarn reports whether to print a deny list warning now: unless one
// was printed for the repository within denyWarnEvery. It records the
// warning it allows.
func (d denySource) shouldWarn() bool {
	warned := d.markerPath("warned")
	if d.markedWithin(warned, denyWarnEvery) {
		return false
	}
	d.mark(warned)
	return true
}

// markerPath returns the path of a file in the cache directory for kind and
// the repository, or for the clone when the repository is unknown, or ""
// when there is neither or no cache directory. An empty marker's
// modification time records when kind last happened.
func (d denySource) markerPath(kind string) string {
	key := strings.ToLower(d.repo)
	if key == "" && d.gitDir != "" {
		sum := sha256.Sum256([]byte(d.gitDir))
		key = "clone-" + hex.EncodeToString(sum[:8])
	}
	if d.cacheDir == "" || key == "" {
		return ""
	}
	return filepath.Join(d.cacheDir, kind+"-"+unsafePathChars.ReplaceAllString(key, "_"))
}

// markedWithin reports whether the marker at path was set less than ttl ago.
func (d denySource) markedWithin(path string, ttl time.Duration) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && d.now().Sub(info.ModTime()) < ttl
}

// mark sets the marker at path to now. A failed write only means the next
// run warns or fetches again.
func (d denySource) mark(path string) {
	if path == "" || os.MkdirAll(filepath.Dir(path), 0o700) != nil || os.WriteFile(path, nil, 0o600) != nil {
		return
	}
	now := d.now()
	_ = os.Chtimes(path, now, now)
}

var unsafePathChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func (d denySource) cachePath() string {
	return d.markerPath("deny")
}

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

// parse returns the patterns in text, dropping comments, blank lines and
// patterns that do not compile (with a warning that does not show them).
func (d denySource) parse(text string) []*regexp.Regexp {
	var out []*regexp.Regexp
	bad := 0
	for _, line := range listLines(text) {
		re, err := regexp.Compile(line)
		if err != nil {
			bad++
			continue
		}
		out = append(out, re)
	}
	if bad > 0 {
		logf(d.stderr, "checkpublic: warning: skipped %d deny list pattern(s) that are not valid regular expressions.\n", bad)
	}
	return out
}
