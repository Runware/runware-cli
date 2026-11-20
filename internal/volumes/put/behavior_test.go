package put

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/output"
	"github.com/runware/runware-cli/pkg/volumeformat"
)

// emptySHA256 is the SHA-256 of the empty string, which an empty file is declared with.
const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func singleFile(t *testing.T, e *testEnv, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(e.srcDir, name)
	writeFile(t, p, content)
	return p
}

func statuses(puts []putRecord) []int {
	out := make([]int, 0, len(puts))
	for _, p := range puts {
		out = append(out, p.Status)
	}
	return out
}

// TestPutTwoPartFile completes a two-part file with the CRC of the whole ciphertext.
func TestPutTwoPartFile(t *testing.T) {
	e := newTestEnv(t)
	file := filepath.Join(e.srcDir, "big.bin")
	writeTwoPartFile(t, file)
	vol := e.cloud.newVolume("model-weights", "")

	sum := e.mustPut(vol, file)

	parts := map[int]int{}
	for _, p := range e.cloud.putLog() {
		if p.Status == http.StatusOK {
			parts[p.Part]++
		}
	}
	if len(parts) != 2 || parts[1] != 1 || parts[2] != 1 {
		t.Errorf("accepted parts = %v, want parts 1 and 2 once each", parts)
	}
	if want := ciphertextSize(t, twoPartSize); sum.BytesSent != want {
		t.Errorf("sent %d bytes, want the object's %d", sum.BytesSent, want)
	}
	got, _ := e.cloud.latest(vol)
	hash, err := fileSHA256(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Size != twoPartSize || got[0].SHA256 != hash {
		t.Errorf("latest files = %+v", got)
	}
}

// TestZeroLengthFileOnePart: an empty file is one part of a salt and a tag.
func TestZeroLengthFileOnePart(t *testing.T) {
	e := newTestEnv(t)
	file := singleFile(t, e, "empty.txt", nil)
	vol := e.cloud.newVolume("model-weights", "")

	sum := e.mustPut(vol, file)

	puts := e.cloud.putLog()
	if len(puts) != 1 || puts[0].Part != 1 || len(puts[0].Salt) != 16 {
		t.Errorf("parts sent = %+v, want one part", puts)
	}
	if sum.BytesSent != 32 {
		t.Errorf("sent %d bytes, want the 32 of a salt and a tag", sum.BytesSent)
	}
	if got, _ := e.cloud.latest(vol); len(got) != 1 || got[0].SHA256 != emptySHA256 || got[0].Size != 0 {
		t.Errorf("latest files = %+v", got)
	}
}

// TestRetryUsesNewSealer: a retried part is sealed again under a fresh salt from the same buffer.
func TestRetryUsesNewSealer(t *testing.T) {
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", patterned(5000, 1))
	vol := e.cloud.newVolume("model-weights", "")
	e.cloud.fault = func(rq partRequest) *fakeFault {
		if rq.Attempt == 1 {
			return &fakeFault{
				Status: http.StatusServiceUnavailable,
				Slug:   slugPartStorageUnavailable,
			}
		}
		return nil
	}

	sum := e.mustPut(vol, file)

	puts := e.cloud.putLog()
	if got := statuses(puts); !slices.Equal(got, []int{http.StatusServiceUnavailable, http.StatusOK}) {
		t.Fatalf("part statuses = %v, want a refusal then an acceptance", got)
	}
	if len(puts[0].Salt) != 16 || bytes.Equal(puts[0].Salt, puts[1].Salt) {
		t.Errorf("the retry reused the salt %x", puts[0].Salt)
	}
	if want := ciphertextSize(t, 5000); sum.BytesSent != want {
		t.Errorf("sent %d bytes, want %d: a retried part counts once", sum.BytesSent, want)
	}
}

func TestStorageFailureAbortsAfterRetries(t *testing.T) {
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", patterned(5000, 1))
	vol := e.cloud.newVolume("model-weights", "")
	e.cloud.fault = func(partRequest) *fakeFault {
		return &fakeFault{
			Status: http.StatusServiceUnavailable,
			Slug:   slugPartStorageUnavailable,
		}
	}

	_, err := e.put(vol, file)

	if !errors.Is(err, ErrPartStorageUnavailable) || !errors.Is(err, errUploadRetriesExhausted) {
		t.Fatalf("error = %v, want a storage failure", err)
	}
	if !strings.HasSuffix(err.Error(), "; run put again") {
		t.Errorf("error = %q, want it to end by telling the user to run put again", err)
	}
	if got := len(e.cloud.putLog()); got != maxUploadAttempts {
		t.Errorf("%d tries, want %d", got, maxUploadAttempts)
	}
	if e.cloud.count("abort") != 1 || e.cloud.count("complete") != 0 || e.cloud.uploadState() != fakeAborted {
		t.Errorf("calls = %v, state = %q, want the session aborted", e.cloud.callLog(), e.cloud.uploadState())
	}
	if dirs := e.uploadDirs(); len(dirs) != 0 {
		t.Errorf("resume state left after retries: %v", dirs)
	}
}

func TestPartRecoversAfterExtendedOutage(t *testing.T) {
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", patterned(5000, 1))
	vol := e.cloud.newVolume("model-weights", "")
	var elapsed atomic.Int64
	e.cloud.fault = func(partRequest) *fakeFault {
		if elapsed.Load() >= int64(uploadTestOutage) {
			return nil
		}
		return &fakeFault{
			Status: http.StatusServiceUnavailable,
			Slug:   slugPartStorageUnavailable,
		}
	}
	var (
		mu     sync.Mutex
		pauses []time.Duration
	)
	opts := e.options(vol, file)
	opts.sleep = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		pauses = append(pauses, d)
		elapsed.Add(int64(d))
		return ctx.Err()
	}

	if _, err := Run(t.Context(), opts); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if elapsed.Load() < int64(uploadTestOutage) || e.cloud.count("complete") != 1 {
		t.Errorf("upload did not publish after recovering from a %v outage", uploadTestOutage)
	}
	checkUploadPauses(t, pauses)
}

