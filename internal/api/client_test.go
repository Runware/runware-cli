package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/api/transport"
)

// dialHTTP is a test helper that dials an HTTP transport and fails the test on error.
func dialHTTP(t *testing.T, apiKey, url string) *transport.HTTPTransport {
	t.Helper()
	tr, err := transport.DialHTTP(context.Background(), apiKey, url, slog.Default())
	if err != nil {
		t.Fatalf("DialHTTP: %v", err)
	}
	return tr
}

// TestSend_Non2xx_InvalidJSON: non-200 with a non-JSON body returns a parse error
// that includes the HTTP status code.
func TestSend_Non2xx_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<html>internal error</html>`))
	}))
	defer srv.Close()

	c := NewClient(dialHTTP(t, "test-key", srv.URL), slog.Default())
	_, err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error from 5xx response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention status 500, got: %v", err)
	}
}

// TestSend_Non2xx_ValidJSON_NoErrors: non-200 with valid JSON that has no errors
// field — the previous blind spot. Must return an HTTP status error.
func TestSend_Non2xx_ValidJSON_NoErrors(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"data": []any{}})
	for _, code := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				_, _ = w.Write(body)
			}))
			defer srv.Close()

			c := NewClient(dialHTTP(t, "test-key", srv.URL), slog.Default())
			_, err := c.Ping(context.Background())
			if err == nil {
				t.Fatalf("expected error for HTTP %d, got nil", code)
			}
			codeStr := http.StatusText(code)
			if !strings.Contains(err.Error(), "HTTP") {
				t.Errorf("expected HTTP status error for %s, got: %v", codeStr, err)
			}
		})
	}
}

// TestSend_Non2xx_ValidJSON_WithErrors: non-200 with a structured errors field —
// errors field checked first, so structured API error surfaces over HTTP status.
func TestSend_Non2xx_ValidJSON_WithErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"code":"badInput","message":"bad input"}]}`))
	}))
	defer srv.Close()

	c := NewClient(dialHTTP(t, "test-key", srv.URL), slog.Default())
	_, err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Errors-first: structured message surfaces, not the HTTP status.
	if !strings.Contains(err.Error(), "bad input") {
		t.Errorf("expected structured API error message (errors first), got: %v", err)
	}
}

// TestSend_200_ValidJSON: happy path returns no error.
func TestSend_200_ValidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"taskType":"ping","taskUUID":"abc"}]}`))
	}))
	defer srv.Close()

	c := NewClient(dialHTTP(t, "test-key", srv.URL), slog.Default())
	_, err := c.Ping(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on 200 response: %v", err)
	}
}

// TestSend_200_WithErrors: 200 response that carries an errors field should still
// surface the API error.
func TestSend_200_WithErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":[{"code":"someError","message":"something went wrong"}]}`))
	}))
	defer srv.Close()

	c := NewClient(dialHTTP(t, "test-key", srv.URL), slog.Default())
	_, err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error from errors field, got nil")
	}
	if !strings.Contains(err.Error(), "something went wrong") {
		t.Errorf("expected structured API error message, got: %v", err)
	}
}

