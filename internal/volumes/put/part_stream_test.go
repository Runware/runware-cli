package put

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/runware/runware-cli/pkg/volumeformat"
)

// changingTransport changes the file once a part's body has yielded after bytes, reads the rest of
// the body as a transport would, and records how much of it there was.
type changingTransport struct {
	after  int64
	change func()

	mu    sync.Mutex
	calls int
	read  []int64
}

func (c *changingTransport) PutPart(_ context.Context, _ Target, body io.Reader, _ int64) (Secret, error) {
	c.mu.Lock()
	c.calls++
	first := c.calls == 1
	c.mu.Unlock()
	n, err := io.CopyN(io.Discard, body, c.after)
	if err == nil {
		if first {
			c.change()
		}
		var rest int64
		rest, err = io.Copy(io.Discard, body)
		n += rest
	}
	c.mu.Lock()
	c.read = append(c.read, n)
	c.mu.Unlock()
	if err != nil {
		return "", err
	}
	return testAck, nil
}

// TestChangeInsideAStreamingPartStopsBeforeItsLastSegment: bytes that change in a part while it
// streams fail the send before the part's last segment goes out, without a retry.
func TestChangeInsideAStreamingPartStopsBeforeItsLastSegment(t *testing.T) {
	t.Parallel()
	segments := int64(volumeformat.MinSegmentsPerPart)
	plain := patterned(int(segments*volumeformat.SegmentPlaintextBytes), 3)
	tr := &changingTransport{after: volumeformat.SaltBytes + 2*volumeformat.SegmentBytes}
	r, f, digest := newTestPart(t, tr, plain)
	tr.change = func() { patchFile(t, f.src.Abs, 40*volumeformat.SegmentPlaintextBytes, []byte("changed")) }

	_, err := r.sendPart(t.Context(), f, 1, digest)

	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("error = %v, want the changed source", err)
	}
	length, _ := f.layout.PartLength(1)
	if want := length - volumeformat.SegmentBytes; tr.calls != 1 || len(tr.read) != 1 || tr.read[0] != want {
		t.Errorf("%d sends read %v bytes, want one send of %d: every segment but the last", tr.calls, tr.read, want)
	}
}

// TestChangeInsideAStreamingPartAbortsThePut: a file changed while one of its parts streams is never
// acknowledged, and the put aborts and publishes nothing.
func TestChangeInsideAStreamingPartAbortsThePut(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	file := filepath.Join(e.srcDir, "big.bin")
	writeTwoPartFile(t, file)
	vol := e.cloud.newVolume("model-weights", "")
	var rec sealRecorder
	var once sync.Once
	opts := e.options(vol, file)
	opts.Parallelism = 1
	opts.newSealer = rec.factory
	opts.Transport = &partHook{
		Transport: opts.Transport,
		after:     volumeformat.SaltBytes + 2*volumeformat.SegmentBytes,
		hook: func(part int) {
			if part == 1 {
				once.Do(func() { patchFile(t, file, 40*volumeformat.SegmentPlaintextBytes, []byte("changed")) })
			}
		},
	}

	_, err := Run(t.Context(), opts)

	assertAborted(t, e, err, file, vol, 0)
	if okPuts(e, "big.bin", 1) != 0 {
		t.Error("the changed part was acknowledged")
	}
	for _, seal := range rec.firstParts(firstPartSize) {
		if int64(len(seal.segments)) == volumeformat.MinSegmentsPerPart {
			t.Error("the changed part's last segment was sealed")
		}
	}
}

// partHook runs hook with the part number once the part's body has yielded after bytes.
type partHook struct {
	Transport
	after int64
	hook  func(part int)
}

func (h *partHook) PutPart(ctx context.Context, target Target, body io.Reader, length int64) (Secret, error) {
	return h.Transport.PutPart(ctx, target, &hookedBody{
		r:     body,
		left:  h.after,
		fire:  func() { h.hook(target.PartNumber) },
		fired: false,
	}, length)
}

type hookedBody struct {
	r     io.Reader
	left  int64
	fire  func()
	fired bool
}

