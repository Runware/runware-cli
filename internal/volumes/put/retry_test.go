package put

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
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
	t.Parallel()
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
	t.Parallel()
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

func TestPublicationRetryRefusesChangedSource(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", []byte("original"))
	vol := e.cloud.newVolume("weights", "")
	opts := e.options(vol, file)
	api := &unavailableCompletion{API: e.cloud}
	opts.API = api
	opts.sleep = func(ctx context.Context, _ time.Duration) error {
		rewrite(t, file, []byte("changed"))
		api.elapsed = uploadTestOutage
		return ctx.Err()
	}
	if _, err := Run(t.Context(), opts); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("put = %v, want the publication retry to detect the changed source", err)
	}
	if files, _ := e.cloud.latest(vol); len(files) != 0 || e.cloud.count("complete") != 0 || e.cloud.count("abort") != 1 {
		t.Fatalf("files = %v, calls = %v, want an abort without publication", files, e.cloud.callLog())
	}
}

type transientAbort struct {
	API
	calls int
}

func (a *transientAbort) AbortUpload(ctx context.Context, ref UploadRef) error {
	a.calls++
	if a.calls == 1 {
		return &ProblemError{Status: http.StatusBadGateway}
	}
	if err := a.API.AbortUpload(ctx, ref); err != nil {
		return err
	}
	if a.calls == 2 {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func TestSourceChangeRetriesAbort(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", patterned(20, 1))
	vol := e.cloud.newVolume("model-weights", "")
	e.cloud.onOpen = func() { rewrite(t, file, patterned(30, 2)) }
	api := &transientAbort{API: e.cloud}
	opts := e.options(vol, file)
	opts.API = api

	_, err := Run(t.Context(), opts)

	if !errors.Is(err, ErrSourceChanged) || errors.Is(err, errUploadRetriesExhausted) || errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("error = %v, want the original source change after abort succeeds", err)
	}
	if api.calls != 3 || e.cloud.count("abort") != 2 || e.cloud.uploadState() != fakeAborted {
		t.Fatalf("attempts = %d, calls = %v, state = %q; want a successful repeated abort", api.calls, e.cloud.callLog(), e.cloud.uploadState())
	}
	if dirs := e.uploadDirs(); e.cloud.count("complete") != 0 || len(dirs) != 0 {
		t.Errorf("calls = %v, resume state = %v; want no publication or leftover state", e.cloud.callLog(), dirs)
	}
}

func TestRenewalBackoffDoesNotPassSessionDeadline(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

func TestAbortDeadlinePreservesLastServerError(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		unavailable := &ProblemError{Status: http.StatusServiceUnavailable}
		api := &failingAbort{err: unavailable}
		r := runner{
			api:   api,
			sleep: sleepContext,
		}
		start := time.Now()
		err := r.abort(t.Context(), ErrSourceChanged)
		if elapsed := time.Since(start); elapsed != 15*time.Second || api.calls < 2 || api.calls >= maxUploadAttempts {
			t.Errorf("abort took %v and made %d attempts, want retries within a 15 second deadline", elapsed, api.calls)
		}
		if !errors.Is(err, ErrSourceChanged) || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, unavailable) {
			t.Errorf("error = %v, want the source change, deadline and last server error", err)
		}
	})
}

type lostCompletionResponse struct {
	API
	called bool
}

func (a *lostCompletionResponse) CompleteUpload(ctx context.Context, ref UploadRef) (UploadResult, error) {
	if !a.called {
		a.called = true
		if _, err := a.API.CompleteUpload(ctx, ref); err != nil {
			return UploadResult{}, err
		}
	}
	return UploadResult{}, io.ErrUnexpectedEOF
}

