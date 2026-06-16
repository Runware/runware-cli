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
// Send returns pre-configured responses in order; Close is a no-op.
// All tasks arguments passed to Send are recorded in captured for inspection.
type mockTransport struct {
	responses []mockResponse
	callCount int
	captured  [][]any
}

type mockResponse struct {
	data []json.RawMessage
	err  error
}

func (m *mockTransport) Close() error { return nil }
func (m *mockTransport) Send(_ context.Context, tasks []any) ([]json.RawMessage, error) {
	m.captured = append(m.captured, tasks)
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

// ---- Client.Poll polling-mechanics tests ----
// Tests for success, progress, and nil-callback behaviour live in client_test.go.

// TestClientPoll_TransientErrorKeepsPolling: non-fatal errors are retried.
func TestClientPoll_TransientErrorKeepsPolling(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{err: fmt.Errorf("temporary error")},
			{data: []json.RawMessage{successItem(t, nil)}},
		},
	}

	results, err := NewClient(mock, slog.Default()).Poll(context.Background(), uuid.Nil, time.Millisecond, 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

// TestClientPoll_AuthErrorFatal: auth errors are returned immediately.
func TestClientPoll_AuthErrorFatal(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{err: transport.CreateRunwareError("invalidApiKey", "unauthorized", transport.RunwareErrorDetails{})},
		},
	}

	_, err := NewClient(mock, slog.Default()).Poll(context.Background(), uuid.Nil, time.Millisecond, 1, nil)
	if !transport.IsAuthError(err) {
		t.Errorf("expected auth error, got %v", err)
	}
	if mock.callCount != 1 {
		t.Errorf("expected exactly 1 call, got %d", mock.callCount)
	}
}

// TestClientPoll_APIErrorFatal: RunwareError is returned immediately.
func TestClientPoll_APIErrorFatal(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{err: transport.CreateRunwareError("unknown", "bad request", transport.RunwareErrorDetails{})},
		},
	}

	_, err := NewClient(mock, slog.Default()).Poll(context.Background(), uuid.Nil, time.Millisecond, 1, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestClientPoll_ContextCancelled: cancelled context returns context.Canceled.
func TestClientPoll_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	mock := &mockTransport{
		responses: []mockResponse{
			{data: nil},
		},
	}

	_, err := NewClient(mock, slog.Default()).Poll(ctx, uuid.Nil, time.Millisecond, 1, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected %v, got error: %v", context.Canceled, err)
	}
}

// TestClientPoll_ContextTimeout: timed-out context returns context.DeadlineExceeded.
func TestClientPoll_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	// Never return success — mock returns nil indefinitely.
	mock := &mockTransport{}

	_, err := NewClient(mock, slog.Default()).Poll(ctx, uuid.Nil, time.Millisecond, 1, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected %v, got error: %v", context.DeadlineExceeded, err)
	}
}
