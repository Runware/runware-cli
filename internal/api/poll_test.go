package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/api/transport"
)

// mockTransport is a test double for transport.Transport.
// Send returns pre-configured responses in order; Connect and Disconnect are no-ops.
type mockTransport struct {
	responses []mockResponse
	callCount int
}

type mockResponse struct {
	data []json.RawMessage
	err  error
}

func (m *mockTransport) Connect(_ context.Context) error { return nil }
func (m *mockTransport) Disconnect() error               { return nil }
func (m *mockTransport) Send(_ context.Context, _ []any) ([]json.RawMessage, error) {
	if m.callCount >= len(m.responses) {
		return nil, nil
	}
	r := m.responses[m.callCount]
	m.callCount++
	if r.err != nil {
		return nil, r.err
	}
	return r.data, nil
}

// Compile-time check that mockTransport implements transport.Transport.
var _ transport.Transport = (*mockTransport)(nil)

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("rawJSON: %v", err)
	}
	return b
}

// parseString parses a JSON string value and returns it if non-empty.
func parseString(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, s != ""
}

// TestPollResults_ReturnOnFirstSuccess: results available on first call.
func TestPollResults_ReturnOnFirstSuccess(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{rawJSON(t, "result1"), rawJSON(t, "result2")}},
		},
	}

	results, err := PollResults(context.Background(), mock, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0] != "result1" || results[1] != "result2" {
		t.Errorf("unexpected results: %v", results)
	}
}

// TestPollResults_RetriesUntilSuccess: empty responses before a successful one.
func TestPollResults_RetriesUntilSuccess(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{data: nil},
			{data: nil},
			{data: []json.RawMessage{rawJSON(t, "final")}},
		},
	}

	results, err := PollResults(context.Background(), mock, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0] != "final" {
		t.Errorf("unexpected results: %v", results)
	}
	if mock.callCount != 3 {
		t.Errorf("expected 3 Send calls, got %d", mock.callCount)
	}
}

// TestPollResults_ParseFailureKeepsPolling: raw data present but parse returns false — must keep polling.
func TestPollResults_ParseFailureKeepsPolling(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			// parse will return false for empty string
			{data: []json.RawMessage{rawJSON(t, "")}},
			{data: []json.RawMessage{rawJSON(t, "valid")}},
		},
	}

	results, err := PollResults(context.Background(), mock, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0] != "valid" {
		t.Errorf("unexpected results: %v", results)
	}
	if mock.callCount != 2 {
		t.Errorf("expected 2 Send calls, got %d", mock.callCount)
	}
}

// TestPollResults_TransientErrorKeepsPolling: non-fatal errors are retried.
func TestPollResults_TransientErrorKeepsPolling(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{err: fmt.Errorf("temporary error")},
			{data: []json.RawMessage{rawJSON(t, "ok")}},
		},
	}

	results, err := PollResults(context.Background(), mock, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0] != "ok" {
		t.Errorf("unexpected results: %v", results)
	}
}

// TestPollResults_AuthErrorFatal: auth errors are returned immediately.
func TestPollResults_AuthErrorFatal(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{err: transport.ErrUnauthorized},
		},
	}

	_, err := PollResults(context.Background(), mock, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if !errors.Is(err, transport.ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
	if mock.callCount != 1 {
		t.Errorf("expected exactly 1 call, got %d", mock.callCount)
	}
}

// TestPollResults_APIErrorFatal: APIError is returned immediately.
func TestPollResults_APIErrorFatal(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{err: transport.APIError{Message: "bad request"}},
		},
	}

	_, err := PollResults(context.Background(), mock, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestPollResults_ContextCancelled: cancelled context returns context.Canceled.
func TestPollResults_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	mock := &mockTransport{
		responses: []mockResponse{
			{data: nil},
		},
	}

	_, err := PollResults(ctx, mock, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected %v, got error: %v", context.Canceled, err)
	}
}

// TestPollResults_ContextTimeout: timed-out context returns context.DeadlineExceeded.
func TestPollResults_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	// Never return success — mock returns nil indefinitely.
	mock := &mockTransport{}

	_, err := PollResults(ctx, mock, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected %v, got error: %v", context.DeadlineExceeded, err)
	}
}
