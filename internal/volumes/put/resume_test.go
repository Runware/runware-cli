package put

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runware/runware-cli/internal/output"
	"github.com/runware/runware-cli/pkg/volumeformat"
)

// firstPartSize is the plaintext of the first part of a two-part file.
const firstPartSize = int64(volumeformat.SegmentPlaintextBytes) * volumeformat.MinSegmentsPerPart

// sealRecord is one sealing: the data key, the part's salt and number, the plaintext length and the
// SHA-256 of each segment's plaintext. The part key is HKDF(data key, salt, part number) and the nonce
// is the segment index within the part, so (data key, salt, part, index) names one key and nonce pair.
type sealRecord struct {
	dataKey  []byte
	salt     []byte
	part     int64
	size     int
	segments [][32]byte
}

// sealRecorder is the instrumented sealer: it wraps the format's part sealer and records every sealing.
type sealRecorder struct {
	mu    sync.Mutex
	seals []sealRecord
}

func (s *sealRecorder) factory(dataKey []byte, id volumeformat.FileIdentity, l volumeformat.Layout, part int64) (partSealer, error) {
	inner, err := volumeformat.NewPartSealer(dataKey, id, l, part)
	if err != nil {
		return nil, err
	}
	return &recordingSealer{
		rec:     s,
		inner:   inner,
		dataKey: bytes.Clone(dataKey),
		layout:  l,
		part:    part,
	}, nil
}

type recordingSealer struct {
	rec     *sealRecorder
	inner   *volumeformat.PartSealer
	dataKey []byte
	layout  volumeformat.Layout
	part    int64
}

func (r *recordingSealer) Len() int64 { return r.inner.Len() }

// StreamReader records the sealing when it starts, and each segment as soon as the format would seal
// it: once its plaintext is whole, and for the part's last segment once the plaintext has also ended.
// A segment the format refuses to seal, because its plaintext failed or changed, is not recorded.
func (r *recordingSealer) StreamReader(plaintext io.Reader) (io.Reader, error) {
	_, n, err := r.layout.PlaintextRange(r.part)
	if err != nil {
		return nil, err
	}
	count, err := r.layout.SegmentsInPart(r.part)
	if err != nil {
		return nil, err
	}
	index := r.rec.add(sealRecord{
		dataKey: r.dataKey,
		salt:    r.inner.Salt(),
		part:    r.part,
		size:    int(n),
	})
	return r.inner.StreamReader(&recordingPlaintext{
		r:     plaintext,
		rec:   r.rec,
		index: index,
		size:  n,
		count: count,
		h:     sha256.New(),
	})
}

// recordingPlaintext hashes the plaintext a sealer reads, segment by segment, into its sealing's record.
type recordingPlaintext struct {
	r     io.Reader
	rec   *sealRecorder
	index int
	size  int64 // the part's plaintext bytes
	count int64 // the part's segments
	read  int64
	seg   int64 // the segment being read
	h     hash.Hash
	last  *[32]byte // the last segment's digest, recorded when the plaintext ends
}

func (p *recordingPlaintext) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	for chunk := b[:n]; len(chunk) > 0 && p.seg < p.count; {
		end := min(p.size, (p.seg+1)*volumeformat.SegmentPlaintextBytes)
		k := min(int64(len(chunk)), end-p.read)
		p.h.Write(chunk[:k])
		p.read += k
		chunk = chunk[k:]
		if p.read == end {
			p.segmentRead()
		}
	}
	if p.size == 0 && p.seg == 0 && p.last == nil {
		p.segmentRead()
	}
	if errors.Is(err, io.EOF) && p.last != nil {
		p.rec.addSegment(p.index, *p.last)
		p.last = nil
	}
	return n, err
}

// segmentRead closes the segment being read: a segment before the last is sealed at once, the last
// only once the plaintext ends.
func (p *recordingPlaintext) segmentRead() {
	sum := [32]byte(p.h.Sum(nil))
	p.h.Reset()
	if p.seg == p.count-1 {
		p.last = &sum
	} else {
		p.rec.addSegment(p.index, sum)
	}
	p.seg++
}

func (s *sealRecorder) add(rec sealRecord) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seals = append(s.seals, rec)
	return len(s.seals) - 1
}

func (s *sealRecorder) addSegment(index int, digest [32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seals[index].segments = append(s.seals[index].segments, digest)
}

func (s *sealRecorder) all() []sealRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]sealRecord, len(s.seals))
	for i, rec := range s.seals {
		rec.segments = slices.Clone(rec.segments)
		out[i] = rec
	}
	return out
}

