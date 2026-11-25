package put

import (
	"context"
	"errors"
	"io"
	"path/filepath"
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
