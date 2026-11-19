package put

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	stateDirName    = "volumes"
	hashCacheFile   = "hashes.json"
	hashCacheForm   = 2
	hashBufBytes    = 1 << 20
	maxCachedHashes = 100_000
)

type cacheEntry struct {
	Size     int64  `json:"size"`
	ModTime  int64  `json:"modTimeUnixNano"`
	Change   int64  `json:"changeTimeUnixNano"`
	SHA256   string `json:"sha256"`
	StoredAt int64  `json:"storedAtUnixNano,omitempty"`
}

type cacheFile struct {
	Version int                   `json:"version"`
	Files   map[string]cacheEntry `json:"files"`
}

// hashCache remembers file digests by path, size, modification time and change time; a zero change time is never trusted.
type hashCache struct {
	path    string
	entries map[string]cacheEntry
	dirty   bool
}

// loadHashCache reads the cache; a missing or unreadable file is an empty cache.
func loadHashCache(stateDir string) *hashCache {
	c := &hashCache{
		path:    filepath.Join(stateDir, hashCacheFile),
		entries: map[string]cacheEntry{},
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return c
	}
	var f cacheFile
	if json.Unmarshal(data, &f) != nil || f.Version != hashCacheForm || f.Files == nil {
		return c
	}
	c.entries = f.Files
	c.trim(maxCachedHashes)
	return c
}

func (c *hashCache) lookup(abs string, size int64, modTime time.Time, change int64) ([32]byte, bool) {
	e, ok := c.entries[abs]
	if !ok || change == 0 || e.Size != size || e.ModTime != modTime.UnixNano() || e.Change != change {
		return [32]byte{}, false
	}
	raw, err := hex.DecodeString(e.SHA256)
	if err != nil || len(raw) != sha256.Size {
		return [32]byte{}, false
	}
	return [32]byte(raw), true
}

func (c *hashCache) store(abs string, size int64, modTime time.Time, change int64, sum [32]byte) {
	c.entries[abs] = cacheEntry{
		Size:     size,
		ModTime:  modTime.UnixNano(),
		Change:   change,
		SHA256:   hex.EncodeToString(sum[:]),
		StoredAt: time.Now().UnixNano(),
	}
	c.dirty = true
}

func (c *hashCache) forget(abs string) {
	if _, ok := c.entries[abs]; ok {
		delete(c.entries, abs)
		c.dirty = true
	}
}

func (c *hashCache) trim(limit int) {
	if len(c.entries) <= limit {
		return
	}
	paths := slices.SortedFunc(maps.Keys(c.entries), func(a, b string) int {
		if order := cmp.Compare(c.entries[b].StoredAt, c.entries[a].StoredAt); order != 0 {
			return order
		}
		return cmp.Compare(a, b)
	})
	for _, path := range paths[limit:] {
		delete(c.entries, path)
	}
	c.dirty = true
}

// save writes the cache when it changed: a 0600 file, in a 0700 directory, by temp file and rename.
func (c *hashCache) save() error {
	if !c.dirty {
		return nil
	}
	c.trim(maxCachedHashes)
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create the hash cache directory: %w", err)
	}
	data, err := json.Marshal(cacheFile{
		Version: hashCacheForm,
		Files:   c.entries,
	})
	if err != nil {
		return fmt.Errorf("encode the hash cache: %w", err)
	}
	if err := writeFileAtomic(dir, filepath.Base(c.path), data); err != nil {
		return fmt.Errorf("write the hash cache: %w", err)
	}
	c.dirty = false
	return nil
}

// writeFileAtomic writes data to dir/name through a 0600 temp file, a sync and a rename.
func writeFileAtomic(dir, name string, data []byte) error {
	tmp, err := os.CreateTemp(dir, name+".*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(dir, name))
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// hashSources sets each source's digest from the cache or by reading the file.
func hashSources(ctx context.Context, sources []Source, cache *hashCache, p *progress, onHash func(string)) error {
	var total int64
	for i := range sources {
		total += sources[i].Size
	}
	p.begin("hashing", len(sources), total)
	defer p.end()
	buf := make([]byte, hashBufBytes)
	for i := range sources {
		s := &sources[i]
		if sum, ok := cache.lookup(s.Abs, s.Size, s.ModTime, s.Change); ok {
			s.SHA256 = sum
			p.addBytes(s.Size)
			p.fileDone()
			continue
		}
		if onHash != nil {
			onHash(s.Abs)
		}
		if err := hashFile(ctx, s, buf, p); err != nil {
			return err
		}
		cache.store(s.Abs, s.Size, s.ModTime, s.Change, s.SHA256)
		p.fileDone()
	}
	return nil
}

// hashFile hashes the file in one read; a file that changes while it is read is an error.
func hashFile(ctx context.Context, s *Source, buf []byte, p *progress) error {
	f, err := os.Open(s.Abs)
	if err != nil {
		return fileErr(s.Abs, err)
	}
	defer func() { _ = f.Close() }()
	before, err := f.Stat()
	if err != nil {
		return fileErr(s.Abs, err)
	}
	change := changeTime(s.Abs, before)
	h := sha256.New()
	n, err := io.CopyBuffer(h, &hashReader{
		ctx: ctx,
		r:   f,
		p:   p,
	}, buf)
	if err != nil {
		return fileErr(s.Abs, err)
	}
	after, err := f.Stat()
	if err != nil {
		return fileErr(s.Abs, err)
	}
	if n != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || changeTime(s.Abs, after) != change {
		return fileErr(s.Abs, ErrSourceChanged)
	}
	s.Size, s.ModTime, s.Change = before.Size(), before.ModTime(), change
	copy(s.SHA256[:], h.Sum(nil))
	return nil
}

// hashReader reads under a context and counts the bytes for progress.
type hashReader struct {
	ctx context.Context
	r   io.Reader
	p   *progress
}

func (h *hashReader) Read(b []byte) (int, error) {
	if err := h.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := h.r.Read(b)
	h.p.addBytes(int64(n))
	return n, err
}

// unchanged returns ErrSourceChanged when the file's size, modification time or change time moved since hashing.
func unchanged(s *Source) error {
	info, err := os.Stat(s.Abs)
	if err != nil {
		return fileErr(s.Abs, fmt.Errorf("%w: %w", ErrSourceChanged, err))
	}
	if info.Size() != s.Size || !info.ModTime().Equal(s.ModTime) || changeTime(s.Abs, info) != s.Change {
		return fileErr(s.Abs, ErrSourceChanged)
	}
	return nil
}