// violations lists every (key, nonce) pair that sealed two different plaintexts, and every part key used
// for more than one sealing (a repeated salt). naive counts the (data key, part, index) triples that
// sealed different plaintexts, which would have repeated a key and nonce pair without the per-part salt.
func (s *sealRecorder) violations() (problems []string, naive int) {
	type pairKey struct {
		dataKey, salt string
		part, index   int64
	}
	type naiveKey struct {
		dataKey     string
		part, index int64
	}
	type partKey struct {
		dataKey, salt string
		part          int64
	}
	pairs := map[pairKey][32]byte{}
	naives := map[naiveKey][32]byte{}
	parts := map[partKey]struct{}{}
	for i, rec := range s.all() {
		pk := partKey{
			dataKey: string(rec.dataKey),
			salt:    string(rec.salt),
			part:    rec.part,
		}
		if _, dup := parts[pk]; dup {
			problems = append(problems, fmt.Sprintf("sealing %d reuses the salt of an earlier sealing of part %d", i, rec.part))
		}
		parts[pk] = struct{}{}
		for idx, digest := range rec.segments {
			k := pairKey{
				dataKey: pk.dataKey,
				salt:    pk.salt,
				part:    rec.part,
				index:   int64(idx),
			}
			if prev, ok := pairs[k]; ok && prev != digest {
				problems = append(problems, fmt.Sprintf("sealing %d seals other plaintext under a used key at part %d segment %d", i, rec.part, idx))
			}
			pairs[k] = digest
			n := naiveKey{
				dataKey: pk.dataKey,
				part:    rec.part,
				index:   int64(idx),
			}
			if prev, ok := naives[n]; ok && prev != digest {
				naive++
			}
			naives[n] = digest
		}
	}
	return problems, naive
}

// requireSafe fails the test when any key and nonce pair sealed two different plaintexts.
func (s *sealRecorder) requireSafe(t *testing.T) int {
	t.Helper()
	problems, naive := s.violations()
	for _, p := range problems {
		t.Error(p)
	}
	return naive
}

// firstParts returns the sealings of first parts with a plaintext of size bytes.
func (s *sealRecorder) firstParts(size int64) []sealRecord {
	var out []sealRecord
	for _, rec := range s.all() {
		if rec.part == 1 && int64(rec.size) == size {
			out = append(out, rec)
		}
	}
	return out
}

func (s *sealRecorder) dataKeys() [][]byte {
	var out [][]byte
	for _, rec := range s.all() {
		out = append(out, rec.dataKey)
	}
	return out
}

// TestSealRecorderCatchesReuse: the instrument reports a key and nonce pair that seals two plaintexts.
func TestSealRecorderCatchesReuse(t *testing.T) {
	t.Parallel()
	var rec sealRecorder
	key, salt := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 16)
	rec.add(sealRecord{
		dataKey:  key,
		salt:     salt,
		part:     1,
		segments: [][32]byte{sha256.Sum256([]byte("a"))},
	})
	rec.add(sealRecord{
		dataKey:  key,
		salt:     salt,
		part:     1,
		segments: [][32]byte{sha256.Sum256([]byte("b"))},
	})

	problems, naive := rec.violations()

	if len(problems) != 2 || naive != 1 {
		t.Errorf("problems = %q, naive = %d, want a reused salt, a reused pair and one naive collision", problems, naive)
	}
}

// resumeTree is a two-part file and a small file, uploaded as a directory.
func resumeTree(t *testing.T, e *testEnv) string {
	t.Helper()
	dir := filepath.Join(e.srcDir, "tree")
	writeTwoPartFile(t, filepath.Join(dir, bigName))
	writeFile(t, filepath.Join(dir, "small.txt"), patterned(3000, 7))
	return dir
}

// resumeOptions is a put of dir through the instrumented sealer, one part at a time.
func resumeOptions(e *testEnv, rec *sealRecorder, vol VolumeRef, dir string) Options {
	opts := e.options(vol, dir)
	opts.Parallelism = 1
	opts.newSealer = rec.factory
	return opts
}

// bigName is the two-part file of resumeTree.
const bigName = "big.bin"

// interruptedPut runs a put that is interrupted while a part of the two-part file is in flight, and
// returns the error text.
func interruptedPut(t *testing.T, e *testEnv, opts Options, part int) string {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	e.cloud.onPut = func(rq partRequest) {
		if rq.Path == bigName && rq.Part == part {
			cancel()
		}
	}
	_, err := Run(ctx, opts)
	e.cloud.onPut = nil
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the interrupted put returned %v, want the cancellation", err)
	}
	return err.Error()
}

// okPuts counts the accepted sends of a part of a file.
func okPuts(e *testEnv, path string, part int) int {
	n := 0
	for _, p := range e.cloud.putLog() {
		if p.Path == path && p.Part == part && p.Status == 200 {
			n++
		}
	}
	return n
}

// copyTree copies a directory with its files' modes.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		data, err := os.ReadFile(p) //nolint:gosec // copies the test's own temporary directory
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm()) //nolint:gosec // copies the test's own temporary directory
	})
	if err != nil {
		t.Fatal(err)
	}
}

