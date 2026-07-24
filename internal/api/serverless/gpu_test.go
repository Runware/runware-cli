package serverless

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runware/runware-cli/internal/api/transport"
)

func TestListGpuTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/gpu-types" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("missing/incorrect auth header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"h100","name":"H100","memory":"80GB","availability":"available","pricing":{"currency":"USD","perSecond":"0.000767"}}]}`))
	}))
	defer srv.Close()

	c := NewClient("test-key", srv.URL, slog.Default())
	gpus, err := c.ListGpuTypes(context.Background())
	if err != nil {
		t.Fatalf("ListGpuTypes: %v", err)
	}
	if len(gpus) != 1 {
		t.Fatalf("expected 1 gpu, got %d", len(gpus))
	}
	if gpus[0].ID != "h100" || gpus[0].Pricing.PerSecond != "0.000767" {
		t.Errorf("unexpected gpu: %+v", gpus[0])
	}
	if gpus[0].Memory == nil || *gpus[0].Memory != "80GB" {
		t.Errorf("unexpected memory: %v", gpus[0].Memory)
	}
}

func TestListGpuTypes_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.ListGpuTypes(context.Background()); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestListGpuTypes_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":401,"message":"Missing or invalid API key"}`))
	}))
	defer srv.Close()

	c := NewClient("bad", srv.URL, slog.Default())
	_, err := c.ListGpuTypes(context.Background())
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeAuth {
		t.Errorf("expected CodeAuth, got %v", re.Code)
	}
}
