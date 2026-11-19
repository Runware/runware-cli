package put

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/runware/runware-cli/internal/api/transport"
)

const retryRenewOperation = "renew"

const uploadTestOutage = 3 * time.Minute

func checkUploadPauses(t *testing.T, pauses []time.Duration) {
	t.Helper()
	window := 500 * time.Millisecond
	for i, delay := range pauses {
		if delay < window/2 || delay >= window {
			t.Errorf("pause %d = %v, want [%v, %v)", i+1, delay, window/2, window)
		}
		window = min(2*window, 30*time.Second)
	}
}

type transientUploadAPI struct {
	API
	operation string
	calls     int
}

func failFirstUploadCall[T any](a *transientUploadAPI, operation string, call func() (T, error)) (T, error) {
	if a.operation == operation {
		a.calls++
		if a.calls == 1 {
			var empty T
			if operation == retryRenewOperation {
				return empty, &ProblemError{Status: http.StatusBadGateway}
			}
			if _, err := call(); err != nil {
				return empty, err
			}
			return empty, io.ErrUnexpectedEOF
		}
	}
	return call()
}

func (a *transientUploadAPI) DeclareFiles(ctx context.Context, ref UploadRef, files FileDeclarations) (DeclarationResult, error) {
	return failFirstUploadCall(a, "declare", func() (DeclarationResult, error) {
		return a.API.DeclareFiles(ctx, ref, files)
	})
}

func (a *transientUploadAPI) RenewParts(ctx context.Context, ref UploadRef, fileID string, parts []int) ([]Target, error) {
	return failFirstUploadCall(a, retryRenewOperation, func() ([]Target, error) {
		return a.API.RenewParts(ctx, ref, fileID, parts)
	})
}

func (a *transientUploadAPI) CompleteFile(ctx context.Context, ref UploadRef, fileID string, completion FileCompletion) (UploadFile, error) {
	return failFirstUploadCall(a, "complete-file", func() (UploadFile, error) {
		return a.API.CompleteFile(ctx, ref, fileID, completion)
	})
}

func (a *transientUploadAPI) CompleteUpload(ctx context.Context, ref UploadRef) (UploadResult, error) {
	return failFirstUploadCall(a, "complete", func() (UploadResult, error) {
		return a.API.CompleteUpload(ctx, ref)
	})
}

func TestPutRetriesTransientUploadAPICalls(t *testing.T) {
	for _, operation := range []string{"declare", retryRenewOperation, "complete-file", "complete"} {
		t.Run(operation, func(t *testing.T) {
			e := newTestEnv(t)
			file := singleFile(t, e, "weights.bin", patterned(20, 1))
			vol := e.cloud.newVolume("model-weights", "")
			if operation == retryRenewOperation {
				e.cloud.declareTTL = -time.Minute
			}
			api := &transientUploadAPI{
				API:       e.cloud,
				operation: operation,
			}
			opts := e.options(vol, file)
			opts.API = api
			var pauses []time.Duration
			opts.sleep = func(ctx context.Context, d time.Duration) error {
				pauses = append(pauses, d)
				return ctx.Err()
			}

			sum, err := Run(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			files, digest := e.cloud.latest(vol)
			if api.calls != 2 || len(pauses) != 1 {
				t.Errorf("requests = %d, pauses = %v; want two requests and one backoff", api.calls, pauses)
			}
			checkUploadPauses(t, pauses)
			if len(files) != 1 || sum.ManifestSHA256 != digest || !sum.Changed || sum.Added != 1 {
				t.Errorf("summary = %+v, want the original result of publishing one file", sum)
			}
			if e.cloud.count("open") != 1 || e.cloud.count("abort") != 0 {
				t.Errorf("calls = %v, want one session and no abort", e.cloud.callLog())
			}
		})
	}
}

type unavailableCompletion struct {
	API
	elapsed time.Duration
}

func (a *unavailableCompletion) CompleteUpload(ctx context.Context, ref UploadRef) (UploadResult, error) {
	if a.elapsed < uploadTestOutage {
		return UploadResult{}, &ProblemError{Status: http.StatusServiceUnavailable}
	}
	return a.API.CompleteUpload(ctx, ref)
}

func TestUploadCompletionRecoversAfterExtendedOutage(t *testing.T) {
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", patterned(20, 1))
	vol := e.cloud.newVolume("model-weights", "")
	opts := e.options(vol, file)
	api := &unavailableCompletion{API: e.cloud}
	opts.API = api
	var pauses []time.Duration
	opts.sleep = func(ctx context.Context, d time.Duration) error {
		api.elapsed += d
		pauses = append(pauses, d)
		return ctx.Err()
	}

	sum, err := Run(t.Context(), opts)

	if err != nil || !sum.Changed || e.cloud.count("complete") != 1 {
		t.Fatalf("summary = %+v, error = %v; want publication after a %v outage", sum, err, uploadTestOutage)
	}
	checkUploadPauses(t, pauses)
}

func TestRenewalBackoffDoesNotPassSessionDeadline(t *testing.T) {
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", patterned(20, 1))
	vol := e.cloud.newVolume("model-weights", "")
	e.cloud.declareTTL = -time.Minute
	api := &transientUploadAPI{
		API:       e.cloud,
		operation: retryRenewOperation,
	}
	opts := e.options(vol, file)
	opts.API = api
	opts.sleep = func(ctx context.Context, _ time.Duration) error {
		e.skew.Store(int64(2 * fakeSessionTTL))
		return ctx.Err()
	}

	_, err := Run(t.Context(), opts)
	if !errors.Is(err, ErrSessionExpired) || api.calls != 1 {
		t.Fatalf("error = %v, renew requests = %d; want the deadline without a second request", err, api.calls)
	}
}

func TestUploadAPIStopsRetriesOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls, pauses := 0, 0
	_, err := retryUploadAPI(ctx, func(context.Context, time.Duration) error {
		pauses++
		return nil
	}, func() (int, error) {
		calls++
		cancel()
		return 0, &ProblemError{Status: http.StatusBadGateway}
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || pauses != 0 {
		t.Fatalf("error = %v, requests = %d, pauses = %d; want cancellation without a retry", err, calls, pauses)
	}
}

func TestUploadAPIPreservesAuthErrorsWithoutRetrying(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			authErr := transport.CreateRunwareError("forbidden", http.StatusText(status), transport.RunwareErrorDetails{
				StatusCode: status,
			})
			calls, pauses := 0, 0
			_, err := retryUploadAPI(t.Context(), func(context.Context, time.Duration) error {
				pauses++
				return nil
			}, func() (int, error) {
				calls++
				return 0, authErr
			})
			if !errors.Is(err, authErr) || !transport.IsAuthError(err) || calls != 1 || pauses != 0 {
				t.Fatalf("error = %v, requests = %d, pauses = %d; want the original auth error without a retry", err, calls, pauses)
			}
		})
	}
}
