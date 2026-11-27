package serverless

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// repeatedCursorServer answers every request with body, whose nextCursor
// never changes, so the second page repeats the first page's cursor.
func repeatedCursorServer(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return newClient("test-key", srv.URL, slog.Default(), srv.Client())
}

func TestAllVolumesReturnsNothingWhenPagingFails(t *testing.T) {
	c := repeatedCursorServer(t, `{"data":[{"id":"6a1f0d6e-5c1b-4f69-9a39-0f4c1c8b2d11","name":"model-weights","environment":"production","status":"live","logicalBytes":0,"fileCount":0,"createdAt":"2026-11-02T10:00:00Z","updatedAt":"2026-11-02T10:00:00Z"}],"nextCursor":"c1"}`)

	volumes, err := c.AllVolumes(context.Background())

	if !errors.Is(err, errRepeatedVolumeCursor) {
		t.Fatalf("err = %v, want errRepeatedVolumeCursor", err)
	}
	if volumes != nil {
		t.Fatalf("volumes = %v, want none: the pages read before the error are incomplete", volumes)
	}
}

func TestAllVolumeReferencesReturnsNothingWhenPagingFails(t *testing.T) {
	c := repeatedCursorServer(t, `{"blocking":[{"appId":"img-gen","appVersionNumber":7,"kind":"current-app-version"}],"rollbackTargets":[],"nextCursor":"r1"}`)

	refs, err := c.AllVolumeReferences(context.Background(), "model-weights")

	if !errors.Is(err, errRepeatedVolumeCursor) {
		t.Fatalf("err = %v, want errRepeatedVolumeCursor", err)
	}
	if refs != nil {
		t.Fatalf("refs = %+v, want none: the pages read before the error are incomplete", refs)
	}
}
