package put

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/runware/runware-cli/pkg/volumeformat"
)

// testAck is the acknowledgement the test transports answer a part with.
const testAck = "test-ack"

type stalledPartTransport struct {
	stall  time.Duration
	starts []time.Time
}

func (s *stalledPartTransport) PutPart(ctx context.Context, _ Target, _ io.Reader, _ int64) (Secret, error) {
	s.starts = append(s.starts, time.Now())
	if err := sleepContext(ctx, s.stall); err != nil {
		return "", err
	}
	return "", errPartStalled
}

func sendTestPart(t *testing.T, tr Transport) (partResult, error) {
	t.Helper()
	r, f, digest := newTestPart(t, tr, []byte("test part"))
	return r.sendPart(t.Context(), f, 1, digest)
}

// testBufferedPartBytes stands in for maxBufferedPartBytes, so a part that proves progress is small.
const testBufferedPartBytes = 4 << 10

// sendLargeTestPart sends a part of twice testBufferedPartBytes with the runner's threshold lowered
// to testBufferedPartBytes, so an attempt that sends all of it proves progress.
func sendLargeTestPart(t *testing.T, tr Transport) (partResult, error) {
	t.Helper()
	return sendSizedTestPart(t, tr, 2*testBufferedPartBytes, testBufferedPartBytes)
}

// sendSizedTestPart sends a part of size bytes with the runner threshold at buffered, or at
// maxBufferedPartBytes when buffered is zero.
func sendSizedTestPart(t *testing.T, tr Transport, size int, buffered int64) (partResult, error) {
	t.Helper()
	r, f, digest := newTestPart(t, tr, make([]byte, size))
	r.bufferedPartBytes = buffered
	return r.sendPart(t.Context(), f, 1, digest)
}

// newTestPart is a runner sending through tr and a one-part file of plain, with the part's digest.
func newTestPart(t *testing.T, tr Transport, plain []byte) (*runner, *fileState, [32]byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "part.bin")
	if err := os.WriteFile(path, plain, 0o600); err != nil {
		t.Fatal(err)
	}
	layout, err := volumeformat.PlanLayout(int64(len(plain)))
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour)
	r := &runner{
		tr:        tr,
		now:       time.Now,
		sleep:     sleepContext,
		expiresAt: expires,
	}
	f := &fileState{
		src:      &Source{Abs: path},
		key:      &fakeFileKey{dataKey: make([]byte, 32)},
		layout:   layout,
		identity: volumeformat.FileIdentity{Size: uint64(len(plain))},
		targets: map[int]Target{
			1: {ExpiresAt: expires},
		},
	}
	return r, f, sha256.Sum256(plain)
}

func TestPartRetryTimeBudget(t *testing.T) {
	for _, stall := range []time.Duration{0, 2 * time.Minute} {
		t.Run(stall.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				now := time.Now()
				tr := &stalledPartTransport{stall: stall}
				if _, err := sendTestPart(t, tr); !errors.Is(err, errUploadRetriesExhausted) {
					t.Fatalf("error = %v, want retry exhaustion", err)
				}
				firstFailure := now.Add(stall)
				retryTime := time.Since(firstFailure)
				if retryTime < 3*time.Minute || retryTime > 5*time.Minute+stall || len(tr.starts) > 18 {
					t.Errorf("retried for %v over %d attempts, want 3 to 5 minutes plus at most one in-flight attempt", retryTime, len(tr.starts))
				}
				for _, start := range tr.starts[1:] {
					if start.Sub(firstFailure) >= 5*time.Minute {
						t.Errorf("started another attempt after the retry window at %v", start.Sub(firstFailure))
					}
				}
			})
		})
	}
}

type slowSuccessfulRetry struct{ calls int }

func (s *slowSuccessfulRetry) PutPart(ctx context.Context, _ Target, body io.Reader, _ int64) (Secret, error) {
	s.calls++
	if s.calls == 1 {
		return "", errPartStalled
	}
	if err := sleepContext(ctx, 6*time.Minute); err != nil {
		return "", err
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		return "", err
	}
	return testAck, nil
}

func TestPartRetryCanFinishAfterRetryWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &slowSuccessfulRetry{}
		result, err := sendTestPart(t, tr)
		if err != nil || result.Ack != testAck || tr.calls != 2 {
			t.Errorf("ack = %q, error = %v, attempts = %d; want one successful slow retry", result.Ack, err, tr.calls)
		}
	})
}

// droppingTransport reads each part body in full, waits attemptTime and then fails the first drops
// attempts, as a connection that drops at the end of a slow send would. With partial set, a failing
// attempt reads only that many bytes of the body first.
type droppingTransport struct {
	attemptTime time.Duration
	drops       int
	partial     int64
	calls       int
}

var errConnectionDropped = errors.New("the connection was reset")

