package put

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type blockedPart struct {
	Transport
	started chan struct{}
	release chan struct{}
}

func (b blockedPart) PutPart(ctx context.Context, target Target, body io.Reader, size int64) (Secret, error) {
	close(b.started)
	select {
	case <-b.release:
		return b.Transport.PutPart(ctx, target, body, size)
	case <-ctx.Done():
		return "", context.Cause(ctx)
	}
}

func TestConcurrentPutLeavesActiveSessionAlone(t *testing.T) {
	e := newTestEnv(t)
	vol := e.cloud.newVolume("weights", "")
	first := singleFile(t, e, "first.txt", []byte("first"))
	second := singleFile(t, e, "second.txt", []byte("second"))
	opts := e.options(vol, first)
	block := blockedPart{
		Transport: opts.Transport,
		started:   make(chan struct{}),
		release:   make(chan struct{}),
	}
	opts.Transport = block
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := Run(ctx, opts)
		result <- err
	}()
	select {
	case <-block.started:
	case err := <-result:
		t.Fatalf("first put stopped before transfer: %v", err)
	}
	_, err := Run(t.Context(), e.options(vol, first, second))
	close(block.release)
	if err != nil {
		t.Errorf("concurrent put: %v", err)
	}
	if err := <-result; err != nil {
		t.Errorf("original put: %v", err)
	}
	if files, _ := e.cloud.latest(vol); len(files) != 2 || e.cloud.count("abort") != 0 || e.cloud.count("open") != 2 {
		t.Fatalf("files = %v, calls = %v, want both puts published", files, e.cloud.callLog())
	}
}

func TestResumeRecoversVerifiedFileFromOlderSnapshot(t *testing.T) {
	for _, when := range []string{"before declaration answer", "before part acknowledgement"} {
		t.Run(when, func(t *testing.T) {
			e := newTestEnv(t)
			vol := e.cloud.newVolume("weights", "")
			first := singleFile(t, e, "first.txt", []byte("first"))
			second := singleFile(t, e, "second.txt", []byte("second"))
			snapshot := filepath.Join(t.TempDir(), "snapshot")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if when == "before declaration answer" {
				e.cloud.onDeclare = func(FileDeclarations) { copyTree(t, e.stateDir(), snapshot) }
			}
			e.cloud.onPut = func(req partRequest) {
				if req.Path == "first.txt" && when == "before part acknowledgement" {
					copyTree(t, e.stateDir(), snapshot)
				}
				if req.Path == "second.txt" {
					cancel()
				}
			}
			opts := e.options(vol, first, second)
			opts.Parallelism = 1
			if _, err := Run(ctx, opts); !errors.Is(err, context.Canceled) {
				t.Fatalf("first put = %v, want cancellation", err)
			}
			e.cloud.onDeclare = nil
			e.cloud.onPut = nil
			restoreTree(t, snapshot, e.stateDir())

			if _, err := Run(t.Context(), opts); err != nil {
				t.Fatal(err)
			}
			if e.cloud.count("open") != 1 || okPuts(e, "first.txt", 1) != 1 {
				t.Fatalf("calls = %v, want one session without resending the verified file", e.cloud.callLog())
			}
		})
	}
}

func TestIndependentPutLeavesSavedSessionAvailable(t *testing.T) {
	e := newTestEnv(t)
	vol := e.cloud.newVolume("weights", "")
	first := filepath.Join(t.TempDir(), "model.bin")
	second := filepath.Join(t.TempDir(), "model.bin")
	writeFile(t, first, []byte("first"))
	writeFile(t, second, []byte("second"))

	opts := e.options(vol, first)
	block := blockedPart{
		Transport: opts.Transport,
		started:   make(chan struct{}),
		release:   make(chan struct{}),
	}
	opts.Transport = block
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := Run(ctx, opts)
		result <- err
	}()
	select {
	case <-block.started:
	case err := <-result:
		t.Fatalf("first put stopped before transfer: %v", err)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("first put = %v, want cancellation", err)
	}

	opts = e.options(vol, second)
	block = blockedPart{
		Transport: opts.Transport,
		started:   make(chan struct{}),
		release:   make(chan struct{}),
	}
	opts.Transport = block
	go func() {
		_, err := Run(t.Context(), opts)
		result <- err
	}()
	select {
	case <-block.started:
	case err := <-result:
		t.Fatalf("independent put stopped before transfer: %v", err)
	}
	_, err := Run(t.Context(), e.options(vol, first))
	close(block.release)
	if err != nil {
		t.Errorf("resume: %v", err)
	}
	if err := <-result; err != nil {
		t.Errorf("independent put: %v", err)
	}
	if e.cloud.count("open") != 2 || e.cloud.count("abort") != 0 || len(e.uploadDirs()) != 0 {
		t.Fatalf("calls = %v, journals = %v, want two sessions without aborts", e.cloud.callLog(), e.uploadDirs())
	}
}

