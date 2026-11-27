package put

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/runware/runware-cli/pkg/volumeformat"
)

// readCounter opens sources through countedFile and counts the bytes read from each, by base name.
// When flipAt is set, a positioned read of flipName returns the byte at that offset inverted, as a
// change after hashing that the file's size and times do not show.
type readCounter struct {
	flipName string
	flipAt   int64

	mu    sync.Mutex
	bytes map[string]int64
}

func newReadCounter() *readCounter {
	return &readCounter{
		flipAt: -1,
		bytes:  map[string]int64{},
	}
}

func (c *readCounter) open(path string) (sourceFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &countedFile{
		File:    f,
		counter: c,
		name:    filepath.Base(path),
	}, nil
}

func (c *readCounter) add(name string, n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bytes[name] += int64(n)
}

func (c *readCounter) read(name string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytes[name]
}

type countedFile struct {
	*os.File
	counter *readCounter
	name    string
}

func (f *countedFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	f.counter.add(f.name, n)
	return n, err
}

func (f *countedFile) ReadAt(p []byte, off int64) (int, error) {
	n, err := f.File.ReadAt(p, off)
	f.counter.add(f.name, n)
	if f.name == f.counter.flipName && f.counter.flipAt >= off && f.counter.flipAt < off+int64(n) {
		p[f.counter.flipAt-off] ^= 0xff
	}
	return n, err
}

// smallName is the one-part file of resumeTree.
const smallName = "small.txt"

// treeSizes is the size of each file of resumeTree.
var treeSizes = map[string]int64{
	bigName:   twoPartSize,
	smallName: 3000,
}

// TestPutReadsEachFileTwice: hashing reads a file once for its digest and every part's digest, and
// the send reads each part once more; nothing else reads it.
func TestPutReadsEachFileTwice(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	counter := newReadCounter()
	opts := e.options(vol, dir)
	opts.openSource = counter.open

	if _, err := Run(t.Context(), opts); err != nil {
		t.Fatal(err)
	}

	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	for name, size := range treeSizes {
		if got := counter.read(name); got != 2*size {
			t.Errorf("%s: read %d bytes, want %d: once to hash it and once to send it", name, got, 2*size)
		}
	}
}

// TestPutCacheHitReadsEachFileOnce: a cache entry with the part digests stands for the hashing read,
// so the file is read only to send it.
func TestPutCacheHitReadsEachFileOnce(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	opts := e.options(vol, dir)
	sources := cachedTree(t, opts)
	counter := newReadCounter()
	opts.openSource = counter.open

	if _, err := Run(t.Context(), opts); err != nil {
		t.Fatal(err)
	}

	if e.hashedCount() != 0 {
		t.Errorf("%d files were hashed, want every one from the cache", e.hashedCount())
	}
	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	for name, size := range treeSizes {
		if got := counter.read(name); got != size {
			t.Errorf("%s: read %d bytes, want %d: only to send it", name, got, size)
		}
	}
	for i := range sources {
		src := &sources[i]
		entry := loadHashCache(e.stateDir()).entries[src.Abs]
		want := 0 // a one-part file keeps only the file's digest
		if filepath.Base(src.Abs) == bigName {
			want = 2
		}
		if len(entry.Parts) != want {
			t.Errorf("%s: the cache holds %d part digests, want %d", src.Target, len(entry.Parts), want)
		}
	}
}

// TestPutOldCacheEntryIsAMiss: an entry saved before part digests were cached is a miss, so the file is
// hashed again rather than read a third time to find its part digests.
func TestPutOldCacheEntryIsAMiss(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	opts := e.options(vol, dir)
	sources := cachedTree(t, opts)
	old := cacheFile{
		Version: 2,
		Files:   map[string]cacheEntry{},
	}
	for i := range sources {
		src := &sources[i]
		old.Files[src.Abs] = cacheEntry{
			Size:     src.Size,
			ModTime:  src.ModTime.UnixNano(),
			Change:   src.Change,
			SHA256:   hex.EncodeToString(src.SHA256[:]),
			StoredAt: time.Now().UnixNano(),
		}
	}
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(e.stateDir(), hashCacheFile), data)
	counter := newReadCounter()
	opts.openSource = counter.open

	if _, err := Run(t.Context(), opts); err != nil {
		t.Fatal(err)
	}

	if e.hashedCount() != len(sources) {
		t.Errorf("%d files were hashed, want all %d: an old entry is a miss", e.hashedCount(), len(sources))
	}
	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	for name, size := range treeSizes {
		if got := counter.read(name); got != 2*size {
			t.Errorf("%s: read %d bytes, want %d", name, got, 2*size)
		}
	}
}