func TestPartsUploadInParallel(t *testing.T) {
	e := newTestEnv(t)
	file := filepath.Join(e.srcDir, "big.bin")
	writeTwoPartFile(t, file)
	vol := e.cloud.newVolume("model-weights", "")
	var (
		arrived    atomic.Int32
		together   atomic.Bool
		secondSeen = make(chan struct{})
	)
	e.cloud.onPut = func(partRequest) {
		switch arrived.Add(1) {
		case 1:
			select {
			case <-secondSeen:
				together.Store(true)
			case <-time.After(10 * time.Second):
			}
		case 2:
			close(secondSeen)
		}
	}

	e.mustPut(vol, file)

	if !together.Load() {
		t.Error("the second part was not sent while the first was in flight")
	}
}

func TestParallelismBoundsPartRequestsAcrossFiles(t *testing.T) {
	const parallel = 2
	e := newTestEnv(t)
	vol := e.cloud.newVolume("model-weights", "")
	var paths []string
	for i := range 3 * parallel {
		paths = append(paths, singleFile(t, e, fmt.Sprintf("part-%d.bin", i), []byte{byte(i)}))
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var (
		inFlight atomic.Int32
		exceeded atomic.Bool
		arrived  = make(chan struct{}, len(paths))
		release  = make(chan struct{})
		once     sync.Once
	)
	unblock := func() { once.Do(func() { close(release) }) }
	e.cloud.onPut = func(partRequest) {
		if inFlight.Add(1) > parallel {
			exceeded.Store(true)
		}
		defer inFlight.Add(-1)
		arrived <- struct{}{}
		<-release
	}
	opts := e.options(vol, paths...)
	opts.Parallelism = parallel
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, opts)
		done <- err
		close(done)
	}()
	defer func() {
		unblock()
		cancel()
		<-done
	}()
	for range parallel {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal("the expected requests did not run together")
		}
	}
	select {
	case <-arrived:
		t.Error("another request started before an upload slot was free")
	case <-time.After(200 * time.Millisecond):
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if exceeded.Load() {
		t.Errorf("more than %d part requests overlapped", parallel)
	}
	if got := len(e.cloud.putLog()); got != len(paths) {
		t.Errorf("sent %d parts, want %d", got, len(paths))
	}
}

// TestParallelismIsBounded: zero or less runs four parts at once, and no more than sixteen run however many are asked for.
func TestParallelismIsBounded(t *testing.T) {
	for asked, want := range map[int]int{
		-3:   4,
		0:    4,
		1:    1,
		16:   16,
		17:   16,
		1000: 16,
	} {
		r, err := newRunner(Options{
			Organization: uuid.NewString(),
			ConfigDir:    t.TempDir(),
			Parallelism:  asked,
		})
		if err != nil {
			t.Fatal(err)
		}

		if r.parallel != want {
			t.Errorf("Parallelism %d runs %d parts at once, want %d", asked, r.parallel, want)
		}
	}
}

type unverifiedCompletion struct {
	API
}

func (u unverifiedCompletion) CompleteFile(ctx context.Context, up UploadRef, fileID string, c FileCompletion) (UploadFile, error) {
	f, err := u.API.CompleteFile(ctx, up, fileID, c)
	f.State = stateUploading
	return f, err
}

func TestPutRefusesAnUnverifiedCompletion(t *testing.T) {
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", patterned(5000, 1))
	vol := e.cloud.newVolume("model-weights", "")
	opts := e.options(vol, file)
	opts.API = unverifiedCompletion{API: e.cloud}

	_, err := Run(t.Context(), opts)

	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("error = %v, want a contract violation", err)
	}
	if e.cloud.count("abort") != 1 || e.cloud.count("complete") != 0 {
		t.Errorf("calls = %v, want an abort and no session completion", e.cloud.callLog())
	}
}

// refusedCompletion answers a completion call with a problem and passes the other calls on.
type refusedCompletion struct {
	API
	file    *ProblemError
	session *ProblemError
}

func (r refusedCompletion) CompleteFile(ctx context.Context, up UploadRef, fileID string, c FileCompletion) (UploadFile, error) {
	if r.file != nil {
		return UploadFile{}, r.file
	}
	return r.API.CompleteFile(ctx, up, fileID, c)
}

func (r refusedCompletion) CompleteUpload(ctx context.Context, up UploadRef) (UploadResult, error) {
	if r.session != nil {
		return UploadResult{}, r.session
	}
	return r.API.CompleteUpload(ctx, up)
}

type refusedCompletionCase struct {
	name      string
	file      *ProblemError
	session   *ProblemError
	wantLimit *LimitError
}