// TestSend_UnauthorizedOn401: 401 with invalidApiKey in errors field is reported
// as an auth error by transport.IsAuthError.
func TestSend_UnauthorizedOn401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"code":"invalidApiKey","message":"invalid key"}]}`))
	}))
	defer srv.Close()

	c := NewClient(dialHTTP(t, "test-key", srv.URL), slog.Default())
	_, err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !transport.IsAuthError(err) {
		t.Errorf("expected IsAuthError true for invalidApiKey, got: %v", err)
	}
}

// TestSend_NoAPIKey: empty API key returns ErrNoAPIKey without making a request.
func TestSend_NoAPIKey(t *testing.T) {
	c := NewClient(dialHTTP(t, "", "http://localhost"), slog.Default())
	_, err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error for empty API key, got nil")
	}
	if !transport.IsAuthError(err) {
		t.Errorf("expected IsAuthError true for empty key, got: %v", err)
	}
}

// ---- PollDynamic tests ----
// These tests reuse mockTransport defined in poll_test.go (same package).

// successItem builds a raw JSON object with status "success" and an arbitrary field.
func successItem(t *testing.T, extra map[string]any) json.RawMessage {
	t.Helper()
	m := map[string]any{fieldStatus: "success"}
	for k, v := range extra {
		m[k] = v
	}
	return rawJSON(t, m)
}

// processingItem builds a raw JSON object with status "processing" and a progress value.
func processingItem(t *testing.T, progress int) json.RawMessage {
	t.Helper()
	return rawJSON(t, map[string]any{fieldStatus: "processing", "progress": progress})
}

// errorItem builds a raw JSON object with status "error" and failure details.
func errorItem(t *testing.T, code, message string) json.RawMessage {
	t.Helper()
	return rawJSON(t, map[string]any{
		fieldStatus: "error",
		"code":      code,
		"message":   message,
	})
}

// TestPoll_SuccessOnFirstPoll: success item returned immediately.
func TestPoll_SuccessOnFirstPoll(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{successItem(t, map[string]any{"videoURL": "https://example.com/v.mp4"})}},
		},
	}
	c := NewClient(mock, slog.Default())
	results, err := c.Poll(context.Background(), uuid.Nil, time.Millisecond, 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// TestPoll_ProcessingSkippedUntilSuccess: processing items are not returned.
func TestPoll_ProcessingSkippedUntilSuccess(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{processingItem(t, 20)}},
			{data: []json.RawMessage{processingItem(t, 60)}},
			{data: []json.RawMessage{successItem(t, nil)}},
		},
	}
	c := NewClient(mock, slog.Default())
	results, err := c.Poll(context.Background(), uuid.Nil, time.Millisecond, 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 success result, got %d", len(results))
	}
	if mock.callCount != 3 {
		t.Errorf("expected 3 poll calls, got %d", mock.callCount)
	}
}

// TestPoll_OnProgressCalled: onProgress fires with the right values.
func TestPoll_OnProgressCalled(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{processingItem(t, 25)}},
			{data: []json.RawMessage{processingItem(t, 75)}},
			{data: []json.RawMessage{successItem(t, nil)}},
		},
	}
	c := NewClient(mock, slog.Default())

	var got []int
	_, err := c.Poll(context.Background(), uuid.Nil, time.Millisecond, 1, func(p int) {
		got = append(got, p)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != 25 || got[1] != 75 {
		t.Errorf("expected progress [25 75], got %v", got)
	}
}

// TestPoll_ErrorStatusReturnsError: a data item with status "error" stops polling.
func TestPoll_ErrorStatusReturnsError(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{errorItem(t, "timeoutProvider", "provider timed out")}},
		},
	}
	_, err := NewClient(mock, slog.Default()).Poll(context.Background(), uuid.Nil, time.Millisecond, 1, nil)
	if err == nil {
		t.Fatal("expected error for status error, got nil")
	}
	if !strings.Contains(err.Error(), "timeoutProvider") || !strings.Contains(err.Error(), "provider timed out") {
		t.Errorf("unexpected error: %v", err)
	}
	if mock.callCount != 1 {
		t.Errorf("expected 1 poll call, got %d", mock.callCount)
	}
}

// TestPoll_ErrorAfterProcessing: processing followed by error stops without hanging.
func TestPoll_ErrorAfterProcessing(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{processingItem(t, 40)}},
			{data: []json.RawMessage{errorItem(t, "processingFailed", "generation failed")}},
		},
	}
	_, err := NewClient(mock, slog.Default()).Poll(context.Background(), uuid.Nil, time.Millisecond, 1, nil)
	if err == nil {
		t.Fatal("expected error after processing, got nil")
	}
	if !strings.Contains(err.Error(), "generation failed") {
		t.Errorf("unexpected error: %v", err)
	}
	if mock.callCount != 2 {
		t.Errorf("expected 2 poll calls, got %d", mock.callCount)
	}
}

// TestPoll_MultiResultReturnsAllFromOneCycle: minResults=4 keeps retrying when
// a partial burst delivers fewer than 4 success items, then returns all 4 once
// the server delivers them together in one cycle. Each poll cycle is evaluated
// independently — no cross-cycle accumulation — so no duplicates arise even
// though the server re-sends all completed results on every getResponse call.
func TestPoll_MultiResultReturnsAllFromOneCycle(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			// Cycle 1: only 2 success items (partial burst / task still in progress).
			{data: []json.RawMessage{successItem(t, nil), successItem(t, nil)}},
			// Cycle 2: all 4 success items — server re-sends the full completed set.
			{data: []json.RawMessage{
				successItem(t, nil), successItem(t, nil),
				successItem(t, nil), successItem(t, nil),
			}},
		},
	}
	c := NewClient(mock, slog.Default())
	results, err := c.Poll(context.Background(), uuid.Nil, time.Millisecond, 4, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 results (from cycle 2 only), got %d", len(results))
	}
	if mock.callCount != 2 {
		t.Errorf("expected exactly 2 poll calls, got %d", mock.callCount)
	}
}

// TestPoll_NilProgressCallback: nil onProgress must not panic on processing items.
func TestPoll_NilProgressCallback(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{processingItem(t, 50)}},
			{data: []json.RawMessage{successItem(t, nil)}},
		},
	}
	c := NewClient(mock, slog.Default())
	_, err := c.Poll(context.Background(), uuid.Nil, time.Millisecond, 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
