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
)

// mockClient is a test double for Client that only implements GetResponse.
// All other methods panic if called.
type mockClient struct {
	responses []mockResponse
	callCount int
}

type mockResponse struct {
	data []json.RawMessage
	err  error
}

func (m *mockClient) GetResponse(_ context.Context, _ uuid.UUID) ([]json.RawMessage, error) {
	if m.callCount >= len(m.responses) {
		return nil, nil
	}
	r := m.responses[m.callCount]
	m.callCount++
	return r.data, r.err
}

func (m *mockClient) Ping(_ context.Context) (*PingResult, error) { panic("not implemented") }
func (m *mockClient) ImageInference(_ context.Context, _ *ImageInferenceRequest) ([]ImageInferenceResult, error) {
	panic("not implemented")
}
func (m *mockClient) VideoInference(_ context.Context, _ *VideoInferenceRequest) ([]VideoInferenceResult, error) {
	panic("not implemented")
}
func (m *mockClient) AudioInference(_ context.Context, _ *AudioInferenceRequest) ([]AudioInferenceResult, error) {
	panic("not implemented")
}
func (m *mockClient) TextInference(_ context.Context, _ *TextInferenceRequest) ([]TextInferenceResult, error) {
	panic("not implemented")
}
func (m *mockClient) AccountDetails(_ context.Context) (*AccountResult, error) {
	panic("not implemented")
}
func (m *mockClient) ModelSearch(_ context.Context, _ *ModelSearchRequest) (*ModelSearchResponse, error) {
	panic("not implemented")
}
func (m *mockClient) Raw(_ context.Context, _ []any) (*APIResponse, error) { panic("not implemented") }

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
	client := &mockClient{
		responses: []mockResponse{
			{data: []json.RawMessage{rawJSON(t, "result1"), rawJSON(t, "result2")}},
		},
	}

	results, err := PollResults(context.Background(), client, uuid.Nil, time.Millisecond, slog.Default(), parseString)
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
	client := &mockClient{
		responses: []mockResponse{
			{data: nil},
			{data: nil},
			{data: []json.RawMessage{rawJSON(t, "final")}},
		},
	}

	results, err := PollResults(context.Background(), client, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0] != "final" {
		t.Errorf("unexpected results: %v", results)
	}
	if client.callCount != 3 {
		t.Errorf("expected 3 GetResponse calls, got %d", client.callCount)
	}
}

// TestPollResults_ParseFailureKeepsPolling: raw data present but parse returns false — must keep polling.
func TestPollResults_ParseFailureKeepsPolling(t *testing.T) {
	client := &mockClient{
		responses: []mockResponse{
			// parse will return false for empty string
			{data: []json.RawMessage{rawJSON(t, "")}},
			{data: []json.RawMessage{rawJSON(t, "valid")}},
		},
	}

	results, err := PollResults(context.Background(), client, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0] != "valid" {
		t.Errorf("unexpected results: %v", results)
	}
	if client.callCount != 2 {
		t.Errorf("expected 2 GetResponse calls, got %d", client.callCount)
	}
}

// TestPollResults_TransientErrorKeepsPolling: non-fatal errors are retried.
func TestPollResults_TransientErrorKeepsPolling(t *testing.T) {
	client := &mockClient{
		responses: []mockResponse{
			{err: fmt.Errorf("temporary error")},
			{data: []json.RawMessage{rawJSON(t, "ok")}},
		},
	}

	results, err := PollResults(context.Background(), client, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0] != "ok" {
		t.Errorf("unexpected results: %v", results)
	}
}

// TestPollResults_AuthErrorFatal: auth errors are returned immediately.
func TestPollResults_AuthErrorFatal(t *testing.T) {
	client := &mockClient{
		responses: []mockResponse{
			{err: ErrUnauthorized},
		},
	}

	_, err := PollResults(context.Background(), client, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
	if client.callCount != 1 {
		t.Errorf("expected exactly 1 call, got %d", client.callCount)
	}
}

// TestPollResults_APIErrorFatal: APIError is returned immediately.
func TestPollResults_APIErrorFatal(t *testing.T) {
	apiErr := APIError{Message: "bad request"}
	client := &mockClient{
		responses: []mockResponse{
			{err: apiErr},
		},
	}

	_, err := PollResults(context.Background(), client, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestPollResults_ContextCancelled: cancelled context returns nil, nil.
func TestPollResults_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	client := &mockClient{
		responses: []mockResponse{
			{data: nil},
		},
	}

	_, err := PollResults(ctx, client, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected %v, got error: %v", context.Canceled, err)
	}
}

// TestPollResults_ContextTimeout: timed-out context returns nil, nil.
func TestPollResults_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	// Never return success — mock returns nil indefinitely.
	client := &mockClient{}

	_, err := PollResults(ctx, client, uuid.Nil, time.Millisecond, slog.Default(), parseString)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected %v, got error: %v", context.DeadlineExceeded, err)
	}
}
