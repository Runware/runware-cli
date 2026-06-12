package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/api/transport"
)

// mockStreamTransport is a test double for transport.StreamSender. SendStream
// feeds the configured frames to onFrame in order, or fails with streamErr.
type mockStreamTransport struct {
	mockTransport
	frames       []json.RawMessage
	streamErr    error
	streamedTask any
}

func (m *mockStreamTransport) SendStream(_ context.Context, task any, onFrame func(json.RawMessage) (bool, error)) error {
	m.streamedTask = task
	if m.streamErr != nil {
		return m.streamErr
	}
	for _, f := range m.frames {
		done, err := onFrame(f)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return nil
}

// Compile-time check that mockStreamTransport implements transport.StreamSender.
var _ transport.StreamSender = (*mockStreamTransport)(nil)

// uploadStatusItem builds a raw modelUpload pipeline frame for mock responses.
func uploadStatusItem(t *testing.T, status, message, air string) json.RawMessage {
	t.Helper()
	return rawJSON(t, map[string]any{
		fieldTaskType: "modelUpload",
		"status":      status,
		"message":     message,
		"air":         air,
	})
}

// minimalUploadRequest returns a request with only the required fields set.
func minimalUploadRequest() ModelUploadRequest {
	return ModelUploadRequest{
		Category:     "lora",
		Name:         "test-model",
		Version:      "1",
		DownloadURL:  "https://example.com/model.safetensors",
		Architecture: "sdxl",
		Format:       "safetensors",
	}
}

// TestModelUpload_PipelinePhasesThenReady: intermediate statuses are reported
// in order through OnStatus, then the ready frame resolves the call.
func TestModelUpload_PipelinePhasesThenReady(t *testing.T) {
	mock := &mockStreamTransport{
		frames: []json.RawMessage{
			uploadStatusItem(t, "validated", "", ""),
			uploadStatusItem(t, "downloaded", "", ""),
			uploadStatusItem(t, "optimized", "", ""),
			uploadStatusItem(t, "stored", "", ""),
			uploadStatusItem(t, "ready", "Model ready.", "myorg:42@1"),
		},
	}

	var statuses []string
	res, err := NewClient(mock, slog.Default()).ModelUpload(context.Background(), minimalUploadRequest(), ModelUploadOptions{
		OnStatus: func(status, _ string) {
			statuses = append(statuses, status)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"validated", "downloaded", "optimized", "stored"}
	if !slices.Equal(statuses, want) {
		t.Errorf("statuses = %v, want %v", statuses, want)
	}
	if res.AIR != "myorg:42@1" {
		t.Errorf("AIR = %q, want %q", res.AIR, "myorg:42@1")
	}
}

// TestModelUpload_DuplicateStatusDeduped: repeated frames for the same phase
// fire OnStatus only once per phase.
func TestModelUpload_DuplicateStatusDeduped(t *testing.T) {
	mock := &mockStreamTransport{
		frames: []json.RawMessage{
			uploadStatusItem(t, "validated", "", ""),
			uploadStatusItem(t, "downloaded", "", ""),
			uploadStatusItem(t, "downloaded", "", ""),
			uploadStatusItem(t, "ready", "", "myorg:42@1"),
		},
	}

	var statuses []string
	_, err := NewClient(mock, slog.Default()).ModelUpload(context.Background(), minimalUploadRequest(), ModelUploadOptions{
		OnStatus: func(status, _ string) {
			statuses = append(statuses, status)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"validated", "downloaded"}
	if !slices.Equal(statuses, want) {
		t.Errorf("statuses = %v, want %v", statuses, want)
	}
}

// TestModelUpload_FailedReturnsError: a "failed" frame surfaces its message.
func TestModelUpload_FailedReturnsError(t *testing.T) {
	mock := &mockStreamTransport{
		frames: []json.RawMessage{
			uploadStatusItem(t, "validated", "", ""),
			uploadStatusItem(t, "failed", "checksum mismatch", ""),
		},
	}

	_, err := NewClient(mock, slog.Default()).ModelUpload(context.Background(), minimalUploadRequest(), ModelUploadOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("expected error to contain failure message, got: %v", err)
	}
}

// TestModelUpload_InjectsSystemFields: taskType and taskUUID are injected into
// the streamed task payload.
func TestModelUpload_InjectsSystemFields(t *testing.T) {
	mock := &mockStreamTransport{
		frames: []json.RawMessage{
			uploadStatusItem(t, "ready", "", "myorg:42@1"),
		},
	}

	_, err := NewClient(mock, slog.Default()).ModelUpload(context.Background(), minimalUploadRequest(), ModelUploadOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	streamed, ok := mock.streamedTask.(ModelUploadRequest)
	if !ok {
		t.Fatalf("expected ModelUploadRequest, got %T", mock.streamedTask)
	}
	if streamed.TaskType != taskTypeModelUpload {
		t.Errorf("taskType = %q, want %q", streamed.TaskType, taskTypeModelUpload)
	}
	if streamed.TaskUUID == uuid.Nil {
		t.Error("taskUUID not injected")
	}
}

// TestModelUpload_StreamErrorPropagated: transport-level errors (e.g. API
// error frames) are returned to the caller.
func TestModelUpload_StreamErrorPropagated(t *testing.T) {
	mock := &mockStreamTransport{
		streamErr: transport.CreateRunwareError("invalidCategory", "bad category", transport.RunwareErrorDetails{}),
	}

	_, err := NewClient(mock, slog.Default()).ModelUpload(context.Background(), minimalUploadRequest(), ModelUploadOptions{})
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
}

// TestModelUpload_StreamEndsWithoutTerminal: a stream that ends without a
// terminal frame is an error, not a nil result.
func TestModelUpload_StreamEndsWithoutTerminal(t *testing.T) {
	mock := &mockStreamTransport{
		frames: []json.RawMessage{
			uploadStatusItem(t, "validated", "", ""),
		},
	}

	_, err := NewClient(mock, slog.Default()).ModelUpload(context.Background(), minimalUploadRequest(), ModelUploadOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "terminal status") {
		t.Errorf("expected terminal-status error, got: %v", err)
	}
}

// TestModelUpload_NonStreamingTransportRejected: a transport without streaming
// support (e.g. HTTP) is rejected with a clear redirect error.
func TestModelUpload_NonStreamingTransportRejected(t *testing.T) {
	mock := &mockTransport{}

	_, err := NewClient(mock, slog.Default()).ModelUpload(context.Background(), minimalUploadRequest(), ModelUploadOptions{})
	if !errors.Is(err, ErrModelUploadTransport) {
		t.Fatalf("expected ErrModelUploadTransport, got: %v", err)
	}
	if mock.callCount != 0 {
		t.Errorf("expected 0 transport calls, got %d", mock.callCount)
	}
}

// TestModelUploadRequest_OmitsUnsetOptionalFields: optional pointer fields must
// not appear in the JSON payload unless set.
func TestModelUploadRequest_OmitsUnsetOptionalFields(t *testing.T) {
	b, err := json.Marshal(minimalUploadRequest())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{"private", "defaultCFG", "defaultSteps", "defaultStrength", "defaultWeight", "tags", "air"} {
		if _, present := payload[key]; present {
			t.Errorf("key %q present in payload, want omitted", key)
		}
	}
}
