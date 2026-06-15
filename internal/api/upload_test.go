package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"
)

// TestUploadImage_Success: a valid imageUpload response is parsed and the request
// carries the imageUpload task type and the supplied image value.
func TestUploadImage_Success(t *testing.T) {
	imageUUID := uuid.New()
	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{rawJSON(t, map[string]any{
				fieldTaskType: "imageUpload",
				"taskUUID":    uuid.New().String(),
				"imageUUID":   imageUUID.String(),
			})}},
		},
	}

	c := NewClient(mock, slog.Default())
	result, err := c.UploadImage(context.Background(), "data:image/png;base64,AAAA")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ImageUUID != imageUUID {
		t.Errorf("ImageUUID = %v, want %v", result.ImageUUID, imageUUID)
	}

	if len(mock.captured) != 1 || len(mock.captured[0]) != 1 {
		t.Fatalf("expected one submitted task, got %#v", mock.captured)
	}
	req, ok := mock.captured[0][0].(*ImageUploadRequest)
	if !ok {
		t.Fatalf("submitted task is not *ImageUploadRequest: %T", mock.captured[0][0])
	}
	if req.TaskType != taskTypeImageUpload {
		t.Errorf("TaskType = %q, want %q", req.TaskType, taskTypeImageUpload)
	}
	if req.Image != "data:image/png;base64,AAAA" {
		t.Errorf("Image = %q, want the supplied value", req.Image)
	}
}

// TestUploadImage_EmptyImage: an empty image is rejected before any transport call.
func TestUploadImage_EmptyImage(t *testing.T) {
	mock := &mockTransport{}
	c := NewClient(mock, slog.Default())
	if _, err := c.UploadImage(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty image, got nil")
	}
	if mock.callCount != 0 {
		t.Errorf("expected no transport calls, got %d", mock.callCount)
	}
}

// TestUploadImage_EmptyResponse: an empty data slice returns an error.
func TestUploadImage_EmptyResponse(t *testing.T) {
	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{}},
		},
	}
	c := NewClient(mock, slog.Default())
	if _, err := c.UploadImage(context.Background(), "https://example.com/x.png"); err == nil {
		t.Fatal("expected error for empty response, got nil")
	}
}