func (b *hookedBody) Read(p []byte) (int, error) {
	if !b.fired && b.left <= 0 {
		b.fired = true
		b.fire()
	}
	if !b.fired {
		p = p[:min(int64(len(p)), b.left)]
	}
	n, err := b.r.Read(p)
	b.left -= int64(n)
	return n, err
}

// discardTransport reads every part body and acknowledges it.
type discardTransport struct{}

func (discardTransport) PutPart(_ context.Context, _ Target, body io.Reader, _ int64) (Secret, error) {
	if _, err := io.Copy(io.Discard, body); err != nil {
		return "", err
	}
	return testAck, nil
}

// TestPartInFlightHoldsAboutOneSegment: sending a part allocates about one segment of memory, the
// same for a part of one segment and for a full 64-segment part, rather than the part's plaintext.
func TestPartInFlightHoldsAboutOneSegment(t *testing.T) {
	if testing.Short() {
		t.Skip("sends a 64 MiB part")
	}
	var allocated [2]int64
	for i, segments := range []int{1, volumeformat.MinSegmentsPerPart} {
		r, f, digest := newTestPart(t, discardTransport{}, bytes.Repeat([]byte{7}, segments*volumeformat.SegmentPlaintextBytes))
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		if _, err := r.sendPart(t.Context(), f, 1, digest); err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		allocated[i] = int64(after.TotalAlloc - before.TotalAlloc)
		t.Logf("a part of %d segments allocated %d KiB", segments, allocated[i]>>10)
		if allocated[i] > 2*volumeformat.SegmentBytes {
			t.Errorf("a part of %d segments allocated %d bytes, want at most two segments", segments, allocated[i])
		}
	}
	if allocated[1] > allocated[0]+256<<10 {
		t.Errorf("a 64-segment part allocated %d bytes, %d more than a one-segment part", allocated[1], allocated[1]-allocated[0])
	}
}

var errDiskRead = errors.New("input/output error")

// failingReadAt opens files whose ReadAt fails with errDiskRead past byte limit. Hashing reads with
// Read and is unaffected; the send reads each part with ReadAt.
type failingReadAt struct {
	limit int64
}

func (o failingReadAt) open(path string) (sourceFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &failingFile{
		File:  f,
		limit: o.limit,
	}, nil
}

type failingFile struct {
	*os.File
	limit int64
}

func (f *failingFile) ReadAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) <= f.limit {
		return f.File.ReadAt(p, off)
	}
	n, err := f.File.ReadAt(p[:max(f.limit-off, 0)], off)
	if err != nil {
		return n, err
	}
	return n, errDiskRead
}

// countingTransport counts the part sends.
type countingTransport struct {
	Transport
	calls atomic.Int64
}

func (c *countingTransport) PutPart(ctx context.Context, target Target, body io.Reader, length int64) (Secret, error) {
	c.calls.Add(1)
	return c.Transport.PutPart(ctx, target, body, length)
}

// TestPutStopsAtAReadErrorOfAStreamingPart: an I/O error reading a part while it streams stops the
// put at once with that error, rather than being retried as a failure of the network.
func TestPutStopsAtAReadErrorOfAStreamingPart(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	file := filepath.Join(e.srcDir, "data.bin")
	writeFile(t, file, patterned(3*volumeformat.SegmentPlaintextBytes, 5))
	vol := e.cloud.newVolume("model-weights", "")
	opts := e.options(vol, file)
	failing := failingReadAt{
		limit: volumeformat.SegmentPlaintextBytes + 100,
	}
	opts.openSource = failing.open
	tr := &countingTransport{
		Transport: opts.Transport,
	}
	opts.Transport = tr

	_, err := Run(t.Context(), opts)

	if !errors.Is(err, errDiskRead) || errors.Is(err, errUploadRetriesExhausted) {
		t.Errorf("error = %v, want the read error", err)
	}
	if n := tr.calls.Load(); n != 1 {
		t.Errorf("the part was sent %d times, want once", n)
	}
}
