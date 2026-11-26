package put

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/runware/runware-cli/pkg/volumeformat"
)

const (
	stateDirName  = "volumes"
	hashCacheFile = "hashes.json"
	// Any change to PlanLayout's part boundaries must bump hashCacheForm, or cached part digests go stale.
	hashCacheForm   = 3 // 3 adds the part digests; an entry of an earlier form is a miss
	hashBufBytes    = 1 << 20
	maxCachedHashes = 100_000
)

// cacheEntry is a file's digest and the digest of each of its parts. Parts is empty for a file of one
// part, whose part digest is the file's.
type cacheEntry struct {
	Size     int64    `json:"size"`
	ModTime  int64    `json:"modTimeUnixNano"`
	Change   int64    `json:"changeTimeUnixNano"`
	SHA256   string   `json:"sha256"`
	Parts    []string `json:"parts,omitempty"`
	StoredAt int64    `json:"storedAtUnixNano,omitempty"`
}

type cacheFile struct {
	Version int                   `json:"version"`
	Files   map[string]cacheEntry `json:"files"`
}

// hashCache remembers file and part digests by path, size, modification time and change time; a zero change time is never trusted.
type hashCache struct {
	path    string
	entries map[string]cacheEntry
	dirty   bool
	// bypassLookup makes every lookup a miss while store still records fresh entries. A resume sets it:
	// its acknowledged parts are never read again, so their digests must come from the file as it is now.
	bypassLookup bool
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

// lookup returns the file's digest and its part digests. An entry without a digest for every part of
// the format's layout for size is a miss, so a hit never needs another read of the file.
func (c *hashCache) lookup(abs string, size int64, modTime time.Time, change int64) ([32]byte, [][32]byte, bool) {
	if c.bypassLookup {
		return [32]byte{}, nil, false
	}
	e, ok := c.entries[abs]
	if !ok || change == 0 || e.Size != size || e.ModTime != modTime.UnixNano() || e.Change != change {
		return [32]byte{}, nil, false
	}
	sum, ok := decodeDigest(e.SHA256)
	if !ok {
		return [32]byte{}, nil, false
	}
	layout, err := volumeformat.PlanLayout(size)
	if err != nil {
		return sum, nil, len(e.Parts) == 0
	}
	if layout.PartCount == 1 {
		return sum, [][32]byte{sum}, len(e.Parts) == 0
	}
	if int64(len(e.Parts)) != layout.PartCount {
		return [32]byte{}, nil, false
	}
	parts := make([][32]byte, len(e.Parts))
	for i, s := range e.Parts {
		if parts[i], ok = decodeDigest(s); !ok {
			return [32]byte{}, nil, false
		}
	}
	return sum, parts, true
}

func decodeDigest(s string) ([32]byte, bool) {
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) != sha256.Size {
		return [32]byte{}, false
	}
	return [32]byte(raw), true
}