// TestPutAbortsWhenACompletionIsRefused: a refused completion names the limit or the file, aborts the session and publishes nothing.
func TestPutAbortsWhenACompletionIsRefused(t *testing.T) {
	cases := []refusedCompletionCase{
		{
			name: "the Volume would hold too many files",
			session: &ProblemError{
				Status: http.StatusConflict,
				Slug:   slugLimitExceeded,
				Detail: "maxFilesPerVolume exceeds its limit",
				Limit:  limitFilesPerVolume,
				Cap:    10000,
			},
			wantLimit: &LimitError{
				Limit: limitFilesPerVolume,
				Cap:   10000,
			},
		},
		{
			name: "a file's CRC does not match",
			file: &ProblemError{
				Status: http.StatusConflict,
				Slug:   "volume-upload-crc-mismatch",
				Detail: "the stored CRC64/NVME differs from the submitted one",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			file := singleFile(t, e, "weights.bin", patterned(5000, 1))
			vol := e.cloud.newVolume("model-weights", "")
			opts := e.options(vol, file)
			opts.API = refusedCompletion{
				API:     e.cloud,
				file:    tc.file,
				session: tc.session,
			}

			_, err := Run(t.Context(), opts)

			if tc.wantLimit != nil {
				limit, ok := errors.AsType[*LimitError](err)
				if !ok || *limit != *tc.wantLimit || !strings.Contains(err.Error(), "10000") {
					t.Errorf("error = %v, want the limit %+v and its cap in the message", err, *tc.wantLimit)
				}
			} else {
				resolved, evalErr := filepath.EvalSymlinks(file)
				if evalErr != nil {
					t.Fatal(evalErr)
				}
				p, isProblem := errors.AsType[*ProblemError](err)
				fe, isFile := errors.AsType[*FileError](err)
				if !isProblem || p.Slug != tc.file.Slug || !isFile || fe.Path != resolved {
					t.Errorf("error = %v, want the problem %s naming %s", err, tc.file.Slug, resolved)
				}
			}
			if e.cloud.count("abort") != 1 || e.cloud.uploadState() != fakeAborted {
				t.Errorf("calls = %v, state = %q, want the session aborted", e.cloud.callLog(), e.cloud.uploadState())
			}
			if files, _ := e.cloud.latest(vol); len(files) != 0 {
				t.Errorf("a refused completion published %v", files)
			}
			if !e.keys.allCleared() || len(e.uploadDirs()) != 0 {
				t.Errorf("keys cleared = %v, resume state = %v", e.keys.allCleared(), e.uploadDirs())
			}
		})
	}
}

func TestFileKeyClearedWhenItsFileCompletes(t *testing.T) {
	e := newTestEnv(t)
	dir := filepath.Join(e.srcDir, "pair")
	writeTree(t, dir, map[string][]byte{
		"a.bin": patterned(100, 1),
		"b.bin": patterned(100, 2),
	})
	vol := e.cloud.newVolume("model-weights", "")
	var firstCleared atomic.Bool
	e.cloud.onCompleteFile = func(path string) {
		if path == "b.bin" {
			cleared := e.keys.clearedFiles()
			firstCleared.Store(len(cleared) > 0 && cleared[0])
		}
	}
	opts := e.options(vol, dir)
	opts.Parallelism = 1

	if _, err := Run(t.Context(), opts); err != nil {
		t.Fatal(err)
	}

	if !firstCleared.Load() {
		t.Error("a completed file's data key was still held when the next file completed")
	}
}

func TestRenewsExpiredTokenBeforeSending(t *testing.T) {
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", patterned(5000, 1))
	vol := e.cloud.newVolume("model-weights", "")
	e.cloud.declareTTL = -time.Minute

	e.mustPut(vol, file)

	if got := statuses(e.cloud.putLog()); !slices.Equal(got, []int{http.StatusOK}) {
		t.Errorf("part statuses = %v, want a single send after the renewal", got)
	}
	if got := e.cloud.renewals(); !slices.Equal(got, []string{"weights.bin:[1]"}) {
		t.Errorf("renewals = %v, want one renewal of part 1", got)
	}
}

func TestRenewsTokenRefusedByEndpoint(t *testing.T) {
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", patterned(5000, 1))
	vol := e.cloud.newVolume("model-weights", "")
	e.cloud.fault = func(req partRequest) *fakeFault {
		if req.Attempt == 1 {
			return &fakeFault{Status: http.StatusUnauthorized, Slug: slugPartTokenExpired}
		}
		return nil
	}
	e.mustPut(vol, file)

	if got := statuses(e.cloud.putLog()); !slices.Equal(got, []int{http.StatusUnauthorized, http.StatusOK}) {
		t.Errorf("part statuses = %v, want an expired token then an acceptance", got)
	}
}

func TestRenewalGivesUp(t *testing.T) {
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", patterned(5000, 1))
	vol := e.cloud.newVolume("model-weights", "")
	e.cloud.fault = func(partRequest) *fakeFault {
		return &fakeFault{Status: http.StatusUnauthorized, Slug: slugPartTokenExpired}
	}

	_, err := e.put(vol, file)

	if !errors.Is(err, ErrPartTokenExpired) {
		t.Fatalf("error = %v, want the expired token", err)
	}
	if got := len(e.cloud.renewals()); got != maxRenewals {
		t.Errorf("%d renewals, want %d", got, maxRenewals)
	}
	if e.cloud.count("abort") != 1 {
		t.Errorf("calls = %v, want the session aborted", e.cloud.callLog())
	}
}