func TestIndependentPutPreservesOverlappingLegacySession(t *testing.T) {
	e := newTestEnv(t)
	vol := e.cloud.newVolume("weights", "")
	first := singleFile(t, e, "first.txt", []byte("first"))
	second := singleFile(t, e, "second.txt", []byte("second"))
	opts := e.options(vol, first, second)
	saveLegacyDeclarations(t, opts)

	if _, err := Run(t.Context(), e.options(vol, first)); err != nil {
		t.Fatalf("independent put: %v", err)
	}
	if e.cloud.count("open") != 2 || e.cloud.count("abort") != 0 || len(e.uploadDirs()) != 1 {
		t.Fatalf("calls = %v, journals = %v, want the legacy journal kept", e.cloud.callLog(), e.uploadDirs())
	}
	if _, err := Run(t.Context(), opts); err != nil {
		t.Fatalf("legacy resume: %v", err)
	}
	if e.cloud.count("open") != 2 || e.cloud.count("abort") != 0 || len(e.uploadDirs()) != 0 {
		t.Fatalf("calls = %v, journals = %v, want the original session resumed", e.cloud.callLog(), e.uploadDirs())
	}
}

func TestResumeRefusesFreshPutAfterUnconfirmedPublication(t *testing.T) {
	e := newTestEnv(t)
	vol := e.cloud.newVolume("weights", "")
	singleFile(t, e, "model.bin", []byte("original"))
	opts := e.options(vol, e.srcDir)
	opts.API = &lostCompletionResponse{API: e.cloud}
	if _, err := Run(t.Context(), opts); !errors.Is(err, errUploadRetriesExhausted) {
		t.Fatalf("first put = %v, want publication retries exhausted", err)
	}
	extra := singleFile(t, e, "extra.bin", []byte("additional"))
	opts.API = e.cloud

	_, err := Run(t.Context(), opts)
	if err == nil || !strings.Contains(err.Error(), "may have published") ||
		!strings.Contains(err.Error(), "paths differ") {
		t.Fatalf("resume = %v, want the path mismatch and publication uncertainty", err)
	}
	if files, _ := e.cloud.latest(vol); len(files) != 1 || e.cloud.count("open") != 1 || len(e.uploadDirs()) != 1 {
		t.Fatalf("files = %v, calls = %v, journals = %v, want only the original publication and its saved state",
			files, e.cloud.callLog(), e.uploadDirs())
	}

	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	sum, err := Run(t.Context(), opts)
	if err != nil || !sum.recovered || sum.BytesSent != 0 || e.cloud.count("open") != 1 || len(e.uploadDirs()) != 0 {
		t.Fatalf("recovery = %+v, error = %v, calls = %v, want the original result without retransmitting", sum, err, e.cloud.callLog())
	}
}

func TestPutKeepsSavedSessionsScopedToAPI(t *testing.T) {
	first := newTestEnv(t)
	second := newTestEnv(t)
	second.configDir = first.configDir
	file := singleFile(t, first, "model.bin", []byte("original"))
	one := first.options(first.cloud.newVolume("weights", ""), file)
	one.APIBaseURL = "https://first.example.test"
	one.API = &lostCompletionResponse{API: first.cloud}
	if _, err := Run(t.Context(), one); !errors.Is(err, errUploadRetriesExhausted) {
		t.Fatalf("first put = %v, want publication retries exhausted", err)
	}
	two := second.options(second.cloud.newVolume("weights", ""), file)
	two.APIBaseURL = "https://second.example.test"
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(t.Context(), two); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("put without a source = %v, want a missing file error", err)
	}
	if calls := second.cloud.callLog(); len(calls) != 0 || len(first.uploadDirs()) != 1 {
		t.Fatalf("calls = %v, journals = %v, want no requests and the other endpoint's journal preserved", calls, first.uploadDirs())
	}
	writeFile(t, file, []byte("original"))
	if _, err := Run(t.Context(), two); err != nil {
		t.Fatalf("put against second endpoint: %v", err)
	}
	if second.cloud.count("open") != 1 || second.cloud.count("abort") != 0 || len(first.uploadDirs()) != 1 {
		t.Fatalf("calls = %v, journals = %v, want an independent upload", second.cloud.callLog(), first.uploadDirs())
	}
	one.API = first.cloud
	one.APIBaseURL = "https://FIRST.example.test:443/"
	sum, err := Run(t.Context(), one)
	if err != nil || !sum.recovered || first.cloud.count("open") != 1 || len(first.uploadDirs()) != 0 {
		t.Fatalf("recovery = %+v, error = %v, calls = %v, want the saved result",
			sum, err, first.cloud.callLog())
	}
}

