package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"
)

// TestMediaStorage_UploadSuccess: a valid upload response is parsed and the
// request carries the mediaStorage task type, the upload operation, and media.
func TestMediaStorage_UploadSuccess(t *testing.T) {
	mediaUUID := uuid.New()
	mediaURL := "https://im.runware.ai/asset.png"
	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{rawJSON(t, map[string]any{
				fieldTaskType: "mediaStorage",
				"taskUUID":    uuid.New().String(),
				"operation":   MediaOperationUpload,
				"mediaUUID":   mediaUUID.String(),
				"mediaURL":    mediaURL,
			})}},
		},
	}

	c := NewClient(mock, slog.Default())
	result, err := c.MediaStorage(context.Background(), MediaOperationUpload, "data:image/png;base64,AAAA")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MediaUUID != mediaUUID {
		t.Errorf("MediaUUID = %v, want %v", result.MediaUUID, mediaUUID)
	}
	if result.MediaURL != mediaURL {
		t.Errorf("MediaURL = %q, want %q", result.MediaURL, mediaURL)
	}

	if len(mock.captured) != 1 || len(mock.captured[0]) != 1 {
		t.Fatalf("expected one submitted task, got %#v", mock.captured)
	}
	req, ok := mock.captured[0][0].(*MediaStorageRequest)
	if !ok {
		t.Fatalf("submitted task is not *MediaStorageRequest: %T", mock.captured[0][0])
	}
	if req.TaskType != taskTypeMediaStorage {
		t.Errorf("TaskType = %q, want %q", req.TaskType, taskTypeMediaStorage)
	}
	if req.Operation != MediaOperationUpload {
		t.Errorf("Operation = %q, want %q", req.Operation, MediaOperationUpload)
	}
	if req.Media != "data:image/png;base64,AAAA" {
		t.Errorf("Media = %q, want the supplied value", req.Media)
	}
}

// TestMediaStorage_DeleteSuccess: a delete response (no mediaURL) is parsed and
// the request carries the delete operation with the target UUID as media.
func TestMediaStorage_DeleteSuccess(t *testing.T) {
	mediaUUID := uuid.New()
	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{rawJSON(t, map[string]any{
				fieldTaskType: "mediaStorage",
				"taskUUID":    uuid.New().String(),
				"operation":   MediaOperationDelete,
				"mediaUUID":   mediaUUID.String(),
			})}},
		},
	}

	c := NewClient(mock, slog.Default())
	result, err := c.MediaStorage(context.Background(), MediaOperationDelete, mediaUUID.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MediaUUID != mediaUUID {
		t.Errorf("MediaUUID = %v, want %v", result.MediaUUID, mediaUUID)
	}
	if result.MediaURL != "" {
		t.Errorf("MediaURL = %q, want empty for delete", result.MediaURL)
	}

	req, ok := mock.captured[0][0].(*MediaStorageRequest)
	if !ok {
		t.Fatalf("submitted task is not *MediaStorageRequest: %T", mock.captured[0][0])
	}
	if req.Operation != MediaOperationDelete {
		t.Errorf("Operation = %q, want %q", req.Operation, MediaOperationDelete)
	}
	if req.Media != mediaUUID.String() {
		t.Errorf("Media = %q, want the target UUID", req.Media)
	}
}

// TestMediaStorage_EmptyOperation: an empty operation is rejected before any
// transport call.
func TestMediaStorage_EmptyOperation(t *testing.T) {
	mock := &mockTransport{}
	c := NewClient(mock, slog.Default())
	if _, err := c.MediaStorage(context.Background(), "", "data:image/png;base64,AAAA"); err == nil {
		t.Fatal("expected error for empty operation, got nil")
	}
	if mock.callCount != 0 {
		t.Errorf("expected no transport calls, got %d", mock.callCount)
	}
}

// TestMediaStorage_EmptyMedia: an empty media value is rejected before any
// transport call.
func TestMediaStorage_EmptyMedia(t *testing.T) {
	mock := &mockTransport{}
	c := NewClient(mock, slog.Default())
	if _, err := c.MediaStorage(context.Background(), MediaOperationUpload, ""); err == nil {
		t.Fatal("expected error for empty media, got nil")
	}
	if mock.callCount != 0 {
		t.Errorf("expected no transport calls, got %d", mock.callCount)
	}
}

// TestMediaStorage_EmptyResponse: an empty data slice returns an error.
func TestMediaStorage_EmptyResponse(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{}},
		},
	}
	c := NewClient(mock, slog.Default())
	if _, err := c.MediaStorage(context.Background(), MediaOperationDelete, uuid.New().String()); err == nil {
		t.Fatal("expected error for empty response, got nil")
	}
}