func TestRejectsAlreadyExpiredRenewal(t *testing.T) {
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", patterned(5000, 1))
	vol := e.cloud.newVolume("model-weights", "")
	e.cloud.declareTTL = -time.Minute
	e.cloud.renewTTL = -time.Minute

	_, err := e.put(vol, file)
	if !errors.Is(err, ErrPartTokenExpired) || len(e.cloud.renewals()) != 1 || len(e.cloud.putLog()) != 0 {
		t.Fatalf("error = %v, calls = %v; want one renewal and no part sends", err, e.cloud.callLog())
	}
}

// TestPutDeclaresInBatches: files are declared at most 100 at a time.
func TestPutDeclaresInBatches(t *testing.T) {
	e := newTestEnv(t)
	dir := filepath.Join(e.srcDir, "many")
	files := make(map[string][]byte, 105)
	for i := range 105 {
		files[fmt.Sprintf("f%03d.bin", i)] = patterned(10+i, byte(i))
	}
	writeTree(t, dir, files)
	vol := e.cloud.newVolume("model-weights", "")
	var (
		mu    sync.Mutex
		sizes []int
	)
	e.cloud.onDeclare = func(batch FileDeclarations) {
		mu.Lock()
		defer mu.Unlock()
		sizes = append(sizes, len(batch.Files))
	}

	sum := e.mustPut(vol, dir)

	mu.Lock()
	defer mu.Unlock()
	if want := []int{maxDeclarationBatch, 5}; !slices.Equal(sizes, want) {
		t.Errorf("declaration batches = %v, want %v", sizes, want)
	}
	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	if sum.Added != 105 {
		t.Errorf("%d files added, want 105", sum.Added)
	}
}

func TestRenewsMissingTargets(t *testing.T) {
	e := newTestEnv(t)
	file := filepath.Join(e.srcDir, "big.bin")
	writeTwoPartFile(t, file)
	vol := e.cloud.newVolume("model-weights", "")
	e.cloud.firstTargets = 1

	e.mustPut(vol, file)

	if got := e.cloud.renewals(); !slices.Equal(got, []string{"big.bin:[2]"}) {
		t.Errorf("renewals = %v, want part 2 only", got)
	}
	for _, p := range e.cloud.putLog() {
		if p.Status != http.StatusOK {
			t.Errorf("part %d was refused with %d, want every part sent with a valid token", p.Part, p.Status)
		}
	}
}

// renewalAPI answers RenewParts with answer and keeps the parts it was asked for.
type renewalAPI struct {
	API
	asked  []int
	answer func(parts []int) ([]Target, error)
}

func (a *renewalAPI) RenewParts(_ context.Context, _ UploadRef, _ string, parts []int) ([]Target, error) {
	a.asked = slices.Clone(parts)
	return a.answer(parts)
}

// targetsFor is a well-formed answer: one target for each part, in order.
func targetsFor(parts []int) []Target {
	targets := make([]Target, len(parts))
	expires := time.Now().Add(time.Minute)
	for i, p := range parts {
		targets[i] = Target{
			PartNumber: p,
			URL:        "https://upload.example.com/v1/parts",
			Token:      Secret("token"),
			ExpiresAt:  expires,
		}
	}
	return targets
}

// renewalRunner is a runner inside its session and a ten-gibibyte file, whose parts have no targets yet.
func renewalRunner(t *testing.T, api API) (*runner, *fileState) {
	t.Helper()
	layout, err := volumeformat.PlanLayout(10 << 30)
	if err != nil {
		t.Fatal(err)
	}
	r := &runner{
		api:       api,
		now:       time.Now,
		expiresAt: time.Now().Add(time.Hour),
	}
	f := &fileState{
		id:      "file",
		layout:  layout,
		targets: map[int]Target{},
	}
	return r, f
}

// TestRenewalAsksForAtMost128Parts: a renewal names the part that needs a target and the parts after it, 128 at the most.
func TestRenewalAsksForAtMost128Parts(t *testing.T) {
	api := &renewalAPI{answer: func(parts []int) ([]Target, error) { return targetsFor(parts), nil }}
	r, f := renewalRunner(t, api)

	if _, err := r.target(t.Context(), f, 1, ""); err != nil {
		t.Fatal(err)
	}
	if got := api.asked; len(got) != 128 || got[0] != 1 || got[len(got)-1] != 128 {
		t.Errorf("asked for %d parts, want parts 1 to 128", len(got))
	}
	if _, err := r.target(t.Context(), f, 129, ""); err != nil {
		t.Fatal(err)
	}
	var want []int
	for p := 129; p <= int(f.layout.PartCount); p++ {
		want = append(want, p)
	}
	if !slices.Equal(api.asked, want) {
		t.Errorf("asked for %d parts from part 129, want the %d that remain", len(api.asked), len(want))
	}
}

type renewalAnswerCase struct {
	name      string
	answer    func(parts []int) ([]Target, error)
	wantLimit *LimitError
}