func TestOnlyDefaultAPIResumesJournalWithoutAnEndpoint(t *testing.T) {
	e := newTestEnv(t)
	vol := e.cloud.newVolume("weights", "")
	file := singleFile(t, e, "model.bin", []byte("original"))
	opts := e.options(vol, file)
	saveLegacyDeclarations(t, opts)

	override := opts
	override.APIBaseURL = "https://another.example.test"
	if _, err := Run(t.Context(), override); err != nil {
		t.Fatalf("put against overridden endpoint: %v", err)
	}
	if e.cloud.count("open") != 2 || e.cloud.count("abort") != 0 || len(e.uploadDirs()) != 1 {
		t.Fatalf("calls = %v, journals = %v, want a fresh upload and the legacy journal preserved", e.cloud.callLog(), e.uploadDirs())
	}
	if _, err := Run(t.Context(), opts); err != nil {
		t.Fatalf("legacy resume: %v", err)
	}
	if e.cloud.count("open") != 2 || e.cloud.count("abort") != 0 || len(e.uploadDirs()) != 0 {
		t.Fatalf("calls = %v, journals = %v, want the legacy upload completed", e.cloud.callLog(), e.uploadDirs())
	}
}

func TestResumeRefusesChangeAfterUnappliedPublication(t *testing.T) {
	e := newTestEnv(t)
	vol := e.cloud.newVolume("weights", "")
	file := singleFile(t, e, "model.bin", []byte("original"))
	opts := e.options(vol, file)
	opts.API = &unavailableCompletion{API: e.cloud}
	if _, err := Run(t.Context(), opts); !errors.Is(err, errUploadRetriesExhausted) {
		t.Fatalf("first put = %v, want publication retries exhausted", err)
	}
	rewrite(t, file, []byte("changed"))
	if _, err := Run(t.Context(), e.options(vol, file)); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("resume = %v, want a source-change error", err)
	}
	if files, _ := e.cloud.latest(vol); len(files) != 0 || e.cloud.count("open") != 1 || e.cloud.count("abort") != 1 {
		t.Fatalf("files = %v, calls = %v, want one aborted session and no publication", files, e.cloud.callLog())
	}
}

func TestResumeReportsUncertainPublicationIfSourceDisappearsDuringHash(t *testing.T) {
	e := newTestEnv(t)
	vol := e.cloud.newVolume("weights", "")
	file := singleFile(t, e, "model.bin", []byte("original"))
	opts := e.options(vol, file)
	opts.API = &lostCompletionResponse{API: e.cloud}
	if _, err := Run(t.Context(), opts); !errors.Is(err, errUploadRetriesExhausted) {
		t.Fatalf("first put = %v, want publication retries exhausted", err)
	}
	if err := os.Remove(filepath.Join(e.stateDir(), hashCacheFile)); err != nil {
		t.Fatal(err)
	}
	opts.API = e.cloud
	opts.onHash = func(path string) {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Run(t.Context(), opts)
	if err == nil || !strings.Contains(err.Error(), "may have published") || !strings.Contains(err.Error(), file) {
		t.Fatalf("resume = %v, want publication uncertainty naming %s", err, file)
	}
	if files, _ := e.cloud.latest(vol); len(files) != 1 || e.cloud.count("open") != 1 || len(e.uploadDirs()) != 1 {
		t.Fatalf("files = %v, calls = %v, journals = %v, want the original publication only", files, e.cloud.callLog(), e.uploadDirs())
	}
}