// restoreTree replaces dst with a copy of src, as restoring a snapshot would.
func restoreTree(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.RemoveAll(dst); err != nil {
		t.Fatal(err)
	}
	copyTree(t, src, dst)
}

// editJournal rewrites the one saved journal's records through edit; a nil result drops the record.
func editJournal(t *testing.T, configDir string, edit func(map[string]any) map[string]any) {
	t.Helper()
	snap := readJournal(t, configDir)
	var out []byte
	for _, rec := range snap.Records {
		if rec = edit(rec); rec == nil {
			continue
		}
		line, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		out = append(append(out, line...), '\n')
	}
	if err := os.WriteFile(filepath.Join(snap.Dir, journalName), out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// patchFile overwrites bytes of a file in place, keeping its size.
func patchFile(t *testing.T, p string, off int64, b []byte) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(b, off); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(3 * time.Second)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
}

// AC 1: an interrupted put resumes from its acknowledged parts, sends none of them again and completes once.
func TestResumeSendsOnlyUnacknowledgedParts(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 2)
	if okPuts(e, bigName, 1) != 1 || okPuts(e, "small.txt", 1) != 0 {
		t.Fatalf("before the resume: puts = %+v", e.cloud.putLog())
	}

	sum, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir))
	if err != nil {
		t.Fatalf("resume: %v", err)
	}

	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	// The interrupted second part may have reached storage without its acknowledgement reaching the CLI,
	// so it can be accepted twice; the acknowledged parts are accepted once.
	if okPuts(e, bigName, 1) != 1 || okPuts(e, bigName, 2) < 1 || okPuts(e, "small.txt", 1) != 1 {
		t.Errorf("puts = %+v, want each acknowledged part accepted once", e.cloud.putLog())
	}
	if e.cloud.count("open") != 1 || e.cloud.count("keypair") != 1 || e.keys.opens != 1 {
		t.Errorf("calls = %v, key opens = %d: the resume must reuse the session and fetch no statement", e.cloud.callLog(), e.keys.opens)
	}
	if e.cloud.count("complete") != 1 || e.cloud.count("complete-file") != 2 || e.cloud.count("abort") != 0 {
		t.Errorf("calls = %v, want one completion of each file and of the session", e.cloud.callLog())
	}
	if sum.Added != 2 || !sum.Changed {
		t.Errorf("summary = %+v", sum)
	}
	if got := len(rec.firstParts(firstPartSize)); got != 1 {
		t.Errorf("the acknowledged first part was sealed %d times", got)
	}
	if dirs := e.uploadDirs(); len(dirs) != 0 {
		t.Errorf("resume state left after completion: %v", dirs)
	}
	rec.requireSafe(t)
}

// AC 1 and 2: a file that changed since the interrupt aborts the saved session, changes nothing in the
// Volume, and the next put opens a new session with fresh keys.
func TestResumeAbortsAChangedFileAndStartsOver(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 2)
	saved := readJournal(t, e.configDir)
	firstSession := filepath.Base(saved.Dir)
	big := filepath.Join(dir, bigName)
	patchFile(t, big, 10, []byte("changed"))

	if _, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir)); !errors.Is(err, ErrSourceChanged) ||
		!strings.Contains(err.Error(), big) {
		t.Fatalf("resume returned %v, want a source-change error naming %s", err, big)
	}
	if files, _ := e.cloud.latest(vol); len(files) != 0 {
		t.Fatalf("files = %v, want no publication after a source change", files)
	}

	e.cloud.mu.Lock()
	state := e.cloud.uploads[firstSession].state
	e.cloud.mu.Unlock()
	if state != fakeAborted {
		t.Errorf("the saved session is %q, want it aborted", state)
	}
	if _, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir)); err != nil {
		t.Fatal(err)
	}
	if e.cloud.count("open") != 2 || e.keys.opens != 2 {
		t.Errorf("calls = %v, key opens = %d, want a new session with a fresh encapsulation", e.cloud.callLog(), e.keys.opens)
	}

	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	if dirs := e.uploadDirs(); len(dirs) != 0 {
		t.Errorf("resume state left: %v", dirs)
	}
	rec.requireSafe(t)
}

