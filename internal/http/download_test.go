package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownload_Success(t *testing.T) {
	content := "video content bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(content)) //nolint:errcheck,gosec
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.mp4")
	if err := Download(context.Background(), srv.URL, dest, 5*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}
	if string(got) != content {
		t.Errorf("file content = %q, want %q", got, content)
	}
}

func TestDownload_BadStatusCode(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusForbidden} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer srv.Close()

			dest := filepath.Join(t.TempDir(), "out.mp4")
			err := Download(context.Background(), srv.URL, dest, 5*time.Second)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "status") {
				t.Errorf("error %q does not mention status", err)
			}
		})
	}
}

func TestDownload_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client gives up.
		<-r.Context().Done()
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.mp4")
	err := Download(context.Background(), srv.URL, dest, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestDownload_ContextAlreadyCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	dest := filepath.Join(t.TempDir(), "out.mp4")
	err := Download(ctx, srv.URL, dest, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestDownload_FailToCreateFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data")) //nolint:errcheck,gosec
	}))
	defer srv.Close()

	// Place a regular file where MkdirAll would need to create a directory,
	// making the destination path unwritable on any platform.
	blocker := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(blocker, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(blocker, "out.mp4")
	err := Download(context.Background(), srv.URL, dest, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for unwritable path, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create destination directory") {
		t.Errorf("error %q does not mention destination directory failure", err)
	}
}
