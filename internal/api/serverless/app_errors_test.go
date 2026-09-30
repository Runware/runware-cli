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

func TestListAppErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/apps/"+testAppID+"/errors" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("window") != "1h" || q.Get("statusClass") != "5xx" || q.Get("limit") != "50" || q.Get("cursor") != testCursorPage2 {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"time":1750000000,"level":"error","body":"request failed"}],"nextCursor":"page-2"}`))
	}))
	defer srv.Close()

	window := ListAppErrorsParamsWindow("1h")
	status := ListAppErrorsParamsStatusClass("5xx")
	limit := Limit(50)
	cursor := Cursor(testCursorPage2)
	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListAppErrors(context.Background(), testAppID, &ListAppErrorsParams{
		Window:      &window,
		StatusClass: &status,
		Limit:       &limit,
		Cursor:      &cursor,
	})
	if err != nil {
		t.Fatalf("ListAppErrors: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].Body != "request failed" || page.Data[0].Time != 1750000000 {
		t.Fatalf("data = %#v", page.Data)
	}
	if page.NextCursor == nil || *page.NextCursor != testCursorPage2 {
		t.Errorf("nextCursor = %v", page.NextCursor)
	}
}

func TestListAppErrors_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.ListAppErrors(context.Background(), testAppID, nil); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}