// AC 2: a file changed between a failed send and its retry is never acknowledged: the resumed put's
// send finds the part's bytes differ from the digest journalled before the first send and stops before
// the part's last segment, or the size and time check before the last part stops it first. The session
// aborts, nothing reaches the Volume, the state is gone, and the next put starts over.
func TestResumeAbortsWhenAPartChangedBeforeItsRetry(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 1)
	big := filepath.Join(dir, bigName)
	interrupted := len(rec.firstParts(firstPartSize))
	accepted := okPuts(e, bigName, 1)
	// A new file makes the resume declare, which happens after hashing and before any part is sent.
	writeFile(t, filepath.Join(dir, "new.txt"), patterned(100, 9))
	e.cloud.mu.Lock()
	e.cloud.onDeclare = func(FileDeclarations) { patchFile(t, big, 10, []byte("changed")) }
	e.cloud.mu.Unlock()

	_, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir))

	resolved, evalErr := filepath.EvalSymlinks(big)
	if evalErr != nil {
		t.Fatal(evalErr)
	}
	if !errors.Is(err, ErrSourceChanged) || !strings.Contains(err.Error(), resolved) {
		t.Fatalf("error = %v, want the changed file", err)
	}
	if e.cloud.uploadState() != fakeAborted {
		t.Errorf("session state = %q, want aborted", e.cloud.uploadState())
	}
	if files, _ := e.cloud.latest(vol); len(files) != 0 {
		t.Errorf("an aborted session published %v", files)
	}
	if dirs := e.uploadDirs(); len(dirs) != 0 {
		t.Errorf("resume state left after the abort: %v", dirs)
	}
	if okPuts(e, bigName, 1) != accepted {
		t.Error("the changed first part was acknowledged")
	}
	for _, seal := range rec.firstParts(firstPartSize)[interrupted:] {
		if int64(len(seal.segments)) == volumeformat.MinSegmentsPerPart {
			t.Error("the changed first part's last segment was sealed")
		}
	}

	e.cloud.mu.Lock()
	e.cloud.onDeclare = nil
	e.cloud.mu.Unlock()
	if _, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir)); err != nil {
		t.Fatal(err)
	}
	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	rec.requireSafe(t)
}

// AC 2: a resume reads every file again rather than trusting the hash cache, so a write inside an
// acknowledged part that moves neither the size, the modification time nor the change time still aborts
// the saved session, and the stale part never reaches the Volume.
func TestResumeRehashesAFileTheCacheWouldTrust(t *testing.T) {
	// A filesystem where a write does not move the change time.
	defer func(saved func(string, fs.FileInfo) int64) { fileChangeTime = saved }(fileChangeTime)
	fileChangeTime = func(string, fs.FileInfo) int64 { return 1 }
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 2)
	if okPuts(e, bigName, 1) != 1 {
		t.Fatalf("before the resume: puts = %+v, want the first part acknowledged", e.cloud.putLog())
	}
	big := filepath.Join(dir, bigName)
	info, err := os.Stat(big)
	if err != nil {
		t.Fatal(err)
	}
	patchFile(t, big, 10, []byte("changed"))
	if err := os.Chtimes(big, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(big)
	if err != nil {
		t.Fatal(err)
	}
	opts := resumeOptions(e, &rec, vol, dir)
	var hashed []string
	opts.onHash = func(p string) { hashed = append(hashed, p) }

	_, err = Run(t.Context(), opts)

	if !errors.Is(err, ErrSourceChanged) || !strings.Contains(err.Error(), resolved) {
		t.Fatalf("resume returned %v, want a source-change error naming %s", err, resolved)
	}
	if !slices.Contains(hashed, resolved) {
		t.Errorf("hashed %v: the resume trusted the cached digest of %s", hashed, resolved)
	}
	if e.cloud.uploadState() != fakeAborted {
		t.Errorf("session state = %q, want aborted", e.cloud.uploadState())
	}
	if files, _ := e.cloud.latest(vol); len(files) != 0 {
		t.Errorf("files = %v, want no publication after a source change", files)
	}
	if okPuts(e, bigName, 1) != 1 {
		t.Errorf("puts = %+v, want the first part acknowledged once", e.cloud.putLog())
	}
}

// AC 2: a resume compares each part's digest from hashing with the digest journalled before the part
// first went out, so a journalled digest that differs aborts the saved session before any part is sent
// again, even when the file's own digest still matches.
func TestResumeAbortsWhenAJournalledPartDigestDiffers(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 2)
	other := sha256.Sum256([]byte("other bytes"))
	edited := 0
	editJournal(t, e.configDir, func(r map[string]any) map[string]any {
		if r["type"] == recordPart && r["part"] == float64(1) && r["length"] == float64(firstPartSize) {
			r["sha256"] = hex.EncodeToString(other[:])
			edited++
		}
		return r
	})
	if edited != 1 {
		t.Fatalf("edited %d part records, want the first part of %s", edited, bigName)
	}
	puts := len(e.cloud.putLog())
	resolved, err := filepath.EvalSymlinks(filepath.Join(dir, bigName))
	if err != nil {
		t.Fatal(err)
	}

	_, err = Run(t.Context(), resumeOptions(e, &rec, vol, dir))

	want := resolved + ": part 1 no longer matches what was hashed, so nothing from this upload was published; run put again"
	if !errors.Is(err, ErrSourceChanged) || err.Error() != want {
		t.Fatalf("resume returned %q, want %q", err, want)
	}
	if e.cloud.uploadState() != fakeAborted {
		t.Errorf("session state = %q, want aborted", e.cloud.uploadState())
	}
	if files, _ := e.cloud.latest(vol); len(files) != 0 {
		t.Errorf("files = %v, want no publication", files)
	}
	if sent := e.cloud.putLog()[puts:]; len(sent) != 0 {
		t.Errorf("the resume sent %+v before it aborted", sent)
	}
}

