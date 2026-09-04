package serverless

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/api/transport"
)

const (
	testTaskID     = "7c9e6679-7425-40de-944b-e07fc1f90ae7"
	testEndpoint   = "infer"
	testTaskJSON   = `{"id":"7c9e6679-7425-40de-944b-e07fc1f90ae7","status":"pending","appId":"my-app","endpointPath":"infer","createdAt":"2026-07-30T12:00:00Z"}`
	testTaskDone   = `{"id":"7c9e6679-7425-40de-944b-e07fc1f90ae7","status":"completed","appId":"my-app","endpointPath":"infer","createdAt":"2026-07-30T12:00:00Z","completedAt":"2026-07-30T12:00:05Z","output":{"ok":true}}`
	testTaskFailed = `{"id":"7c9e6679-7425-40de-944b-e07fc1f90ae7","status":"failed","appId":"my-app","endpointPath":"infer","createdAt":"2026-07-30T12:00:00Z","error":"oom killed"}`
)

func TestValidateEndpointPath(t *testing.T) {
	cases := []struct {
		path    string
		wantErr string
	}{
		{path: "infer"},
		{path: "a"},
		{path: "my-endpoint"},
		{path: "", wantErr: "required"},
		{path: "/infer", wantErr: "bare segment"},
		{path: "/infer", wantErr: `"infer"`},
		{path: "Infer", wantErr: "invalid"},
		{path: "my_endpoint", wantErr: "invalid"},
		{path: "infer/", wantErr: "invalid"},
		{path: "/", wantErr: "leading slash"},
	}
	for _, tc := range cases {
		err := ValidateEndpointPath(tc.path)
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("ValidateEndpointPath(%q): %v", tc.path, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("ValidateEndpointPath(%q): expected error containing %q", tc.path, tc.wantErr)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("ValidateEndpointPath(%q): error %q does not contain %q", tc.path, err, tc.wantErr)
		}
	}
}

func TestInvokeAsync(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/invoke-async/" + testEndpoint
		if r.Method != http.MethodPost || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		posts.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var invocation map[string]any
		if err := json.Unmarshal(body, &invocation); err != nil {
			t.Errorf("body is not JSON: %s", body)
		}
		if invocation["taskId"] != testTaskID {
			t.Errorf("taskId = %v, want %s", invocation["taskId"], testTaskID)
		}
		payload, _ := invocation["payload"].(map[string]any)
		if payload["prompt"] != "hi" {
			t.Errorf("payload = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(testTaskJSON))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	task, err := c.InvokeAsync(context.Background(), testAppID, testEndpoint, testTaskID, TaskPayload{"prompt": "hi"})
	if err != nil {
		t.Fatalf("InvokeAsync: %v", err)
	}
	if task.Id != testTaskID || task.Status != TaskStatusPending {
		t.Fatalf("unexpected task: %+v", task)
	}
	if posts.Load() != 1 {
		t.Fatalf("expected 1 POST, got %d", posts.Load())
	}
}

func TestInvokeAsync_RejectsLeadingSlash(t *testing.T) {
	c := NewClient("test-key", "https://example.invalid", slog.Default())
	_, err := c.InvokeAsync(context.Background(), testAppID, "/infer", "", nil)
	if err == nil || !strings.Contains(err.Error(), "bare segment") {
		t.Fatalf("expected leading-slash error, got %v", err)
	}
}

func TestInvokeAsync_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.InvokeAsync(context.Background(), testAppID, testEndpoint, "", nil); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestInvokeSync_Completed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/invoke-sync/" + testEndpoint
		if r.Method != http.MethodPost || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testTaskDone))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	task, err := c.InvokeSync(context.Background(), testAppID, testEndpoint, "", nil)
	if err != nil {
		t.Fatalf("InvokeSync: %v", err)
	}
	if task.Status != TaskStatusCompleted || task.Output == nil || (*task.Output)["ok"] != true {
		t.Fatalf("unexpected task: %+v", task)
	}
}

func TestInvokeAsync_GeneratesTaskID(t *testing.T) {
	var gotID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var invocation map[string]any
		if err := json.Unmarshal(body, &invocation); err != nil {
			t.Errorf("body is not JSON: %s", body)
		}
		gotID, _ = invocation["taskId"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(testTaskJSON))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	if _, err := c.InvokeAsync(context.Background(), testAppID, testEndpoint, "", nil); err != nil {
		t.Fatalf("InvokeAsync: %v", err)
	}
	if _, err := uuid.Parse(gotID); err != nil {
		t.Fatalf("generated taskId %q is not a UUID: %v", gotID, err)
	}
}