// TestRenewalChecksTheAnswer: a short, reordered or misnumbered answer breaks the contract, and a quota refusal names the limit.
func TestRenewalChecksTheAnswer(t *testing.T) {
	cases := []renewalAnswerCase{
		{
			name:   "one target short",
			answer: func(parts []int) ([]Target, error) { return targetsFor(parts[:len(parts)-1]), nil },
		},
		{
			name: "targets in another order",
			answer: func(parts []int) ([]Target, error) {
				targets := targetsFor(parts)
				targets[0], targets[1] = targets[1], targets[0]
				return targets, nil
			},
		},
		{
			name: "a target for another part",
			answer: func(parts []int) ([]Target, error) {
				targets := targetsFor(parts)
				targets[0].PartNumber = 999
				return targets, nil
			},
		},
		{
			name: "the organization is over its stored quota",
			answer: func([]int) ([]Target, error) {
				return nil, &ProblemError{
					Status: http.StatusConflict,
					Slug:   slugLimitExceeded,
					Detail: "maxStoredBytes exceeds its limit",
					Limit:  limitStoredBytes,
					Cap:    1 << 40,
				}
			},
			wantLimit: &LimitError{
				Limit: limitStoredBytes,
				Cap:   1 << 40,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, f := renewalRunner(t, &renewalAPI{answer: tc.answer})

			_, err := r.target(t.Context(), f, 1, "")

			if tc.wantLimit != nil {
				if limit, ok := errors.AsType[*LimitError](err); !ok || *limit != *tc.wantLimit {
					t.Errorf("error = %v, want the limit %+v", err, *tc.wantLimit)
				}
				return
			}
			if !errors.Is(err, ErrProtocol) {
				t.Errorf("error = %v, want a contract violation", err)
			}
		})
	}
}

func TestNeverRenewsPastSessionDeadline(t *testing.T) {
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", patterned(5000, 1))
	vol := e.cloud.newVolume("model-weights", "")
	e.cloud.onDeclare = func(FileDeclarations) { e.skew.Store(int64(48 * time.Hour)) }

	_, err := e.put(vol, file)

	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("error = %v, want the session deadline", err)
	}
	if got := e.cloud.renewals(); len(got) != 0 {
		t.Errorf("renewed %v after the session deadline", got)
	}
	if e.cloud.count("abort") != 1 {
		t.Errorf("calls = %v, want the session aborted", e.cloud.callLog())
	}
}

type fatalPartCase struct {
	name  string
	fault fakeFault
	want  error
}

func TestAbortsOnFatalPartRefusal(t *testing.T) {
	cases := []fatalPartCase{
		{
			name: "volume-part-token-invalid",
			fault: fakeFault{
				Status: http.StatusUnauthorized,
				Slug:   slugPartTokenInvalid,
			},
			want: ErrPartTokenInvalid,
		},
		{
			name: "volume-part-length-mismatch",
			fault: fakeFault{
				Status: http.StatusBadRequest,
				Slug:   slugPartLengthMismatch,
			},
			want: ErrPartLengthMismatch,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			file := singleFile(t, e, "weights.bin", patterned(5000, 1))
			vol := e.cloud.newVolume("model-weights", "")
			e.cloud.fault = func(partRequest) *fakeFault { return &tc.fault }

			_, err := e.put(vol, file)

			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if !strings.Contains(err.Error(), "weights.bin") || !strings.Contains(err.Error(), "part 1") {
				t.Errorf("error %q does not name the file and the part", err)
			}
			if got := len(e.cloud.putLog()); got != 1 {
				t.Errorf("%d tries, want the refusal to stand", got)
			}
			if e.cloud.count("abort") != 1 || e.cloud.count("complete") != 0 || e.cloud.uploadState() != fakeAborted {
				t.Errorf("calls = %v, state = %q", e.cloud.callLog(), e.cloud.uploadState())
			}
			if files, _ := e.cloud.latest(vol); len(files) != 0 {
				t.Errorf("a refused upload published %v", files)
			}
		})
	}
}

type cancelOnSecondPart struct {
	Transport
	cancel context.CancelFunc
	calls  int
}

func (c *cancelOnSecondPart) PutPart(ctx context.Context, target Target, body io.Reader, length int64) (Secret, error) {
	c.calls++
	if c.calls == 2 {
		c.cancel()
		return "", context.Cause(ctx)
	}
	return c.Transport.PutPart(ctx, target, body, length)
}

type observedAbort struct {
	API
	before func()
}

func (a observedAbort) AbortUpload(ctx context.Context, ref UploadRef) error {
	a.before()
	return a.API.AbortUpload(ctx, ref)
}

func TestInterruptAbortsAndCleansUploadJournal(t *testing.T) {
	e := newTestEnv(t)
	first := singleFile(t, e, "first.bin", patterned(5000, 1))
	second := singleFile(t, e, "second.bin", patterned(5000, 2))
	vol := e.cloud.newVolume("model-weights", "")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	opts := e.options(vol, first, second)
	opts.Parallelism = 1
	opts.Transport = &cancelOnSecondPart{
		Transport: opts.Transport,
		cancel:    cancel,
	}
	var snap journalSnapshot
	opts.API = observedAbort{
		API:    e.cloud,
		before: journalAt(t, e, &snap),
	}

	_, err := Run(ctx, opts)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want the cancellation", err)
	}
	if _, ok := errors.AsType[*FileError](err); ok {
		t.Errorf("error = %v, want the interrupt on its own, not a failure of one file", err)
	}
	if e.cloud.count("abort") != 1 || e.cloud.count("complete") != 0 || e.cloud.uploadState() != fakeAborted {
		t.Errorf("calls = %v, state = %q, want an aborted session and no completion", e.cloud.callLog(), e.cloud.uploadState())
	}
	if acks := snap.ofType(recordAck); len(acks) != 1 || acks[0]["index"] != float64(0) {
		t.Error("the journal does not contain the first file's acknowledgement")
	}
	if done := snap.ofType(recordComplete); len(done) != 1 || done[0]["index"] != float64(0) {
		t.Fatal("the first file is not recorded as complete in the journal")
	}
	if _, ok := snap.Keys[0]; ok {
		t.Error("the completed file still has resume keys")
	}
	if len(snap.Keys[1].DataKey) != 32 {
		t.Error("the interrupted file lost its resume key")
	}
	if !e.keys.allCleared() {
		t.Error("a key was not cleared after the interrupt")
	}
	if dirs := e.uploadDirs(); len(dirs) != 0 {
		t.Errorf("resume state left after the interrupt: %v", dirs)
	}
}