// AC 2: a retry reads the part from the file again, and a part whose bytes changed since they were
// journalled stops before its last segment is sealed: the put fails and the changed bytes never go out.
func TestRetryOfAChangedPartFailsBeforeItCompletes(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	content := patterned(5000, 1)
	file := singleFile(t, e, "weights.bin", content)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	e.cloud.fault = func(rq partRequest) *fakeFault {
		if rq.Attempt == 1 {
			patchFile(t, file, 0, []byte("changed"))
			return &fakeFault{
				Status: 503,
				Slug:   slugPartStorageUnavailable,
			}
		}
		return nil
	}
	opts := e.options(vol, file)
	opts.newSealer = rec.factory

	_, err := Run(t.Context(), opts)

	assertAborted(t, e, err, file, vol, 0)
	if errors.Is(err, errUploadRetriesExhausted) {
		t.Errorf("error = %v, want the change, not exhausted retries", err)
	}
	seals := rec.firstParts(5000)
	if len(seals) != 2 || bytes.Equal(seals[0].salt, seals[1].salt) {
		t.Fatalf("%d sealings, want the first send and one retry under a fresh salt", len(seals))
	}
	if len(seals[0].segments) != 1 || seals[0].segments[0] != sha256.Sum256(content) || len(seals[1].segments) != 0 {
		t.Errorf("sealed %d and %d segments, want the original bytes once and nothing of the changed part", len(seals[0].segments), len(seals[1].segments))
	}
	if okPuts(e, "weights.bin", 1) != 0 {
		t.Error("a part was acknowledged")
	}
	if naive := rec.requireSafe(t); naive != 0 {
		t.Errorf("%d segments sealed other plaintext under the same data key", naive)
	}
}

// AC 2: a retry of an unchanged part reads it from the file again and seals the same plaintext, every
// segment of it, under a fresh salt.
func TestRetryResealsTheSameBytes(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	size := 3*volumeformat.SegmentPlaintextBytes + 7
	file := singleFile(t, e, "weights.bin", patterned(size, 1))
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	e.cloud.fault = func(rq partRequest) *fakeFault {
		if rq.Attempt == 1 {
			return &fakeFault{
				Status: 503,
				Slug:   slugPartStorageUnavailable,
			}
		}
		return nil
	}
	opts := e.options(vol, file)
	opts.newSealer = rec.factory

	if _, err := Run(t.Context(), opts); err != nil {
		t.Fatal(err)
	}

	seals := rec.firstParts(int64(size))
	if len(seals) != 2 || bytes.Equal(seals[0].salt, seals[1].salt) || len(seals[0].segments) != 4 || !slices.Equal(seals[0].segments, seals[1].segments) {
		t.Fatal("the retry must seal every segment of the same plaintext under a fresh salt")
	}
	requireFiles(t, e, vol, expectedFiles(t, e.srcDir, ""))
	rec.requireSafe(t)
}

// AC 3: resuming from a snapshot taken before a part was acknowledged seals that part again under a
// fresh salt, so no key and nonce pair repeats, and the session still completes.
func TestResumeFromRolledBackState(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	e.cloud.onPut = func(rq partRequest) {
		switch {
		case rq.Path == bigName && rq.Part == 1:
			copyTree(t, e.stateDir(), snapshot)
		case rq.Path == bigName && rq.Part == 2:
			cancel()
		}
	}
	if _, err := Run(ctx, resumeOptions(e, &rec, vol, dir)); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want the interrupt", err)
	}
	e.cloud.onPut = nil
	restoreTree(t, snapshot, e.stateDir())

	if _, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir)); err != nil {
		t.Fatal(err)
	}

	seals := rec.firstParts(firstPartSize)
	if len(seals) != 2 || bytes.Equal(seals[0].salt, seals[1].salt) || !bytes.Equal(seals[0].dataKey, seals[1].dataKey) {
		t.Errorf("the rolled-back part must be sealed again under the same data key and a fresh salt")
	}
	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	rec.requireSafe(t)
}

