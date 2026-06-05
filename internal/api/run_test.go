package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testModelAIR is a placeholder AIR identifier used across Client.Run tests.
const testModelAIR = "test:model@1"

// inferenceSchemaServer starts an httptest.Server that returns the given
// requestSchema JSON for any request. The returned URL (with trailing slash) is
// suitable for use as Client.schemaBaseURL in tests.
func inferenceSchemaServer(t *testing.T, requestSchema any) *httptest.Server {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"requestSchema":  requestSchema,
		"responseSchema": map[string]any{},
		"documentation":  "",
	})
	if err != nil {
		t.Fatalf("inferenceSchemaServer: marshal: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// requestSchemaWithTaskType returns a minimal request schema whose taskType
// property has a const value, and whose deliveryMethod property has the given
// default (empty string means the property is omitted entirely).
func requestSchemaWithTaskType(taskType, deliveryMethodDefault string) map[string]any {
	props := map[string]any{
		"taskType": map[string]any{
			"const": taskType,
		},
	}
	if deliveryMethodDefault != "" {
		props["deliveryMethod"] = map[string]any{
			"default": deliveryMethodDefault,
		}
	}
	return map[string]any{
		"properties": props,
	}
}

// TestClientRun_SyncSuccess: schema auto-detects taskType and sync delivery;
// the submit response is returned directly without polling.
func TestClientRun_SyncSuccess(t *testing.T) {
	srv := inferenceSchemaServer(t, requestSchemaWithTaskType("imageInference", "sync"))

	expected := successItem(t, map[string]any{"imageURL": "https://example.com/img.png"})
	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{expected}},
		},
	}

	c := NewClient(mock, slog.Default())
	c.schemaBaseURL = srv.URL + "/"

	results, err := c.Run(context.Background(), testModelAIR, map[string]any{}, RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	// Only one transport call: the submit — no poll.
	if mock.callCount != 1 {
		t.Errorf("expected 1 transport call (submit only), got %d", mock.callCount)
	}
}

// TestClientRun_AsyncSuccess: schema auto-detects taskType and async delivery;
// submit is called once, then Poll is called until a success item is received.
func TestClientRun_AsyncSuccess(t *testing.T) {
	srv := inferenceSchemaServer(t, requestSchemaWithTaskType("videoInference", "async"))

	submitAck := rawJSON(t, map[string]any{"taskUUID": "some-uuid"})
	pollResult := successItem(t, map[string]any{"videoURL": "https://example.com/vid.mp4"})

	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{submitAck}},  // submit
			{data: []json.RawMessage{pollResult}}, // first poll → success
		},
	}

	c := NewClient(mock, slog.Default())
	c.schemaBaseURL = srv.URL + "/"

	results, err := c.Run(context.Background(), testModelAIR, map[string]any{}, RunOptions{
		PollInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	// Two transport calls: submit + one poll.
	if mock.callCount != 2 {
		t.Errorf("expected 2 transport calls (submit + poll), got %d", mock.callCount)
	}
}

// TestClientRun_SchemaUnavailable_TaskTypeProvided: schema endpoint returns 404;
// because TaskType is provided in RunOptions the call proceeds without validation.
func TestClientRun_SchemaUnavailable_TaskTypeProvided(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	expected := successItem(t, map[string]any{"audioURL": "https://example.com/aud.mp3"})
	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{expected}},
		},
	}

	c := NewClient(mock, slog.Default())
	c.schemaBaseURL = srv.URL + "/"

	results, err := c.Run(context.Background(), testModelAIR, map[string]any{}, RunOptions{
		TaskType: "audioInference",
	})
	if err != nil {
		t.Fatalf("unexpected error when schema unavailable but TaskType provided: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// TestClientRun_SchemaUnavailable_NoTaskType: schema endpoint returns 404 and no
// TaskType is provided; Run must return a descriptive error.
func TestClientRun_SchemaUnavailable_NoTaskType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(&mockTransport{}, slog.Default())
	c.schemaBaseURL = srv.URL + "/"

	_, err := c.Run(context.Background(), testModelAIR, map[string]any{}, RunOptions{})
	if err == nil {
		t.Fatal("expected error when schema unavailable and no TaskType, got nil")
	}
	if !strings.Contains(err.Error(), "RunOptions.TaskType") {
		t.Errorf("expected error to mention RunOptions.TaskType, got: %v", err)
	}
}

// TestClientRun_ValidationFailure: schema declares a required field that is absent
// from params; Run must return a validation error before submitting.
func TestClientRun_ValidationFailure(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{
			"taskType":       map[string]any{"const": "imageInference"},
			"deliveryMethod": map[string]any{"default": "sync"},
		},
		"required": []string{"positivePrompt"},
	}
	srv := inferenceSchemaServer(t, schema)

	mock := &mockTransport{}

	c := NewClient(mock, slog.Default())
	c.schemaBaseURL = srv.URL + "/"

	_, err := c.Run(context.Background(), testModelAIR, map[string]any{}, RunOptions{})
	if err == nil {
		t.Fatal("expected validation error for missing required field, got nil")
	}
	if !strings.Contains(err.Error(), "positivePrompt") {
		t.Errorf("expected error to mention positivePrompt, got: %v", err)
	}
	// No transport calls should have been made — error must surface before submit.
	if mock.callCount != 0 {
		t.Errorf("expected 0 transport calls before validation error, got %d", mock.callCount)
	}
}

// TestClientRun_OnProgressCalled: async delivery; OnProgress receives each
// progress value reported by the poll loop before the success item arrives.
func TestClientRun_OnProgressCalled(t *testing.T) {
	srv := inferenceSchemaServer(t, requestSchemaWithTaskType("videoInference", "async"))

	mock := &mockTransport{
		responses: []mockResponse{
			{data: []json.RawMessage{rawJSON(t, map[string]any{"taskUUID": "x"})}}, // submit
			{data: []json.RawMessage{processingItem(t, 30)}},
			{data: []json.RawMessage{processingItem(t, 70)}},
			{data: []json.RawMessage{successItem(t, nil)}},
		},
	}

	c := NewClient(mock, slog.Default())
	c.schemaBaseURL = srv.URL + "/"

	var got []int
	_, err := c.Run(context.Background(), testModelAIR, map[string]any{}, RunOptions{
		PollInterval: time.Millisecond,
		OnProgress: func(p int) {
			got = append(got, p)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fmt.Sprint(got) != "[30 70]" {
		t.Errorf("expected progress [30 70], got %v", got)
	}
}
