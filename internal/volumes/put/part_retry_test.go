package put

import (
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/runware/runware-cli/pkg/volumeformat"
)

type stalledPartTransport struct {
	stall time.Duration
	calls int
}

func (s *stalledPartTransport) PutPart(ctx context.Context, _ Target, _ io.Reader, _ int64) (Secret, error) {
	s.calls++
	if err := sleepContext(ctx, s.stall); err != nil {
		return "", err
	}
	return "", errPartStalled
}

func TestPartRetryTimeBudget(t *testing.T) {
	for _, stall := range []time.Duration{0, 2 * time.Minute} {
		t.Run(stall.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				plain := []byte("test part")
				layout, err := volumeformat.PlanLayout(int64(len(plain)))
				if err != nil {
					t.Fatal(err)
				}
				now := time.Now()
				expires := now.Add(time.Hour)
				tr := &stalledPartTransport{stall: stall}
				r := runner{
					tr:        tr,
					now:       time.Now,
					sleep:     sleepContext,
					expiresAt: expires,
				}
				f := fileState{
					key:      &fakeFileKey{dataKey: make([]byte, 32)},
					layout:   layout,
					identity: volumeformat.FileIdentity{Size: uint64(len(plain))},
					targets: map[int]Target{
						1: {ExpiresAt: expires},
					},
				}
				if _, err := r.sendPart(t.Context(), &f, 1, plain); !errors.Is(err, errUploadRetriesExhausted) {
					t.Fatalf("error = %v, want retry exhaustion", err)
				}
				retryTime := time.Since(now) - stall
				if retryTime < 3*time.Minute || retryTime > 5*time.Minute || tr.calls > 18 {
					t.Errorf("retried for %v over %d attempts, want 3 to 5 minutes and at most 18 attempts", retryTime, tr.calls)
				}
				if stall > 0 && retryTime != 5*time.Minute {
					t.Errorf("stalled retries took %v, want the deadline to stop the in-flight attempt", retryTime)
				}
			})
		})
	}
}