// AC 3: state rolled back to before a part's digest was journalled cannot tell that the file changed
// before the part was read again. The part is sealed with other plaintext under the same data key, and
// only the per-part salt keeps every key and nonce pair unique; the whole-file check then aborts.
func TestRolledBackStateNeverRepeatsAKeyAndNonce(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	big := filepath.Join(dir, bigName)
	// The second job waits for the first sealer before the reader can check
	// the whole-file digest on the last part.
	if err := os.Truncate(big, 2*firstPartSize+1); err != nil {
		t.Fatal(err)
	}
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	e.cloud.mu.Lock()
	e.cloud.onDeclare = func(FileDeclarations) { copyTree(t, e.stateDir(), snapshot) }
	e.cloud.mu.Unlock()
	interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 2)
	restoreTree(t, snapshot, e.stateDir())
	e.cloud.mu.Lock()
	e.cloud.onDeclare = func(FileDeclarations) { patchFile(t, big, 10, []byte("changed")) }
	e.cloud.mu.Unlock()

	_, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir))

	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("error = %v, want the changed file", err)
	}
	if e.cloud.count("open") != 1 {
		t.Errorf("calls = %v, want the saved session resumed", e.cloud.callLog())
	}
	if files, _ := e.cloud.latest(vol); len(files) != 0 {
		t.Errorf("an aborted session published %v", files)
	}
	if naive := rec.requireSafe(t); naive == 0 {
		t.Error("the test did not seal other plaintext under a used data key and part, so it proves nothing")
	}
}

// AC 3: two copies of the same state resume one session; the second finds it completed, seals nothing
// new and removes its state.
func TestResumeFromCopiedState(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 2)
	copyDir := filepath.Join(t.TempDir(), "config")
	copyTree(t, e.configDir, copyDir)

	if _, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir)); err != nil {
		t.Fatal(err)
	}
	sealed := len(rec.all())
	opts := resumeOptions(e, &rec, vol, dir)
	opts.ConfigDir = copyDir
	_, err := Run(t.Context(), opts)

	if err == nil {
		t.Fatal("a copy of completed state resumed a completed session")
	}
	if got := len(rec.all()); got != sealed {
		t.Errorf("the copy sealed %d parts for a completed session", got-sealed)
	}
	if entries, _ := os.ReadDir(filepath.Join(copyDir, stateDirName, uploadsDirName)); len(entries) != 0 {
		t.Errorf("the copy's state was kept: %v", entries)
	}
	if e.cloud.count("complete") != 1 {
		t.Errorf("calls = %v, want one completion", e.cloud.callLog())
	}
	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	rec.requireSafe(t)
}

// AC 4: resume state is 0700 and 0600, a resume narrows modes a copy widened, and completion removes it.
func TestResumeStateModes(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == goosWindows {
		t.Skip("POSIX modes")
	}
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 2)
	snap := readJournal(t, e.configDir)
	for p, want := range map[string]fs.FileMode{
		e.stateDir(): 0o700,
		filepath.Join(e.stateDir(), uploadsDirName): 0o700,
		snap.Dir:                             0o700,
		filepath.Join(snap.Dir, journalName): 0o600,
	} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s is %o, want %o", p, got, want)
		}
	}
	if err := os.Chmod(snap.Dir, 0o755); err != nil { //nolint:gosec // widens the state as a careless copy would
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(snap.Dir, journalName), 0o644); err != nil { //nolint:gosec // widens the state as a careless copy would
		t.Fatal(err)
	}
	var during journalSnapshot
	e.cloud.onPut = func(partRequest) { during = readJournal(t, e.configDir) }

	if _, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir)); err != nil {
		t.Fatal(err)
	}

	if during.DirMode != 0o700 || during.Mode != 0o600 {
		t.Errorf("during the resume the state is %o and %o, want 700 and 600", during.DirMode, during.Mode)
	}
	if dirs := e.uploadDirs(); len(dirs) != 0 {
		t.Errorf("resume state left after completion: %v", dirs)
	}
}

// AC 4: expired state is removed, for every Volume, and unreadable state once it is older than any session.
func TestExpiredStateIsRemoved(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	other := e.cloud.newVolume("other", "")
	var rec sealRecorder
	interruptedPut(t, e, resumeOptions(e, &rec, other, dir), 2)
	editJournal(t, e.configDir, func(r map[string]any) map[string]any {
		if r["type"] == recordSession {
			r["expiresAt"] = time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
		}
		return r
	})
	root := filepath.Join(e.stateDir(), uploadsDirName)
	oldBroken, newBroken := filepath.Join(root, "old-broken"), filepath.Join(root, "new-broken")
	for _, d := range []string{oldBroken, newBroken} {
		writeFile(t, filepath.Join(d, journalName), []byte("not json\n"))
	}
	old := time.Now().Add(-unreadableStateAge - time.Hour)
	if err := os.Chtimes(oldBroken, old, old); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir)); err != nil {
		t.Fatal(err)
	}

	dirs := e.uploadDirs()
	if len(dirs) != 1 || dirs[0] != newBroken {
		t.Errorf("resume state left = %v, want only the recent unreadable directory", dirs)
	}
	if e.cloud.count("open") != 2 || e.cloud.count("abort") != 0 {
		t.Errorf("calls = %v, want a new session and no abort of the expired one", e.cloud.callLog())
	}
}

