package serverless

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/runware/runware-cli/internal/api/transport"
)

const (
	testLogsEntriesPath  = "/v1/logs/queries/runtime/entries"
	testLogsTailPath     = "/v1/logs/queries/runtime_tail/tail"
	testLogBodyReady     = "ready"
	testLogsProblemJSON  = "application/problem+json"
	testLogsEventStream  = "text/event-stream"
	testLogsWindow       = "1h"
	testLogsUnknownAppID = "no-such-app"
)

func TestGetLogEntries_SendsSelectorsAndDefaultLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != testLogsEntriesPath {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("window") != testLogsWindow || q.Get("limit") != "20" || q.Get("deployment") != testAppID {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		if q.Has("cursor") || q.Has("endpoint") {
			t.Errorf("unset selectors must be absent, query = %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"entries":[{"time":1750000000,"level":"info","body":%q,"fields":{"k":"v"}}]}`, testLogBodyReady)
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	deployment := testAppID
	page, err := c.GetLogEntries(context.Background(), LogQueryRuntime, GetLogEntriesParams{
		Window:     LogWindow(testLogsWindow),
		Deployment: &deployment,
	})
	if err != nil {
		t.Fatalf("GetLogEntries: %v", err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Body != testLogBodyReady || page.Entries[0].Time != 1750000000 {
		t.Fatalf("entries = %#v", page.Entries)
	}
	if page.Entries[0].Level == nil || *page.Entries[0].Level != "info" {
		t.Errorf("level = %v", page.Entries[0].Level)
	}
	if page.NextCursor != nil {
		t.Errorf("nextCursor must be absent on the last page, got %q", *page.NextCursor)
	}
}

func TestGetLogEntries_PassesLimitAndCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("limit") != "5" || q.Get("cursor") != testCursorPage2 {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"entries":[],"nextCursor":%q}`, testCursorPage3)
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	limit := Limit(5)
	cursor := testCursorPage2
	page, err := c.GetLogEntries(context.Background(), LogQueryRuntime, GetLogEntriesParams{
		Window: LogWindow(testLogsWindow),
		Limit:  &limit,
		Cursor: &cursor,
	})
	if err != nil {
		t.Fatalf("GetLogEntries: %v", err)
	}
	if page.Entries == nil || len(page.Entries) != 0 {
		t.Errorf("entries = %#v, want an empty non-nil slice", page.Entries)
	}
	if page.NextCursor == nil || *page.NextCursor != testCursorPage3 {
		t.Errorf("nextCursor = %v", page.NextCursor)
	}
}

func TestGetLogEntries_NoAPIKey(t *testing.T) {
	c := newClient("", "http://unused", slog.Default(), http.DefaultClient)
	_, err := c.GetLogEntries(context.Background(), LogQueryRuntime, GetLogEntriesParams{Window: LogWindow(testLogsWindow)})
	if !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestGetLogEntries_UnknownAppIsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", testLogsProblemJSON)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"https://docs.runware.ai/serverless/errors#not-found","title":"Not Found","status":404,"detail":"app no-such-app not found"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	deployment := testLogsUnknownAppID
	_, err := c.GetLogEntries(context.Background(), LogQueryRuntime, GetLogEntriesParams{
		Window:     LogWindow(testLogsWindow),
		Deployment: &deployment,
	})
	re, ok := errors.AsType[*transport.RunwareError](err)
	if !ok {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeNotFound || re.Message != "app no-such-app not found" {
		t.Errorf("code=%q message=%q", re.Code, re.Message)
	}
}

func TestGetLogEntries_ValidationErrorNamesTheParameter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", testLogsProblemJSON)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"type":"https://docs.runware.ai/serverless/errors#validation-error","title":"Unprocessable Entity","status":422,"detail":"request failed validation","errors":[{"pointer":"/window","detail":"this query does not answer window 30d"}]}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.GetLogEntries(context.Background(), LogQueryRuntime, GetLogEntriesParams{Window: LogWindow("30d")})
	if err == nil || !strings.Contains(err.Error(), "/window: this query does not answer window 30d") {
		t.Fatalf("err = %v", err)
	}
}

func serveSSE(t *testing.T, frames ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != testLogsTailPath {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("deployment") != testAppID {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", testLogsEventStream+"; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()
		for _, frame := range frames {
			_, _ = w.Write([]byte(frame))
			flusher.Flush()
		}
	}))
}

func TestTailLogs_EmitsEntriesUntilEnd(t *testing.T) {
	srv := serveSSE(t,
		": keepalive\n\n",
		"data: {\"time\":1750000000,\"body\":\"ready\"}\n\n",
		"data: {\"time\":1750000001,\"level\":\"warn\",\"body\":\"slow\"}\n\n",
		"event: end\ndata: \n\n",
	)
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	var got []LogEntry
	err := c.TailLogs(context.Background(), LogQueryRuntimeTail, testAppID, func(e LogEntry) error {
		got = append(got, e)
		return nil
	})
	if !errors.Is(err, ErrTailEnded) {
		t.Fatalf("err = %v, want ErrTailEnded", err)
	}
	if len(got) != 2 || got[0].Body != testLogBodyReady || got[1].Body != "slow" || got[1].Level == nil || *got[1].Level != "warn" {
		t.Fatalf("entries = %#v", got)
	}
}

func TestTailLogs_ErrorEventCarriesDetail(t *testing.T) {
	srv := serveSSE(t, "event: error\ndata: the log stream became unavailable\n\n")
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	err := c.TailLogs(context.Background(), LogQueryRuntimeTail, testAppID, func(LogEntry) error { return nil })
	se, ok := errors.AsType[*TailStreamError](err)
	if !ok || se.Detail != "the log stream became unavailable" {
		t.Fatalf("err = %#v", err)
	}
}

func TestTailLogs_ConnectionCloseWithoutEndCountsAsEnded(t *testing.T) {
	srv := serveSSE(t, "data: {\"time\":1,\"body\":\"x\"}\n\n")
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	err := c.TailLogs(context.Background(), LogQueryRuntimeTail, testAppID, func(LogEntry) error { return nil })
	if !errors.Is(err, ErrTailEnded) {
		t.Fatalf("err = %v", err)
	}
}

func TestTailLogs_CancelReturnsContextError(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", testLogsEventStream)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	// The cancel lands after the client's whole-request timeout would have
	// fired, so a tail that kept the timeout fails this test with a deadline error.
	c := newClient("test-key", srv.URL, slog.Default(), &http.Client{Timeout: 100 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(400 * time.Millisecond)
		cancel()
	}()
	err := c.TailLogs(ctx, LogQueryRuntimeTail, testAppID, func(LogEntry) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestTailLogs_NonStreamStatusIsAProblem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", testLogsProblemJSON)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"type":"https://docs.runware.ai/serverless/errors#validation-error","title":"Unprocessable Entity","status":422,"detail":"request failed validation","errors":[{"pointer":"/queryId","detail":"this query does not support live tail"}]}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	err := c.TailLogs(context.Background(), LogQueryRuntime, testAppID, func(LogEntry) error { return nil })
	re, ok := errors.AsType[*transport.RunwareError](err)
	if !ok || re.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(re.Message, "/queryId: this query does not support live tail") {
		t.Fatalf("err = %#v", err)
	}
}

func TestTailLogs_RejectsNonEventStreamBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	err := c.TailLogs(context.Background(), LogQueryRuntimeTail, testAppID, func(LogEntry) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "unexpected content type") {
		t.Fatalf("err = %v", err)
	}
}