// failingAbort is an API whose session cannot be aborted.
type failingAbort struct {
	API
	err   error
	calls int
}

func (f *failingAbort) AbortUpload(context.Context, UploadRef) error {
	f.calls++
	return f.err
}

type abortCase struct {
	name             string
	err              error
	wantCalls        int
	wantAbortFailure bool
}

func TestAbortFailureStillRemovesJournal(t *testing.T) {
	cases := []abortCase{
		{
			name: "the API is unreachable",
			err: &ProblemError{
				Status: http.StatusServiceUnavailable,
				Detail: "unavailable",
			},
			wantCalls:        maxUploadAttempts,
			wantAbortFailure: true,
		},
		{
			name: "the session is already gone",
			err: &ProblemError{
				Status: http.StatusConflict,
				Slug:   slugUploadNotOpen,
				Detail: "expired",
			},
			wantCalls: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			file := singleFile(t, e, "weights.bin", patterned(64, 1))
			vol := e.cloud.newVolume("model-weights", "")
			e.cloud.onOpen = func() { rewrite(t, file, patterned(80, 2)) }
			opts := e.options(vol, file)
			api := &failingAbort{
				API: e.cloud,
				err: tc.err,
			}
			opts.API = api

			_, err := Run(t.Context(), opts)

			if !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("error = %v, want the source change", err)
			}
			if api.calls != tc.wantCalls {
				t.Errorf("abort attempts = %d, want %d", api.calls, tc.wantCalls)
			}
			if got := errors.Is(err, tc.err); got != tc.wantAbortFailure {
				t.Errorf("error = %v, exposes abort failure = %v, want %v", err, got, tc.wantAbortFailure)
			}
			if dirs := e.uploadDirs(); len(dirs) != 0 {
				t.Errorf("resume state left on disk: %v", dirs)
			}
		})
	}
}

func TestJournalDropsReusedFileKeys(t *testing.T) {
	e := newTestEnv(t)
	reused := singleFile(t, e, "known.bin", []byte("already stored"))
	missing := singleFile(t, e, "missing.bin", []byte("needs transfer"))
	e.mustPut(e.cloud.newVolume("original", ""), reused)

	e.cloud.fault = func(partRequest) *fakeFault {
		return &fakeFault{
			Status: http.StatusUnauthorized,
			Slug:   slugPartTokenInvalid,
		}
	}
	opts := e.options(e.cloud.newVolume("another", ""), reused, missing)
	var snap journalSnapshot
	opts.API = observedAbort{
		API:    e.cloud,
		before: journalAt(t, e, &snap),
	}
	_, err := Run(t.Context(), opts)
	if !errors.Is(err, ErrPartTokenInvalid) || e.cloud.uploadState() != fakeAborted {
		t.Fatalf("error = %v, state = %q, want a refused part and an aborted session", err, e.cloud.uploadState())
	}

	if _, ok := snap.Keys[0]; ok {
		t.Error("the reused file still has resume keys")
	}
	if len(snap.Keys[1].DataKey) != 32 {
		t.Error("the in-flight file lost its resume key")
	}
}

func keyForms(b []byte) []string {
	return []string{
		hex.EncodeToString(b),
		base64.StdEncoding.EncodeToString(b),
		base64.RawURLEncoding.EncodeToString(b),
		fmt.Sprintf("%d", b),
		fmt.Sprintf("%v", b),
	}
}

// TestNoSecretsInLogs: no token, ack, key or upload URL reaches progress output, summaries or errors.
func TestNoSecretsInLogs(t *testing.T) {
	e := newTestEnv(t)
	dir := standardTree(t, e)
	var out strings.Builder

	sum := e.mustPut(e.cloud.newVolume("ok", ""), dir)
	var text, js bytes.Buffer
	if err := sum.WriteText(&text); err != nil {
		t.Fatal(err)
	}
	if err := output.PrintTo(&js, output.FormatJSON, sum); err != nil {
		t.Fatal(err)
	}
	out.WriteString(e.progress.String() + text.String() + js.String())

	e.cloud.fault = func(partRequest) *fakeFault {
		return &fakeFault{
			Status: http.StatusUnauthorized,
			Slug:   slugPartTokenInvalid,
		}
	}
	_, err := e.put(e.cloud.newVolume("refused", ""), singleFile(t, e, "refused.bin", patterned(100, 6)))
	if err == nil {
		t.Fatal("a refused part did not fail the put")
	}
	out.WriteString(err.Error())

	e.cloud.fault = nil
	url := e.cloud.server.URL
	e.cloud.server.Close()
	_, err = e.put(e.cloud.newVolume("unreachable", ""), singleFile(t, e, "unreachable.bin", patterned(100, 7)))
	if err == nil {
		t.Fatal("an unreachable upload endpoint did not fail the put")
	}
	out.WriteString(err.Error())

	got := out.String()
	secrets := slices.Clone(e.cloud.issued())
	for _, material := range e.keys.keyMaterial() {
		secrets = append(secrets, keyForms(material)...)
	}
	if len(secrets) < 10 {
		t.Fatalf("only %d secrets were collected, the test is not looking", len(secrets))
	}
	secrets = append(secrets, url)
	for i, secret := range secrets {
		if strings.Contains(got, secret) {
			t.Errorf("the output holds secret %d of %d", i, len(secrets))
		}
	}
}

