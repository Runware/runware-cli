package transport

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPTransport_BrokenResponseBodyIsConnectionError(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "body stalls until timeout",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			},
		},
		{
			name: "connection closes before body is complete",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "100")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"data":`)) //nolint:errcheck,gosec
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			tr, err := DialHTTP(context.Background(), "key", srv.URL, slog.Default(), WithTimeout(50*time.Millisecond))
			if err != nil {
				t.Fatalf("dial: %v", err)
			}

			_, err = tr.Send(context.Background(), []any{map[string]string{"taskType": "ping"}})

			if err == nil {
				t.Fatal("expected an error reading the body, got nil")
			}
			if !IsConnectionError(err) {
				t.Errorf("IsConnectionError(%v) = false, want true", err)
			}
		})
	}
}
