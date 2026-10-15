package transport

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPTransport_BodyStallIsConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	tr, err := DialHTTP(context.Background(), "key", srv.URL, slog.Default(), WithTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	_, err = tr.Send(context.Background(), []any{map[string]string{"taskType": "ping"}})

	if err == nil {
		t.Fatal("expected a timeout reading the body, got nil")
	}
	if !IsConnectionError(err) {
		t.Errorf("IsConnectionError(%v) = false, want true", err)
	}
}