func TestDeclareLimitErrorNamesFile(t *testing.T) {
	e := newTestEnv(t)
	dir := filepath.Join(e.srcDir, "data")
	writeTree(t, dir, map[string][]byte{
		"small.bin": patterned(50, 1),
		"big.bin":   patterned(200, 2),
	})
	vol := e.cloud.newVolume("model-weights", "")
	e.cloud.maxFileBytes = 100

	_, err := e.put(vol, dir)

	limit, ok := errors.AsType[*LimitError](err)
	if !ok || limit.Limit != limitFileBytes || limit.Cap != 100 {
		t.Fatalf("error = %v, want the file size limit of 100 bytes", err)
	}
	if !strings.Contains(err.Error(), "big.bin") || strings.Contains(err.Error(), "small.bin") || !strings.Contains(err.Error(), limitFileBytes) {
		t.Errorf("error %q does not name big.bin and the limit", err)
	}
	if len(e.cloud.putLog()) != 0 || e.cloud.count("abort") != 1 {
		t.Errorf("calls = %v, parts = %d, want an abort and no parts", e.cloud.callLog(), len(e.cloud.putLog()))
	}
}

func TestKeyPairNotReadyRetries(t *testing.T) {
	var (
		mu     sync.Mutex
		pauses []time.Duration
	)
	record := func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		pauses = append(pauses, d)
		return ctx.Err()
	}

	t.Run("ready after two tries", func(t *testing.T) {
		e := newTestEnv(t)
		file := singleFile(t, e, "weights.bin", patterned(64, 1))
		vol := e.cloud.newVolume("model-weights", "")
		e.cloud.notReady = 2
		opts := e.options(vol, file)
		opts.sleep = record

		if _, err := Run(t.Context(), opts); err != nil {
			t.Fatal(err)
		}

		mu.Lock()
		defer mu.Unlock()
		if want := []time.Duration{baseBackoff, 2 * baseBackoff}; !slices.Equal(pauses, want) {
			t.Errorf("pauses = %v, want %v", pauses, want)
		}
		if got := e.cloud.count("keypair"); got != 3 {
			t.Errorf("the key pair was read %d times, want 3", got)
		}
	})

	t.Run("never ready", func(t *testing.T) {
		e := newTestEnv(t)
		file := singleFile(t, e, "weights.bin", patterned(64, 1))
		vol := e.cloud.newVolume("model-weights", "")
		e.cloud.notReady = 100

		_, err := e.put(vol, file)

		if !errors.Is(err, ErrKeyPairNotReady) {
			t.Fatalf("error = %v, want the key pair not ready", err)
		}
		if got := e.cloud.count("keypair"); got != keyPairAttempts {
			t.Errorf("the key pair was read %d times, want %d", got, keyPairAttempts)
		}
		if e.cloud.count("open") != 0 || e.hashedCount() != 0 {
			t.Errorf("calls = %v, hashed %d: nothing should start without a key pair", e.cloud.callLog(), e.hashedCount())
		}
	})
}

// TestVolumeNotFoundStopsEarly: a missing Volume is reported before any file is hashed.
func TestVolumeNotFoundStopsEarly(t *testing.T) {
	e := newTestEnv(t)
	dir := standardTree(t, e)

	_, err := e.put(VolumeRef{Name: "missing"}, dir)

	p, ok := errors.AsType[*ProblemError](err)
	if !ok || p.Status != http.StatusNotFound {
		t.Fatalf("error = %v, want a 404 problem", err)
	}
	if e.hashedCount() != 0 || e.cloud.count("keypair") != 0 || e.cloud.count("open") != 0 {
		t.Errorf("calls = %v, hashed %d, want the put to stop at the Volume", e.cloud.callLog(), e.hashedCount())
	}
}