// A saved key that no longer opens under the session sealing key is lost: the session is aborted and the
// put starts over.
func TestLostKeyAbortsTheSavedSession(t *testing.T) {
	t.Parallel()
	cases := map[string]func(t *testing.T, snap journalSnapshot, index int){
		"a key that does not open": func(t *testing.T, snap journalSnapshot, index int) {
			keys := snap.Keys[index]
			keys.DataKey = make([]byte, 32)
			line, err := encodeLine(keys)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(snap.Dir, fileKeyName(index)), line, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"a missing key": func(t *testing.T, snap journalSnapshot, index int) {
			if err := os.Remove(filepath.Join(snap.Dir, fileKeyName(index))); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, lose := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newTestEnv(t)
			dir := resumeTree(t, e)
			vol := e.cloud.newVolume("model-weights", "")
			var rec sealRecorder
			interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 2)
			snap := readJournal(t, e.configDir)
			index := -1
			for _, r := range snap.ofType(recordFile) {
				if r["path"] == bigName {
					index = int(r["index"].(float64))
				}
			}
			if _, ok := snap.Keys[index]; !ok {
				t.Fatalf("no saved keys for %s", bigName)
			}
			lose(t, snap, index)

			if _, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir)); err != nil {
				t.Fatal(err)
			}

			if e.cloud.count("abort") != 1 || e.cloud.count("open") != 2 {
				t.Errorf("calls = %v, want the saved session aborted and a new one opened", e.cloud.callLog())
			}
			if !strings.Contains(e.progress.String(), "a saved file key was lost") {
				t.Errorf("progress %q does not say the key was lost", e.progress.String())
			}
			requireFiles(t, e, vol, expectedFiles(t, dir, ""))
			rec.requireSafe(t)
		})
	}
}

// A file whose content the session reused has no saved keys left; the resume still continues the session.
func TestResumeWithAReusedFile(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	small := singleFile(t, e, "small.txt", patterned(3000, 7))
	e.mustPut(e.cloud.newVolume("earlier", ""), small)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 2)
	snap := readJournal(t, e.configDir)
	reused := -1
	for _, r := range snap.ofType(recordDeclared) {
		if r["state"] == stateReused {
			reused = int(r["index"].(float64))
		}
	}
	if _, ok := snap.Keys[reused]; reused < 0 || ok {
		t.Fatalf("declared records = %v, keys = %d: want one reused file without keys", snap.ofType(recordDeclared), len(snap.Keys))
	}
	opens := e.cloud.count("open")

	if _, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir)); err != nil {
		t.Fatal(err)
	}

	if e.cloud.count("abort") != 0 || e.cloud.count("open") != opens {
		t.Errorf("calls = %v, want the saved session resumed", e.cloud.callLog())
	}
	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	rec.requireSafe(t)
}

// State saved in another journal format is not resumed: the session is aborted and the put starts over.
func TestOtherJournalVersionAbortsTheSavedSession(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 2)
	editJournal(t, e.configDir, func(r map[string]any) map[string]any {
		if r["type"] == recordSession {
			r["version"] = journalVersion + 1
		}
		return r
	})

	if _, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir)); err != nil {
		t.Fatal(err)
	}

	if e.cloud.count("abort") != 1 || e.cloud.count("open") != 2 {
		t.Errorf("calls = %v, want the saved session aborted and a new one opened", e.cloud.callLog())
	}
	if !strings.Contains(e.progress.String(), "saved in another format") {
		t.Errorf("progress %q does not say the format differs", e.progress.String())
	}
	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
}

// A crash during a journal write leaves a torn last line; the resume drops it and carries on.
func TestResumeDropsATornLastLine(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 2)
	snap := readJournal(t, e.configDir)
	f, err := os.OpenFile(filepath.Join(snap.Dir, journalName), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"ack","ind`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir)); err != nil {
		t.Fatal(err)
	}

	if e.cloud.count("open") != 1 || okPuts(e, bigName, 1) != 1 {
		t.Errorf("calls = %v, want the session resumed without resending its acknowledged part", e.cloud.callLog())
	}
	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
}