func TestLostCompletionCanBeRecovered(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"cancelled during backoff", "retry limit reached"} {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			file := singleFile(t, e, "weights.bin", patterned(20, 1))
			vol := e.cloud.newVolume("model-weights", "")
			opts := e.options(vol, file)
			opts.API = &lostCompletionResponse{API: e.cloud}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := errUploadRetriesExhausted
			if name == "cancelled during backoff" {
				want = context.Canceled
				opts.sleep = func(ctx context.Context, _ time.Duration) error {
					cancel()
					return context.Cause(ctx)
				}
			}
			_, err := Run(ctx, opts)
			if !errors.Is(err, want) || !strings.Contains(err.Error(), "publication may have completed") {
				t.Fatalf("error = %v, want %v and the uncertain completion warning", err, want)
			}
			if e.cloud.count("abort") != 0 || len(e.uploadDirs()) != 1 {
				t.Fatalf("calls = %v, state = %v, want the journal kept for the result", e.cloud.callLog(), e.uploadDirs())
			}
			sum, err := Run(t.Context(), e.options(vol, file))
			if err != nil {
				t.Fatalf("recover completion: %v", err)
			}
			if !sum.Changed || sum.Added != 1 || sum.BytesSent != 0 ||
				len(sum.Files) != 1 || sum.Files[0].Action != ActionAdded || !sum.recovered {
				t.Fatalf("recovered summary = %+v, want the original publication and zero bytes resent", sum)
			}
			if e.cloud.count("open") != 1 || len(e.cloud.putLog()) != 1 || len(e.uploadDirs()) != 0 {
				t.Fatalf("calls = %v, state = %v, want one upload and no remaining journal", e.cloud.callLog(), e.uploadDirs())
			}
			if files, _ := e.cloud.latest(vol); len(files) != 1 || e.cloud.uploadState() != fakeCompleted {
				t.Fatalf("files = %v, state = %q, want the original publication", files, e.cloud.uploadState())
			}
		})
	}
}

func TestUploadAPIPreservesAuthErrorsWithoutRetrying(t *testing.T) {
	t.Parallel()
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

var errCompletionStalled = errors.New("the completion request timed out")

// stalledCompletion answers every completion after a stall with a timeout.
type stalledCompletion struct {
	API
	stall  time.Duration
	starts []time.Time
}

func (s *stalledCompletion) wait(ctx context.Context) error {
	s.starts = append(s.starts, time.Now())
	if err := sleepContext(ctx, s.stall); err != nil {
		return err
	}
	return errCompletionStalled
}

func (s *stalledCompletion) CompleteFile(ctx context.Context, _ UploadRef, _ string, _ FileCompletion) (UploadFile, error) {
	return UploadFile{}, s.wait(ctx)
}

func (s *stalledCompletion) CompleteUpload(ctx context.Context, _ UploadRef) (UploadResult, error) {
	return UploadResult{}, s.wait(ctx)
}

// TestCompletionRetryTimeBudget: when every completion stalls until the
// request limit, put stops retrying after maxCompletionRetryTime, not after
// maxUploadAttempts stalls.
func TestCompletionRetryTimeBudget(t *testing.T) {
	t.Parallel()
	calls := map[string]func(ctx context.Context, r *runner) error{
		"file": func(ctx context.Context, r *runner) error {
			return r.completeFile(ctx, &fileState{src: &Source{Abs: "weights.bin"}})
		},
		"session": func(ctx context.Context, r *runner) error {
			_, err := r.completeUpload(ctx, nil)
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				api := &stalledCompletion{stall: 5 * time.Minute}
				r := &runner{
					api:   api,
					sleep: sleepContext,
				}
				start := time.Now()
				err := call(t.Context(), r)
				if !errors.Is(err, errUploadRetriesExhausted) || !errors.Is(err, errCompletionStalled) {
					t.Fatalf("error = %v, want retry exhaustion and the last timeout", err)
				}
				for _, s := range api.starts[1:] {
					if s.Sub(start) >= maxCompletionRetryTime {
						t.Errorf("started another attempt %v after the first", s.Sub(start))
					}
				}
				elapsed := time.Since(start)
				if elapsed > maxCompletionRetryTime+api.stall+maxUploadBackoff {
					t.Errorf("retried for %v over %d attempts, want the retry window plus one attempt", elapsed, len(api.starts))
				}
				// A fixed bound, so a longer window fails too: a 15 minute
				// window fits three 5 minute stalls.
				if elapsed >= 20*time.Minute || len(api.starts) != 3 {
					t.Errorf("retried for %v over %d attempts, want 3 attempts in under 20 minutes", elapsed, len(api.starts))
				}
			})
		})
	}
}
