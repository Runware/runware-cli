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
	plain := []byte("test part")
	layout, err := volumeformat.PlanLayout(int64(len(plain)))
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour)
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
	return r.sendPart(t.Context(), &f, 1, plain)
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
	return "test-ack", nil
}

func TestPartRetryCanFinishAfterRetryWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &slowSuccessfulRetry{}
		result, err := sendTestPart(t, tr)
		if err != nil || result.Ack != "test-ack" || tr.calls != 2 {
			t.Errorf("ack = %q, error = %v, attempts = %d; want one successful slow retry", result.Ack, err, tr.calls)
		}
	})
}
