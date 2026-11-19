package put

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHashCacheKey(t *testing.T) {
	c := loadHashCache(t.TempDir())
	mod := time.Date(2026, time.November, 16, 9, 0, 0, 123, time.UTC)
	sum := sha256.Sum256([]byte("content"))
	c.store("/data/a.bin", 7, mod, 42, sum)

	if got, ok := c.lookup("/data/a.bin", 7, mod, 42); !ok || got != sum {
		t.Errorf("lookup of the same path, size and times = %x, %v", got, ok)
	}
	for name, miss := range map[string]func() bool{
		"another size":        func() bool { _, ok := c.lookup("/data/a.bin", 8, mod, 42); return ok },
		"a later time":        func() bool { _, ok := c.lookup("/data/a.bin", 7, mod.Add(time.Nanosecond), 42); return ok },
		"an earlier one":      func() bool { _, ok := c.lookup("/data/a.bin", 7, mod.Add(-time.Second), 42); return ok },
		"another change time": func() bool { _, ok := c.lookup("/data/a.bin", 7, mod, 43); return ok },
		"another path":        func() bool { _, ok := c.lookup("/data/b.bin", 7, mod, 42); return ok },
	} {
		if miss() {
			t.Errorf("the cache answered for %s", name)
		}
	}

	c.store("/data/c.bin", 7, mod, 0, sum)
	if _, ok := c.lookup("/data/c.bin", 7, mod, 0); ok {
		t.Error("the cache answered for a file with no change time")
	}

	c.forget("/data/a.bin")
	if _, ok := c.lookup("/data/a.bin", 7, mod, 42); ok {
		t.Error("the cache answered for a forgotten file")
	}
}

func TestHashCacheRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "volumes")
	c := loadHashCache(dir)
	mod := time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC)
	sum := sha256.Sum256([]byte("content"))
	c.store("/data/a.bin", 7, mod, 42, sum)
	if err := c.save(); err != nil {
		t.Fatal(err)
	}

	if got, ok := loadHashCache(dir).lookup("/data/a.bin", 7, mod, 42); !ok || got != sum {
		t.Errorf("a saved entry reads back as %x, %v", got, ok)
	}
}

func TestHashCacheKeepsNewestEntriesWithinLimit(t *testing.T) {
	dir := t.TempDir()
	c := loadHashCache(dir)
	for i := range maxCachedHashes {
		c.entries[strconv.Itoa(i)] = cacheEntry{StoredAt: int64(i)}
	}
	mod := time.Unix(1, 0)
	want := sha256.Sum256([]byte("new content"))
	c.store("/data/new.bin", 11, mod, 42, want)
	if err := c.save(); err != nil {
		t.Fatal(err)
	}

	reloaded := loadHashCache(dir)
	if reloaded.dirty || len(reloaded.entries) != maxCachedHashes {
		t.Fatalf("saved cache did not stay within %d entries", maxCachedHashes)
	}
	if _, ok := reloaded.entries["0"]; ok {
		t.Error("the oldest entry was not evicted")
	}
	if got, ok := reloaded.lookup("/data/new.bin", 11, mod, 42); !ok || got != want {
		t.Error("the most recently cached hash was lost")
	}
}

func TestHashCacheCorruptFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, hashCacheFile), []byte("{not json"))

	c := loadHashCache(dir)

	if len(c.entries) != 0 {
		t.Fatalf("a corrupt cache holds %d entries", len(c.entries))
	}
	c.store("/data/a.bin", 1, time.Unix(1, 0), 1, [32]byte{1})
	if err := c.save(); err != nil {
		t.Errorf("a corrupt cache could not be replaced: %v", err)
	}
}

func TestPutChecksContentWhenSourceMetadataMatches(t *testing.T) {
	e := newTestEnv(t)
	const name = "changed.bin"
	file := singleFile(t, e, name, []byte("new bytes"))
	sources, err := Walk([]string{file}, "")
	if err != nil {
		t.Fatal(err)
	}
	src := sources[0]
	if src.Change == 0 {
		t.Skip("this filesystem does not report a change time for cache hits")
	}
	cache := loadHashCache(e.stateDir())
	cache.store(src.Abs, src.Size, src.ModTime, src.Change, sha256.Sum256([]byte("old bytes")))
	if err := cache.save(); err != nil {
		t.Fatal(err)
	}

	_, err = e.put(e.cloud.newVolume("model-weights", ""), file)
	if !errors.Is(err, ErrSourceChanged) || !strings.Contains(err.Error(), name) {
		t.Fatalf("put error = %v, want ErrSourceChanged naming %s", err, name)
	}
	if e.hashedCount() != 0 || len(e.cloud.putLog()) != 0 {
		t.Error("put should use the cached hash and reject the changed content before sending the part")
	}
}

func TestHashCacheFilePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no permission bits")
	}
	dir := filepath.Join(t.TempDir(), "volumes")
	c := loadHashCache(dir)
	c.store("/data/a.bin", 1, time.Unix(1, 0), 1, [32]byte{1})
	if err := c.save(); err != nil {
		t.Fatal(err)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(filepath.Join(dir, hashCacheFile))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 || fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("cache modes: directory %o, file %o, want 700 and 600", dirInfo.Mode().Perm(), fileInfo.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Errorf("the directory holds %d entries (%v), want the cache only, no temp file", len(entries), err)
	}
}

func TestHashFileSetsDigestAndIdentity(t *testing.T) {
	content := patterned(3<<20+5, 1)
	file := filepath.Join(t.TempDir(), "a.bin")
	writeFile(t, file, content)
	src := Source{
		Abs:  file,
		Size: 1,
	}

	if err := hashFile(t.Context(), &src, make([]byte, hashBufBytes), nil); err != nil {
		t.Fatal(err)
	}

	if want := sha256.Sum256(content); src.SHA256 != want {
		t.Errorf("digest = %x, want %x", src.SHA256, want)
	}
	if src.Size != int64(len(content)) || src.ModTime.IsZero() {
		t.Errorf("identity = size %d, time %v, want the size and time the file had when read", src.Size, src.ModTime)
	}
}

func TestHashFileHonorsContext(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.bin")
	writeFile(t, file, patterned(1<<20, 1))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	src := Source{Abs: file}

	err := hashFile(ctx, &src, make([]byte, hashBufBytes), nil)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want the cancellation", err)
	}
}

// changingContext changes a file the second time hashing asks whether it was cancelled, which is between two reads.
type changingContext struct {
	context.Context
	asked  int
	change func()
}

func (c *changingContext) Err() error {
	if c.asked++; c.asked == 2 {
		c.change()
	}
	return c.Context.Err()
}

type changedWhileReadCase struct {
	name            string
	change          func(t *testing.T, file string)
	needsChangeTime bool
}

// TestHashFileRefusesAFileThatChangesWhileItIsRead: a file that grows, shrinks or is touched during the read is not hashed.
func TestHashFileRefusesAFileThatChangesWhileItIsRead(t *testing.T) {
	cases := []changedWhileReadCase{
		{
			name:   "grows",
			change: func(t *testing.T, file string) { appendTo(t, file, patterned(100, 2)) },
		},
		{
			name: "shrinks",
			change: func(t *testing.T, file string) {
				if err := os.Truncate(file, 3<<19); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "is touched",
			change: func(t *testing.T, file string) {
				info, err := os.Stat(file)
				if err != nil {
					t.Fatal(err)
				}
				later := info.ModTime().Add(2 * time.Second)
				if err := os.Chtimes(file, later, later); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:            "is rewritten keeping its size and time",
			change:          func(t *testing.T, file string) { swapKeepingStat(t, file, patterned(3<<20, 2)) },
			needsChangeTime: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "a.bin")
			writeFile(t, file, patterned(3<<20, 1))
			if tc.needsChangeTime {
				info, err := os.Stat(file)
				if err != nil {
					t.Fatal(err)
				}
				if changeTime(file, info) == 0 {
					t.Skip("this platform reports no change time")
				}
			}
			ctx := &changingContext{
				Context: t.Context(),
				change:  func() { tc.change(t, file) },
			}
			src := Source{Abs: file}

			err := hashFile(ctx, &src, make([]byte, hashBufBytes), nil)

			if !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("error = %v, want ErrSourceChanged", err)
			}
			if fe, ok := errors.AsType[*FileError](err); !ok || fe.Path != file {
				t.Errorf("error %v does not name %s", err, file)
			}
			if src.SHA256 != [32]byte{} {
				t.Error("the digest of a file that changed was recorded")
			}
		})
	}
}

func TestUnchanged(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.bin")
	writeFile(t, file, patterned(10, 1))
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	src := Source{
		Abs:     file,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Change:  changeTime(file, info),
	}
	if err := unchanged(&src); err != nil {
		t.Fatalf("an untouched file is changed: %v", err)
	}

	if src.Change != 0 {
		swapKeepingStat(t, file, patterned(10, 2))
		if err := unchanged(&src); !errors.Is(err, ErrSourceChanged) {
			t.Errorf("a file rewritten at the same size and time: error = %v, want ErrSourceChanged", err)
		}
	}

	later := info.ModTime().Add(time.Second)
	if err := os.Chtimes(file, later, later); err != nil {
		t.Fatal(err)
	}
	if err := unchanged(&src); !errors.Is(err, ErrSourceChanged) {
		t.Errorf("a touched file: error = %v, want ErrSourceChanged", err)
	}

	writeFile(t, file, patterned(11, 1))
	src.ModTime = later
	if err := unchanged(&src); !errors.Is(err, ErrSourceChanged) {
		t.Errorf("a file of another size: error = %v, want ErrSourceChanged", err)
	}

	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := unchanged(&src); !errors.Is(err, ErrSourceChanged) {
		t.Errorf("a removed file: error = %v, want ErrSourceChanged", err)
	}
}