// AC 5: an interrupted and resumed upload at debug level writes no data key, session sealing key,
// sealed key, part token or acknowledgement to its output, its errors or its log.
func TestResumeCanaryHoldsNoSecrets(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	var logs lockedBuffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	opts := resumeOptions(e, &rec, vol, dir)
	opts.Logger = logger
	interruptErr := interruptedPut(t, e, opts, 2)
	opts = resumeOptions(e, &rec, vol, dir)
	opts.Logger = logger
	sum, err := Run(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	var text, js bytes.Buffer
	if err := sum.WriteText(&text); err != nil {
		t.Fatal(err)
	}
	if err := output.PrintTo(&js, output.FormatJSON, sum); err != nil {
		t.Fatal(err)
	}
	out := strings.Join([]string{e.progress.String(), logs.String(), interruptErr, text.String(), js.String()}, "\n")
	if !strings.Contains(logs.String(), "resuming session") {
		t.Fatalf("the debug log %q does not show the resume", logs.String())
	}

	secrets := append(e.keys.keyMaterial(), rec.dataKeys()...)
	for _, s := range secrets {
		for _, form := range keyForms(s) {
			if strings.Contains(out, form) {
				t.Errorf("key material appears in the output")
			}
		}
	}
	for _, s := range e.cloud.issued() {
		if strings.Contains(out, s) {
			t.Errorf("a part token or acknowledgement appears in the output")
		}
	}
}

// A resumed put that is interrupted again keeps the session for the next resume, as a fresh put does.
func TestInterruptedResumeKeepsTheSession(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 2)

	// A signal ends the context with its own cause, which the failed requests do not carry.
	errSignal := errors.New("interrupt signal received")
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	e.cloud.onPut = func(rq partRequest) {
		if rq.Path == "small.txt" {
			cancel(errSignal)
		}
	}
	_, err := Run(ctx, resumeOptions(e, &rec, vol, dir))
	e.cloud.onPut = nil

	if !errors.Is(err, errSignal) || !strings.Contains(err.Error(), "run put again with the same paths to resume") {
		t.Fatalf("the interrupted resume returned %v, want the interrupt and how to resume", err)
	}
	if e.cloud.count("abort") != 0 || e.cloud.uploadState() != fakeOpen || len(e.uploadDirs()) != 1 {
		t.Fatalf("calls = %v, state = %q, resume state = %v; want the session and its state kept",
			e.cloud.callLog(), e.cloud.uploadState(), e.uploadDirs())
	}

	if _, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir)); err != nil {
		t.Fatalf("second resume: %v", err)
	}
	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	if e.cloud.count("open") != 1 || e.cloud.count("complete") != 1 || len(e.uploadDirs()) != 0 {
		t.Errorf("calls = %v, resume state = %v; want one session completed once and no state left", e.cloud.callLog(), e.uploadDirs())
	}
	rec.requireSafe(t)
}

// A resumed put that runs out of part retries keeps its acknowledged parts for the next run.
func TestResumeOutOfRetriesKeepsSession(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	// Set before the first put: a part handler the interrupt left running still reads it.
	var failing atomic.Bool
	e.cloud.fault = func(partRequest) *fakeFault {
		if !failing.Load() {
			return nil
		}
		return &fakeFault{
			Status: http.StatusServiceUnavailable,
			Slug:   slugPartStorageUnavailable,
		}
	}
	interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 2)

	failing.Store(true)
	_, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir))
	failing.Store(false)

	if !errors.Is(err, errUploadRetriesExhausted) || !strings.Contains(err.Error(), "run put again with the same paths to resume") {
		t.Fatalf("error = %v, want retry exhaustion ending by telling the user to run put again", err)
	}
	if e.cloud.count("abort") != 0 || e.cloud.uploadState() != fakeOpen || len(e.uploadDirs()) != 1 {
		t.Fatalf("calls = %v, state = %q, resume state = %v; want the session and state kept",
			e.cloud.callLog(), e.cloud.uploadState(), e.uploadDirs())
	}

	if _, err := Run(t.Context(), resumeOptions(e, &rec, vol, dir)); err != nil {
		t.Fatalf("put after the outage: %v", err)
	}
	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	if e.cloud.count("open") != 1 || okPuts(e, bigName, 1) != 1 {
		t.Errorf("calls = %v, want the same session and no resend of the first part", e.cloud.callLog())
	}
	rec.requireSafe(t)
}

// A resumed put retries its completion through an outage, as a fresh put does.
func TestResumedCompletionRecoversAfterExtendedOutage(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	dir := resumeTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	interruptedPut(t, e, resumeOptions(e, &rec, vol, dir), 2)
	opts := resumeOptions(e, &rec, vol, dir)
	api := &unavailableCompletion{API: e.cloud}
	opts.API = api
	var pauses []time.Duration
	opts.sleep = func(ctx context.Context, d time.Duration) error {
		api.elapsed += d
		pauses = append(pauses, d)
		return ctx.Err()
	}

	sum, err := Run(t.Context(), opts)

	if err != nil || !sum.Changed || e.cloud.count("complete") != 1 || e.cloud.count("open") != 1 {
		t.Fatalf("summary = %+v, error = %v, calls = %v; want the resumed session published after a %v outage",
			sum, err, e.cloud.callLog(), uploadTestOutage)
	}
	checkUploadPauses(t, pauses)
	if len(e.uploadDirs()) != 0 {
		t.Errorf("resume state left after completion: %v", e.uploadDirs())
	}
}