// store remembers the file's digest and its part digests; a file of one part keeps only the file's.
func (c *hashCache) store(abs string, size int64, modTime time.Time, change int64, sum [32]byte, parts [][32]byte) {
	var hexParts []string
	if len(parts) > 1 {
		hexParts = make([]string, len(parts))
		for i := range parts {
			hexParts[i] = hex.EncodeToString(parts[i][:])
		}
	}
	c.entries[abs] = cacheEntry{
		Size:     size,
		ModTime:  modTime.UnixNano(),
		Change:   change,
		SHA256:   hex.EncodeToString(sum[:]),
		Parts:    hexParts,
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

// hashSources sets each source's digest and part digests from the cache or by reading the file once.
func (r *runner) hashSources(ctx context.Context, sources []Source, cache *hashCache) error {
	var total int64
	for i := range sources {
		total += sources[i].Size
	}
	p := r.progress
	p.begin("hashing", len(sources), total)
	defer p.end()
	buf := make([]byte, hashBufBytes)
	for i := range sources {
		s := &sources[i]
		if sum, parts, ok := cache.lookup(s.Abs, s.Size, s.ModTime, s.Change); ok {
			s.SHA256, s.Parts = sum, parts
			p.addBytes(s.Size)
			p.fileDone()
			continue
		}
		if r.o.onHash != nil {
			r.o.onHash(s.Abs)
		}
		if err := hashFile(ctx, s, buf, p, r.openSource); err != nil {
			return err
		}
		cache.store(s.Abs, s.Size, s.ModTime, s.Change, s.SHA256, s.Parts)
		p.fileDone()
	}
	return nil
}

// hashFile hashes the file and each part of its layout in one read; a file that changes while it is
// read is an error. A size the format has no layout for gets no part digests: no layout is approved
// for it, so none of its parts is ever sent.
func hashFile(ctx context.Context, s *Source, buf []byte, p *progress, open func(string) (sourceFile, error)) error {
	f, err := open(s.Abs)
	if err != nil {
		return fileErr(s.Abs, err)
	}
	defer func() { _ = f.Close() }()
	before, err := f.Stat()
	if err != nil {
		return fileErr(s.Abs, err)
	}
	change := fileChangeTime(s.Abs, before)
	whole := sha256.New()
	var w io.Writer = whole
	layout, layoutErr := volumeformat.PlanLayout(before.Size())
	var parts *partHasher
	if layoutErr == nil && layout.PartCount > 1 {
		parts = newPartHasher(layout)
		w = io.MultiWriter(whole, parts)
	}
	n, err := io.CopyBuffer(w, &hashReader{
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
	if n != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || fileChangeTime(s.Abs, after) != change {
		return fileErr(s.Abs, ErrSourceChanged)
	}
	s.Size, s.ModTime, s.Change = before.Size(), before.ModTime(), change
	copy(s.SHA256[:], whole.Sum(nil))
	switch {
	case layoutErr != nil:
		s.Parts = nil
	case parts == nil:
		s.Parts = [][32]byte{s.SHA256}
	default:
		s.Parts = parts.sums
	}
	return nil
}

// partHasher hashes what is written to it part by part, in the layout's parts, and keeps each part's
// digest. More bytes than the layout holds are a changed source.
type partHasher struct {
	layout volumeformat.Layout
	h      hash.Hash
	left   int64 // bytes of the current part not yet written
	sums   [][32]byte
}

func newPartHasher(l volumeformat.Layout) *partHasher {
	return &partHasher{
		layout: l,
		h:      sha256.New(),
		sums:   make([][32]byte, 0, l.PartCount),
	}
}

func (ph *partHasher) Write(b []byte) (int, error) {
	written := len(b)
	for len(b) > 0 {
		if ph.left == 0 {
			next := int64(len(ph.sums)) + 1
			_, n, err := ph.layout.PlaintextRange(next)
			if err != nil {
				return 0, fmt.Errorf("%w: it grew while it was read", ErrSourceChanged)
			}
			ph.left = n
		}
		k := min(int64(len(b)), ph.left)
		ph.h.Write(b[:k])
		ph.left -= k
		b = b[k:]
		if ph.left == 0 {
			ph.sums = append(ph.sums, [32]byte(ph.h.Sum(nil)))
			ph.h.Reset()
		}
	}
	return written, nil
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

// fileChangeTime is changeTime. Tests replace it to model a filesystem where a write does not move the
// change time.
var fileChangeTime = changeTime

// unchanged returns ErrSourceChanged when the file's size, modification time or change time moved since hashing.
func unchanged(s *Source) error {
	info, err := os.Stat(s.Abs)
	if err != nil {
		return fileErr(s.Abs, fmt.Errorf("%w: %w", ErrSourceChanged, err))
	}
	if info.Size() != s.Size || !info.ModTime().Equal(s.ModTime) || fileChangeTime(s.Abs, info) != s.Change {
		return fileErr(s.Abs, ErrSourceChanged)
	}
	return nil
}

// sourceFile is a source file opened for reading.
type sourceFile interface {
	io.Reader
	io.ReaderAt
	io.Closer
	Stat() (os.FileInfo, error)
}

// openSource opens a source file for reading, through the options' hook when one is set.
func (r *runner) openSource(path string) (sourceFile, error) {
	if r.o.openSource != nil {
		return r.o.openSource(path)
	}
	return openFile(path)
}

func openFile(path string) (sourceFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return f, nil
}