func TestPutNothingToUpload(t *testing.T) {
	e := newTestEnv(t)
	empty := filepath.Join(e.srcDir, "empty")
	if err := os.MkdirAll(filepath.Join(empty, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	vol := e.cloud.newVolume("model-weights", "")

	_, err := e.put(vol, empty)

	if !errors.Is(err, ErrNoFiles) {
		t.Fatalf("error = %v, want no files", err)
	}
	if calls := e.cloud.callLog(); len(calls) != 0 {
		t.Errorf("the API was called: %v", calls)
	}
}

func TestPutReportsProgress(t *testing.T) {
	e := newTestEnv(t)
	dir := standardTree(t, e)
	e.mustPut(e.cloud.newVolume("model-weights", ""), dir)

	got := e.progress.String()
	for _, want := range []string{"hashing: 4/4 files", "uploading: 4/4 files", "(100%)"} {
		if !strings.Contains(got, want) {
			t.Errorf("progress lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\r") {
		t.Error("progress to a writer that is not a terminal rewrites a line")
	}
}

// journalAt returns a hook that reads the resume state at the first call.
func journalAt(t *testing.T, e *testEnv, snap *journalSnapshot) func() {
	t.Helper()
	var once sync.Once
	return func() { once.Do(func() { *snap = readJournal(t, e.configDir) }) }
}

func decodeStd(t *testing.T, v any) []byte {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("%v is not a base64 string", v)
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestJournalSavedBeforeDeclare: the session key and each file's keys are saved before the declaration.
func TestJournalSavedBeforeDeclare(t *testing.T) {
	e := newTestEnv(t)
	dir := standardTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	var (
		snap     journalSnapshot
		declared FileDeclarations
	)
	capture := journalAt(t, e, &snap)
	e.cloud.onDeclare = func(batch FileDeclarations) {
		declared = batch
		capture()
	}

	e.mustPut(vol, dir)

	sessions := snap.ofType(recordSession)
	if len(sessions) != 1 {
		t.Fatalf("%d session records before the declaration, want 1", len(sessions))
	}
	if sessions[0]["version"] != float64(1) {
		t.Error("the journal does not start with format version 1")
	}
	material := e.keys.keyMaterial()
	if got := decodeStd(t, sessions[0]["sessionKey"]); !bytes.Equal(got, material[0]) {
		t.Error("the saved session key is not the session's sealing key")
	}
	files := snap.ofType(recordFile)
	if len(files) != len(declared.Files) || len(files) == 0 {
		t.Fatalf("%d file records before the declaration of %d files", len(files), len(declared.Files))
	}
	for i, rec := range files {
		d := declared.Files[i]
		sealed, err := base64.RawURLEncoding.DecodeString(d.SealedKey)
		if err != nil {
			t.Fatal(err)
		}
		keys := snap.Keys[i]
		if rec["path"] != d.Path || !bytes.Equal(keys.SealedKey, sealed) || keys.Attempt != d.AttemptID {
			t.Errorf("file record %d is not the declaration of %s", i, d.Path)
		}
		if len(keys.DataKey) != 32 {
			t.Errorf("file record %d holds no 32-byte data key", i)
		}
		if rec["dataKey"] != nil || rec["sealedKey"] != nil || rec["attemptId"] != nil {
			t.Errorf("file record %d holds keys in the append-only journal", i)
		}
	}
}

// TestJournalPartDigestBeforeSeal: a part's plaintext digest is saved before the part is sealed.
func TestJournalPartDigestBeforeSeal(t *testing.T) {
	e := newTestEnv(t)
	content := patterned(5000, 1)
	file := singleFile(t, e, "weights.bin", content)
	vol := e.cloud.newVolume("model-weights", "")
	var snap journalSnapshot
	capture := journalAt(t, e, &snap)
	e.cloud.onPut = func(partRequest) { capture() }

	e.mustPut(vol, file)

	parts := snap.ofType(recordPart)
	if len(parts) != 1 {
		t.Fatalf("%d part records when the first part was sent, want 1", len(parts))
	}
	if parts[0]["sha256"] != sha256Hex(content) || parts[0]["length"] != float64(len(content)) || parts[0]["part"] != float64(1) {
		t.Errorf("part record = %v, want the digest and length of the plaintext", parts[0])
	}
}

// TestJournalHoldsAcks: a completed file's layout, acks and CRCs are in the resume state.
func TestJournalHoldsAcks(t *testing.T) {
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", patterned(5000, 1))
	vol := e.cloud.newVolume("model-weights", "")
	var snap journalSnapshot
	capture := journalAt(t, e, &snap)
	e.cloud.onCompleteFile = func(string) { capture() }

	e.mustPut(vol, file)

	acks := snap.ofType(recordAck)
	if len(acks) != 1 || len(e.cloud.acks) != 1 || acks[0]["ack"] != e.cloud.acks[0] {
		t.Fatalf("ack records = %v, the Worker issued %v", acks, e.cloud.acks)
	}
	if crc, _ := acks[0]["crc64nvme"].(string); len(crc) != 16 || acks[0]["length"] != float64(ciphertextSize(t, 5000)) {
		t.Errorf("ack record = %v", acks[0])
	}
	declared := snap.ofType(recordDeclared)
	if len(declared) != 1 || declared[0]["state"] != stateUploading || declared[0]["layout"] == nil {
		t.Errorf("declared records = %v, want one uploading file with its layout", declared)
	}
}

func TestJournalFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no permission bits")
	}
	e := newTestEnv(t)
	file := singleFile(t, e, "weights.bin", patterned(5000, 1))
	vol := e.cloud.newVolume("model-weights", "")
	var snap journalSnapshot
	capture := journalAt(t, e, &snap)
	e.cloud.onDeclare = func(FileDeclarations) { capture() }

	e.mustPut(vol, file)

	if snap.DirMode != 0o700 || snap.Mode != 0o600 {
		t.Errorf("resume state modes: directory %o, file %o, want 700 and 600", snap.DirMode, snap.Mode)
	}
}
