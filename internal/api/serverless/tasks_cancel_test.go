package serverless

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testTaskCanceled = `{"id":"7c9e6679-7425-40de-944b-e07fc1f90ae7","status":"canceled","appId":"my-app",` +
	`"endpointPath":"infer","createdAt":"2026-07-30T12:00:00Z","completedAt":"2026-07-30T12:00:05Z"}`

func TestCancelTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/tasks/" + testTaskID + "/cancel"
		if r.Method != http.MethodPost || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testTaskCanceled))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	task, err := c.CancelTask(context.Background(), testAppID, testTaskID)
	if err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if task.Status != TaskStatusCanceled {
		t.Fatalf("unexpected task: %+v", task)
	}
}

func TestCancelTask_RefusesAStartedTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"type":"https://docs.runware.ai/serverless/errors#task-not-cancelable",` +
			`"title":"Conflict","status":409,"detail":"The task has already started or finished, so it cannot be canceled"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.CancelTask(context.Background(), testAppID, testTaskID)
	if err == nil || !strings.Contains(err.Error(), "cannot be canceled") {
		t.Fatalf("expected the refusal's detail, got %v", err)
	}
}

func TestCancelTask_InvalidID(t *testing.T) {
	c := NewClient("test-key", "https://example.invalid", slog.Default())
	_, err := c.CancelTask(context.Background(), testAppID, "not-a-uuid")
	if err == nil || !strings.Contains(err.Error(), "invalid task id") {
		t.Fatalf("expected invalid task id error, got %v", err)
	}
}

func TestCancelQueuedTasks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/tasks/cancel"
		if r.Method != http.MethodPost || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"canceled":3}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	canceled, err := c.CancelQueuedTasks(context.Background(), testAppID)
	if err != nil {
		t.Fatalf("CancelQueuedTasks: %v", err)
	}
	if canceled != 3 {
		t.Fatalf("canceled = %d", canceled)
	}
}