func TestInvokeSync_WaitExpiry202(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(testTaskJSON))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	task, err := c.InvokeSync(context.Background(), testAppID, testEndpoint, "", nil)
	if err != nil {
		t.Fatalf("InvokeSync must not treat 202 as failure: %v", err)
	}
	if task.Id != testTaskID || task.Status != TaskStatusPending {
		t.Fatalf("unexpected task: %+v", task)
	}
}

func TestGetTask_InvalidID(t *testing.T) {
	c := NewClient("test-key", "https://example.invalid", slog.Default())
	_, err := c.GetTask(context.Background(), testAppID, "not-a-uuid")
	if err == nil || !strings.Contains(err.Error(), "invalid task id") {
		t.Fatalf("expected invalid task id error, got %v", err)
	}
}

func TestInvokeAsync_InvalidTaskID(t *testing.T) {
	c := NewClient("test-key", "https://example.invalid", slog.Default())
	_, err := c.InvokeAsync(context.Background(), testAppID, testEndpoint, "not-a-uuid", nil)
	if err == nil || !strings.Contains(err.Error(), "invalid task id") {
		t.Fatalf("expected invalid task id error, got %v", err)
	}
}

func TestGetTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/tasks/" + testTaskID
		if r.Method != http.MethodGet || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testTaskDone))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	task, err := c.GetTask(context.Background(), testAppID, testTaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != TaskStatusCompleted {
		t.Fatalf("unexpected task: %+v", task)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"No task exists"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.GetTask(context.Background(), testAppID, testTaskID)
	var re *transport.RunwareError
	if !errors.As(err, &re) || re.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 RunwareError, got %v", err)
	}
}

func TestListTasks_CursorAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/tasks"
		if r.Method != http.MethodGet || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Errorf("limit query = %q, want 10", got)
		}
		if got := r.URL.Query().Get("cursor"); got != testCursorPage2 {
			t.Errorf("cursor query = %q, want %s", got, testCursorPage2)
		}
		if got := r.URL.Query().Get("status"); got != "pending" {
			t.Errorf("status query = %q, want pending", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[` + testTaskJSON + `],"nextCursor":"` + testCursorPage3 + `"}`))
	}))
	defer srv.Close()

	limit := Limit(10)
	cursor := Cursor(testCursorPage2)
	status := TaskStatusPending
	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListTasks(context.Background(), testAppID, &ListTasksParams{
		Limit:  &limit,
		Cursor: &cursor,
		Status: &status,
	})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].Id != testTaskID {
		t.Fatalf("unexpected tasks: %+v", page.Data)
	}
	if page.NextCursor == nil || *page.NextCursor != testCursorPage3 {
		t.Fatalf("unexpected nextCursor: %+v", page.NextCursor)
	}
}

func TestListTasks_EmptyPageKeepsCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"nextCursor":"` + testCursorPage2 + `"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListTasks(context.Background(), testAppID, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(page.Data) != 0 {
		t.Fatalf("expected empty data, got %+v", page.Data)
	}
	if page.NextCursor == nil || *page.NextCursor != testCursorPage2 {
		t.Fatalf("unexpected nextCursor: %+v", page.NextCursor)
	}
}

func TestListTasks_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.ListTasks(context.Background(), testAppID, nil); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestWaitTask_PollsUntilCompleted(t *testing.T) {
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps/"+testAppID+"/tasks/"+testTaskID {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		n := gets.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			_, _ = w.Write([]byte(testTaskJSON))
			return
		}
		_, _ = w.Write([]byte(testTaskDone))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	task, err := c.WaitTask(context.Background(), testAppID, testTaskID, time.Millisecond)
	if err != nil {
		t.Fatalf("WaitTask: %v", err)
	}
	if task.Status != TaskStatusCompleted {
		t.Fatalf("unexpected task: %+v", task)
	}
	if gets.Load() < 2 {
		t.Fatalf("expected at least 2 GETs, got %d", gets.Load())
	}
}

func TestWaitTask_RetriesTransient404(t *testing.T) {
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := gets.Add(1)
		if n == 1 {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testTaskFailed))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	task, err := c.WaitTask(context.Background(), testAppID, testTaskID, time.Millisecond)
	if err != nil {
		t.Fatalf("WaitTask: %v", err)
	}
	if task.Status != TaskStatusFailed || task.Error == nil || *task.Error != "oom killed" {
		t.Fatalf("unexpected task: %+v", task)
	}
}

func TestWaitTask_DoesNotPost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("WaitTask must not %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testTaskDone))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	if _, err := c.WaitTask(context.Background(), testAppID, testTaskID, time.Millisecond); err != nil {
		t.Fatalf("WaitTask: %v", err)
	}
}