// cachedTree hashes the put's sources into the hash cache, as an earlier put would, and returns them.
func cachedTree(t *testing.T, opts Options) []Source {
	t.Helper()
	sources, err := Walk(opts.Paths, opts.To)
	if err != nil {
		t.Fatal(err)
	}
	for i := range sources {
		src := &sources[i]
		if src.Change == 0 {
			t.Skip("this filesystem does not report a change time for cache hits")
		}
	}
	r, err := newRunner(opts)
	if err != nil {
		t.Fatal(err)
	}
	r.o.onHash = nil
	cache := loadHashCache(r.stateDir)
	if err := r.hashSources(t.Context(), sources, cache); err != nil {
		t.Fatal(err)
	}
	if err := cache.save(); err != nil {
		t.Fatal(err)
	}
	return sources
}

// TestHashCacheNeedsEveryPartDigest: an entry for a file of several parts is a hit only with a digest
// for each part, and an entry for a one-part file answers the file's digest as its part's.
func TestHashCacheNeedsEveryPartDigest(t *testing.T) {
	t.Parallel()
	c := loadHashCache(t.TempDir())
	mod := time.Unix(1, 0)
	sum := [32]byte{1}
	parts := [][32]byte{{2}, {3}}

	c.store("/data/one.bin", 7, mod, 42, sum, [][32]byte{sum})
	if got, gotParts, ok := c.lookup("/data/one.bin", 7, mod, 42); !ok || got != sum || len(gotParts) != 1 || gotParts[0] != sum {
		t.Errorf("a one-part entry reads back as %x, %x, %v", got, gotParts, ok)
	}
	c.store("/data/two.bin", twoPartSize, mod, 42, sum, parts)
	if got, gotParts, ok := c.lookup("/data/two.bin", twoPartSize, mod, 42); !ok || got != sum || len(gotParts) != 2 || gotParts[0] != parts[0] || gotParts[1] != parts[1] {
		t.Errorf("a two-part entry reads back as %x, %x, %v", got, gotParts, ok)
	}
	for name, entryParts := range map[string][]string{
		"no part digests":       nil,
		"too few part digests":  {hex.EncodeToString(parts[0][:])},
		"a malformed digest":    {hex.EncodeToString(parts[0][:]), "zz"},
		"too many part digests": {hex.EncodeToString(parts[0][:]), hex.EncodeToString(parts[1][:]), hex.EncodeToString(parts[1][:])},
	} {
		e := c.entries["/data/two.bin"]
		e.Parts = entryParts
		c.entries["/data/three.bin"] = e
		if _, _, ok := c.lookup("/data/three.bin", twoPartSize, mod, 42); ok {
			t.Errorf("the cache answered for a two-part entry with %s", name)
		}
	}
}

// TestHashFileDigestsEveryPart: hashing gives each part of the layout the digest of its bytes.
func TestHashFileDigestsEveryPart(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), bigName)
	writeTwoPartFile(t, file)
	src := Source{Abs: file}

	if err := hashFile(t.Context(), &src, make([]byte, hashBufBytes), nil, openFile); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(file) //nolint:gosec // the test's own temporary file
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Parts) != 2 || src.Parts[0] != sha256.Sum256(content[:firstPartSize]) || src.Parts[1] != sha256.Sum256(content[firstPartSize:]) {
		t.Errorf("part digests = %x, want the digest of each part's bytes", src.Parts)
	}
	if src.SHA256 != sha256.Sum256(content) {
		t.Error("the file's digest is not the digest of its bytes")
	}
}

// partChange is a byte of a part that differs when the part is sent.
type partChange struct {
	name string
	part int64
	at   int64 // the file offset of the byte
}