func (d *droppingTransport) PutPart(ctx context.Context, _ Target, body io.Reader, _ int64) (Secret, error) {
	d.calls++
	if d.calls <= d.drops && d.partial > 0 {
		body = io.LimitReader(body, d.partial)
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		return "", err
	}
	if err := sleepContext(ctx, d.attemptTime); err != nil {
		return "", err
	}
	if d.calls <= d.drops {
		return "", errConnectionDropped
	}
	return testAck, nil
}

// TestPartRetryWindowRestartsAfterSlowAttempts: attempts that each send for longer than the retry
// window, as one part does among many on a slow link, keep getting retries.
func TestPartRetryWindowRestartsAfterSlowAttempts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &droppingTransport{
			attemptTime: 14 * time.Minute,
			drops:       3,
			calls:       0,
		}
		result, err := sendLargeTestPart(t, tr)
		if err != nil || result.Ack != testAck || tr.calls != 4 {
			t.Errorf("ack = %q, error = %v, attempts = %d; want three retries, the last acknowledged", result.Ack, err, tr.calls)
		}
	})
}

// TestPartRetryWindowRestartsAfterAPartialSend: a connection that drops partway through a large
// part, after sending 17 MiB of 18, more than the production threshold but less than the part,
// restarts the retry window, so slow attempts that each drop before the end keep getting retries.
func TestPartRetryWindowRestartsAfterAPartialSend(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &droppingTransport{
			attemptTime: 6 * time.Minute,
			drops:       2,
			partial:     17 << 20,
			calls:       0,
		}
		result, err := sendSizedTestPart(t, tr, 18<<20, 0)
		if err != nil || result.Ack != testAck || tr.calls != 3 {
			t.Errorf("ack = %q, error = %v, attempts = %d; want two retries, the last acknowledged", result.Ack, err, tr.calls)
		}
	})
}

// TestPartFastFailuresStopAtTheAttemptLimit: attempts that send the body and fail at once restart
// the retry window every time, and still stop at the attempt limit.
func TestPartFastFailuresStopAtTheAttemptLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &droppingTransport{
			attemptTime: 0,
			drops:       2 * maxUploadAttempts,
			calls:       0,
		}
		if _, err := sendLargeTestPart(t, tr); !errors.Is(err, errUploadRetriesExhausted) || tr.calls != maxUploadAttempts {
			t.Errorf("error = %v after %d attempts, want retry exhaustion after %d", err, tr.calls, maxUploadAttempts)
		}
	})
}

// bufferedStallTransport fails the first attempt after reading its body in full, then reads buffered
// bytes of each later body and stalls, as a link that stops moving data does while the client's
// buffers still take the start of every attempt.
type bufferedStallTransport struct {
	buffered int64
	stall    time.Duration
	starts   []time.Time
	ends     []time.Time
}

func (b *bufferedStallTransport) PutPart(ctx context.Context, _ Target, body io.Reader, _ int64) (Secret, error) {
	b.starts = append(b.starts, time.Now())
	defer func() { b.ends = append(b.ends, time.Now()) }()
	if len(b.starts) == 1 {
		if _, err := io.Copy(io.Discard, body); err != nil {
			return "", err
		}
		return "", errConnectionDropped
	}
	if _, err := io.CopyN(io.Discard, body, b.buffered); err != nil {
		return "", err
	}
	if err := sleepContext(ctx, b.stall); err != nil {
		return "", err
	}
	return "", errPartStalled
}

// TestPartStalledRetriesStopAtTheRetryWindow: after an attempt that sends the part and fails, attempts
// that stall having sent no more than the buffers hold do not restart the retry window, so the part
// stops within it rather than at the attempt limit.
//
// The last case keeps the production threshold and stalls after 16 MiB, written as a literal so
// that lowering maxBufferedPartBytes below the send buffers it is sized for fails the test.
func TestPartStalledRetriesStopAtTheRetryWindow(t *testing.T) {
	cases := []struct {
		threshold int64 // the runner's threshold; zero is maxBufferedPartBytes
		buffered  int64 // bytes each stalled attempt sends
		size      int
	}{
		{threshold: testBufferedPartBytes, buffered: 0, size: 2 * testBufferedPartBytes},
		{threshold: testBufferedPartBytes, buffered: testBufferedPartBytes, size: 2 * testBufferedPartBytes},
		{threshold: 0, buffered: 16 << 20, size: 17 << 20},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%d of %d bytes buffered", c.buffered, c.size), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tr := &bufferedStallTransport{
					buffered: c.buffered,
					stall:    2 * time.Minute,
					starts:   nil,
					ends:     nil,
				}
				if _, err := sendSizedTestPart(t, tr, c.size, c.threshold); !errors.Is(err, errUploadRetriesExhausted) {
					t.Fatalf("error = %v, want retry exhaustion", err)
				}
				if len(tr.starts) < 3 {
					t.Errorf("%d attempts, want the sent one and at least two stalled ones", len(tr.starts))
				}
				for _, start := range tr.starts[1:] {
					if late := start.Sub(tr.ends[0]); late > 5*time.Minute {
						t.Errorf("started an attempt %v after the first one ended, want at most 5 minutes", late)
					}
				}
			})
		})
	}
}