// TestChangeBetweenHashingAndSendNeverCompletesThePart: bytes that differ from those hashed when the
// part is sent, with the file's size and times unchanged, stop the part before its last segment is
// sealed: it is never acknowledged and the put aborts.
func TestChangeBetweenHashingAndSendNeverCompletesThePart(t *testing.T) {
	t.Parallel()
	for _, tc := range []partChange{
		{
			name: "in the first part",
			part: 1,
			at:   10,
		},
		{
			name: "in the last part",
			part: 2,
			at:   firstPartSize + 10,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newTestEnv(t)
			file := filepath.Join(e.srcDir, bigName)
			writeTwoPartFile(t, file)
			vol := e.cloud.newVolume("model-weights", "")
			var rec sealRecorder
			counter := newReadCounter()
			counter.flipName, counter.flipAt = bigName, tc.at
			opts := e.options(vol, file)
			opts.Parallelism = 1
			opts.newSealer = rec.factory
			opts.openSource = counter.open

			_, err := Run(t.Context(), opts)

			assertAborted(t, e, err, file, vol, 0)
			resolved, evalErr := filepath.EvalSymlinks(file)
			if evalErr != nil {
				t.Fatal(evalErr)
			}
			want := fmt.Sprintf("%s: part %d no longer matches what was hashed, so nothing from this upload was published; run put again", resolved, tc.part)
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err, want)
			}
			if okPuts(e, bigName, int(tc.part)) != 0 {
				t.Errorf("part %d, which changed, was acknowledged", tc.part)
			}
			if e.cloud.count("complete-file") != 0 {
				t.Error("the changed file was completed")
			}
			layout, err := volumeformat.PlanLayout(twoPartSize)
			if err != nil {
				t.Fatal(err)
			}
			segments, err := layout.SegmentsInPart(tc.part)
			if err != nil {
				t.Fatal(err)
			}
			sealed := 0
			for _, seal := range rec.all() {
				if seal.part != tc.part {
					continue
				}
				sealed++
				if int64(len(seal.segments)) == segments {
					t.Errorf("the changed part %d was sealed to its last segment", tc.part)
				}
			}
			if sealed == 0 {
				t.Errorf("part %d was never sent, so the send's check was not reached", tc.part)
			}
			rec.requireSafe(t)
		})
	}
}

// statChange is a change to a file that moves its size or times.
type statChange struct {
	name   string
	change func(t *testing.T, file string)
}

// TestChangeBeforeTheLastPartStopsItBeforeItIsSent: before the last part of a file is journalled, the
// file's size and times are checked against hashing, so a file that grew, or whose modification time
// moved, after it was hashed never sends its last part, even when every part's bytes still match.
func TestChangeBeforeTheLastPartStopsItBeforeItIsSent(t *testing.T) {
	t.Parallel()
	for _, tc := range []statChange{
		{
			name:   "it grows by a byte",
			change: func(t *testing.T, file string) { appendTo(t, file, []byte{0}) },
		},
		{
			name: "its modification time moves",
			change: func(t *testing.T, file string) {
				later := time.Now().Add(5 * time.Second)
				if err := os.Chtimes(file, later, later); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newTestEnv(t)
			file := filepath.Join(e.srcDir, bigName)
			writeTwoPartFile(t, file)
			vol := e.cloud.newVolume("model-weights", "")
			var rec sealRecorder
			opts := e.options(vol, file)
			opts.Parallelism = 1
			opts.newSealer = rec.factory
			// Declaration follows hashing and checks the file before it is sent, so the change lands
			// after every check before the one for the last part.
			e.cloud.onDeclare = func(FileDeclarations) { tc.change(t, file) }

			_, err := Run(t.Context(), opts)

			assertAborted(t, e, err, file, vol, 0)
			for _, p := range e.cloud.putLog() {
				if p.Part == 2 {
					t.Errorf("the last part was sent (status %d) after the file changed", p.Status)
				}
			}
			for _, seal := range rec.all() {
				if seal.part == 2 {
					t.Error("the last part was sealed after the file changed")
				}
			}
			if e.cloud.count("complete-file") != 0 {
				t.Error("the changed file was completed")
			}
		})
	}
}
