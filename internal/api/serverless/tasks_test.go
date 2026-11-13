package serverless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/api/transport"
	"gopkg.in/yaml.v3"
)

const (
	testTaskID     = "7c9e6679-7425-40de-944b-e07fc1f90ae7"
	testEndpoint   = "infer"
	testNested     = "v1/chat/completions"
	testTaskJSON   = `{"id":"7c9e6679-7425-40de-944b-e07fc1f90ae7","status":"pending","appId":"my-app","endpointPath":"infer","createdAt":"2026-07-30T12:00:00Z"}`
	testTaskDone   = `{"id":"7c9e6679-7425-40de-944b-e07fc1f90ae7","status":"completed","appId":"my-app","endpointPath":"infer","createdAt":"2026-07-30T12:00:00Z","completedAt":"2026-07-30T12:00:05Z","output":{"ok":true}}`
	testTaskFailed = `{"id":"7c9e6679-7425-40de-944b-e07fc1f90ae7","status":"failed","appId":"my-app","endpointPath":"infer","createdAt":"2026-07-30T12:00:00Z","error":"oom killed"}`
)

func TestValidateEndpointPath(t *testing.T) {
	const wantInvalid = "invalid"

	cases := []struct {
		path    string
		wantErr string
	}{
		{path: "infer"},
		{path: "a"},
		{path: "my-endpoint"},
		{path: testNested},
		{path: "v1/models"},
		{path: "a/b/c/d/e"},
		{path: strings.Repeat("ab/", 84) + "ab"},
		{path: "", wantErr: "required"},
		{path: "/infer", wantErr: "leading slash"},
		{path: "/infer", wantErr: `"infer"`},
		{path: "/" + testNested, wantErr: `"` + testNested + `"`},
		{path: "Infer", wantErr: wantInvalid},
		{path: "my_endpoint", wantErr: wantInvalid},
		{path: "infer/", wantErr: wantInvalid},
		{path: "v1//completions", wantErr: wantInvalid},
		{path: "v1/Chat", wantErr: wantInvalid},
		{path: testNested + "/", wantErr: wantInvalid},
		{path: strings.Repeat("ab/", 85) + "ab", wantErr: "over the 255"},
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
	if err == nil || !strings.Contains(err.Error(), "leading slash") {
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
	output, ok := task.Output.(map[string]any)
	if task.Status != TaskStatusCompleted || !ok || output["ok"] != true {
		t.Fatalf("unexpected task: %+v", task)
	}
}

func TestInvokeAsync_CompletedDuplicateTaskAcceptsAnyJSONOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
	}{
		{name: "scalar", output: `"done"`},
		{name: "list", output: `["done",1,false]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"id":"` + testTaskID + `","status":"completed","appId":"my-app","endpointPath":"infer","createdAt":"2026-07-30T12:00:00Z","output":` + tc.output + `}`
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
			task, err := c.InvokeAsync(context.Background(), testAppID, testEndpoint, testTaskID, nil)
			if err != nil {
				t.Fatalf("InvokeAsync: %v", err)
			}
			if task.Output == nil {
				t.Fatal("expected output to be decoded")
			}
		})
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

// invokeMethod is one of the two invoke operations under test. okStatus is the
// status its success answers with.
type invokeMethod struct {
	name     string
	invoke   func(c *Client, ctx context.Context, appID, endpointPath, taskID string, body TaskPayload) (*Task, error)
	okStatus int
}

var invokeMethods = []invokeMethod{
	{
		name:     "sync",
		invoke:   (*Client).InvokeSync,
		okStatus: http.StatusOK,
	},
	{
		name:     "async",
		invoke:   (*Client).InvokeAsync,
		okStatus: http.StatusAccepted,
	},
}

// invokeAnswer is how the stub API answers an invocation. okStatus is the
// status a success carries for the method under test.
type invokeAnswer func(w http.ResponseWriter, okStatus int)

type invokeAnswerCase struct {
	name   string
	answer invokeAnswer
}

// answerHTML answers status with an HTML page, as a proxy in front of the API does.
func answerHTML(status int) invokeAnswer {
	return func(w http.ResponseWriter, _ int) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, "<html>error code: %d</html>", status)
	}
}

// answerProblem answers status with the API's own problem document.
func answerProblem(status int, problemType string) invokeAnswer {
	return func(w http.ResponseWriter, _ int) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"type":%q,"title":%q,"status":%d}`, problemType, http.StatusText(status), status)
	}
}

// answerDropped closes the connection before any response is written.
func answerDropped(w http.ResponseWriter, _ int) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		panic(err)
	}
	_ = conn.Close()
}

// answerCutShort starts a success and aborts it mid-body, so reading the
// body fails as it does when the connection drops during the transfer.
func answerCutShort(w http.ResponseWriter, okStatus int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(testTaskDone)))
	w.WriteHeader(okStatus)
	_, _ = w.Write([]byte(testTaskDone[:len(testTaskDone)/2]))
	w.(http.Flusher).Flush()
	panic(http.ErrAbortHandler)
}

// answerMalformed answers a success whose JSON body does not parse.
func answerMalformed(w http.ResponseWriter, okStatus int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(okStatus)
	_, _ = w.Write([]byte(`{"id":`))
}

// answerNotJSON answers a success that carries no JSON body.
func answerNotJSON(w http.ResponseWriter, okStatus int) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(okStatus)
	_, _ = w.Write([]byte("ok"))
}

// answerBody answers status with body under contentType.
func answerBody(status int, contentType, body string) invokeAnswer {
	return func(w http.ResponseWriter, _ int) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// answerProblemCutShort starts a problem document for status and aborts it
// mid-body, so reading the body fails.
func answerProblemCutShort(status int) invokeAnswer {
	return func(w http.ResponseWriter, _ int) {
		body := fmt.Sprintf(`{"type":"about:blank","title":%q,"status":%d}`, http.StatusText(status), status)
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body[:len(body)/2]))
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}
}

// lostAnswers leave unknown whether the API accepted the task.
var lostAnswers = []invokeAnswerCase{
	{
		name:   "proxy 520",
		answer: answerHTML(520),
	},
	{
		name:   "proxy 524",
		answer: answerHTML(524),
	},
	{
		name:   "proxy 503",
		answer: answerHTML(http.StatusServiceUnavailable),
	},
	{
		name:   "proxy JSON 503",
		answer: answerBody(http.StatusServiceUnavailable, "application/json", `{"error":"upstream unavailable"}`),
	},
	{
		name:   "proxy JSON 503 with a status",
		answer: answerBody(http.StatusServiceUnavailable, "application/json", `{"status":503,"error":"Service Unavailable"}`),
	},
	{
		name:   "proxy problem 503",
		answer: answerBody(http.StatusServiceUnavailable, "application/problem+json", `{"type":"about:blank","title":"Service Unavailable","status":503}`),
	},
	// A refusal type alone does not make the API's 503: its title and its
	// status are checked on their own.
	{
		name:   "capacity unavailable 503 without a title",
		answer: answerBody(http.StatusServiceUnavailable, "application/problem+json", `{"type":"`+capacityUnavailableType+`","status":503}`),
	},
	{
		name:   "capacity unavailable 503 without a status",
		answer: answerBody(http.StatusServiceUnavailable, "application/problem+json", `{"type":"`+capacityUnavailableType+`","title":"Service Unavailable"}`),
	},
	{
		name:   "service unavailable 503 naming another status",
		answer: answerBody(http.StatusServiceUnavailable, "application/problem+json", `{"type":"`+serviceUnavailableType+`","title":"Service Unavailable","status":500}`),
	},
	{
		name:   "api 502 body malformed",
		answer: answerBody(http.StatusBadGateway, "application/problem+json", `{"type":`),
	},
	{
		name:   "api 502 body cut short",
		answer: answerProblemCutShort(http.StatusBadGateway),
	},
	{
		name:   "api 500",
		answer: answerProblem(http.StatusInternalServerError, "https://docs.runware.ai/serverless/errors#internal-server-error"),
	},
	{
		name:   "api 502",
		answer: answerProblem(http.StatusBadGateway, "https://docs.runware.ai/serverless/errors#bad-gateway"),
	},
	{
		name:   "connection dropped",
		answer: answerDropped,
	},
	{
		name:   "body cut short",
		answer: answerCutShort,
	},
	{
		name:   "body malformed",
		answer: answerMalformed,
	},
	{
		name:   "body not JSON",
		answer: answerNotJSON,
	},
	{
		// A sync invoke's 202 is the wait window expiring; its task must
		// still be read back, or the id it was sent under recovered.
		name: "accepted body not JSON",
		answer: func(w http.ResponseWriter, _ int) {
			answerNotJSON(w, http.StatusAccepted)
		},
	},
}

// refusedAnswers say the API did not accept the task.
var refusedAnswers = []invokeAnswerCase{
	{
		name:   "capacity unavailable",
		answer: answerProblem(http.StatusServiceUnavailable, "https://docs.runware.ai/serverless/errors#capacity-unavailable"),
	},
	{
		name:   "service unavailable",
		answer: answerProblem(http.StatusServiceUnavailable, "https://docs.runware.ai/serverless/errors#service-unavailable"),
	},
	{
		name:   "bad request",
		answer: answerProblem(http.StatusBadRequest, "https://docs.runware.ai/serverless/errors#bad-request"),
	},
	{
		name:   "not found",
		answer: answerProblem(http.StatusNotFound, "https://docs.runware.ai/serverless/errors#not-found"),
	},
	{
		name:   "conflict",
		answer: answerProblem(http.StatusConflict, "https://docs.runware.ai/serverless/errors#conflict"),
	},
	{
		name:   "environment paused",
		answer: answerProblem(http.StatusTooManyRequests, "https://docs.runware.ai/serverless/errors#environment-paused"),
	},
	{
		name:   "bad request body malformed",
		answer: answerBody(http.StatusBadRequest, "application/problem+json", `{"type":`),
	},
	{
		name:   "unauthorized body cut short",
		answer: answerProblemCutShort(http.StatusUnauthorized),
	},
	{
		// The last client error: a status the operation does not bind is
		// still a refusal below 500.
		name:   "client closed request 499",
		answer: answerBody(499, "application/problem+json", `{"type":"about:blank","title":"Client Closed Request","status":499}`),
	},
}

// invokeAgainst invokes testTaskID with m against a stub API that answers with answer.
func invokeAgainst(t *testing.T, m invokeMethod, answer invokeAnswer) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		answer(w, m.okStatus)
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := m.invoke(c, context.Background(), testAppID, testEndpoint, testTaskID, nil)
	return err
}

func TestInvoke_LostResponseNamesTheTaskSent(t *testing.T) {
	for _, m := range invokeMethods {
		for _, tc := range lostAnswers {
			t.Run(m.name+"/"+tc.name, func(t *testing.T) {
				err := invokeAgainst(t, m, tc.answer)
				lost, ok := errors.AsType[*ResponseLostError](err)
				if !ok {
					t.Fatalf("err = %v, want a *ResponseLostError", err)
				}
				if lost.TaskID != testTaskID {
					t.Fatalf("TaskID = %q, want %q", lost.TaskID, testTaskID)
				}
				if !strings.Contains(err.Error(), testTaskID) {
					t.Fatalf("err = %q, want it to name the task", err)
				}
			})
		}
	}
}

func TestInvoke_RefusalIsNotALostResponse(t *testing.T) {
	for _, m := range invokeMethods {
		for _, tc := range refusedAnswers {
			t.Run(m.name+"/"+tc.name, func(t *testing.T) {
				err := invokeAgainst(t, m, tc.answer)
				if err == nil {
					t.Fatal("expected an error")
				}
				if _, ok := errors.AsType[*ResponseLostError](err); ok {
					t.Fatalf("err = %v, want a refusal, not a lost response", err)
				}
			})
		}
	}
}

func TestInvoke_LostResponseNamesAGeneratedTaskID(t *testing.T) {
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var invocation map[string]any
		if err := json.NewDecoder(r.Body).Decode(&invocation); err != nil {
			t.Errorf("decode body: %v", err)
		}
		sent, _ = invocation["taskId"].(string)
		answerHTML(520)(w, http.StatusOK)
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.InvokeSync(context.Background(), testAppID, testEndpoint, "", nil)
	lost, ok := errors.AsType[*ResponseLostError](err)
	if !ok {
		t.Fatalf("err = %v, want a *ResponseLostError", err)
	}
	if sent == "" || lost.TaskID != sent {
		t.Fatalf("TaskID = %q, want the id sent, %q", lost.TaskID, sent)
	}
}

func TestInvoke_LostResponseKeepsTheAPIProblem(t *testing.T) {
	const badGateway = "https://docs.runware.ai/serverless/errors#bad-gateway"
	err := invokeAgainst(t, invokeMethods[0], answerProblem(http.StatusBadGateway, badGateway))
	if _, ok := errors.AsType[*ResponseLostError](err); !ok {
		t.Fatalf("err = %v, want a *ResponseLostError", err)
	}
	re, ok := errors.AsType[*transport.RunwareError](err)
	if !ok {
		t.Fatalf("err = %v, want the API problem inside", err)
	}
	if re.StatusCode != http.StatusBadGateway || re.ProblemType != badGateway {
		t.Fatalf("status = %d, problem type = %q; want 502, %q", re.StatusCode, re.ProblemType, badGateway)
	}
}

// unreadableStatusCase is a status answered with a body that does not parse,
// and whether the invocation it answers is lost rather than refused.
type unreadableStatusCase struct {
	status int
	lost   bool
}

func TestInvoke_UnreadableResponseKeepsItsStatus(t *testing.T) {
	cases := []unreadableStatusCase{
		{
			status: http.StatusBadRequest,
			lost:   false,
		},
		{
			status: http.StatusUnauthorized,
			lost:   false,
		},
		{
			// The first server error: the boundary with the refusals.
			status: http.StatusInternalServerError,
			lost:   true,
		},
		{
			status: http.StatusBadGateway,
			lost:   true,
		},
	}
	for _, m := range invokeMethods {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/%d", m.name, tc.status), func(t *testing.T) {
				err := invokeAgainst(t, m, answerBody(tc.status, "application/problem+json", `{"type":`))
				lostErr, lost := errors.AsType[*ResponseLostError](err)
				if lost != tc.lost {
					t.Fatalf("err = %v, lost = %t; want lost = %t", err, lost, tc.lost)
				}
				if lost && lostErr.TaskID != testTaskID {
					t.Fatalf("TaskID = %q, want the id sent, %q", lostErr.TaskID, testTaskID)
				}
				re, ok := errors.AsType[*transport.RunwareError](err)
				if !ok {
					t.Fatalf("err = %v, want a *transport.RunwareError", err)
				}
				if re.StatusCode != tc.status {
					t.Fatalf("status = %d, want %d", re.StatusCode, tc.status)
				}
			})
		}
	}
}

func TestProblemToError_NamesAStatusWithoutText(t *testing.T) {
	if got := problemToError(nil, 520).Error(); got != "HTTP 520" {
		t.Fatalf("message = %q, want %q", got, "HTTP 520")
	}
	if got := problemToError(nil, http.StatusBadGateway).Error(); got != "HTTP 502: Bad Gateway" {
		t.Fatalf("message = %q, want %q", got, "HTTP 502: Bad Gateway")
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
	page, err := c.ListTasks(context.Background(), testAppID, ListTasksParams{
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

func TestListTasks_AcceptsScalarOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"` + testTaskID + `","status":"completed","appId":"my-app","endpointPath":"infer","createdAt":"2026-07-30T12:00:00Z","output":"done"}]}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListTasks(context.Background(), testAppID, ListTasksParams{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].Output != "done" {
		t.Fatalf("unexpected tasks: %+v", page.Data)
	}
}

func TestListTasks_EmptyPageKeepsCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"nextCursor":"` + testCursorPage2 + `"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListTasks(context.Background(), testAppID, ListTasksParams{})
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
	if _, err := c.ListTasks(context.Background(), testAppID, ListTasksParams{}); !errors.Is(err, transport.ErrNoAPIKey) {
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

func TestEndpointPathEditorRestoresTheSeparators(t *testing.T) {
	cases := []struct {
		name         string
		endpointPath string
		url          string
		want         string
	}{
		{
			name:         "a nested path",
			endpointPath: testNested,
			url:          "https://example.invalid/v1/apps/my-app/invoke-sync/v1%2Fchat%2Fcompletions",
			want:         "/v1/apps/my-app/invoke-sync/v1/chat/completions",
		},
		{
			name:         "escaped in lower case",
			endpointPath: testNested,
			url:          "https://example.invalid/v1/apps/my-app/invoke-async/v1%2fchat%2fcompletions",
			want:         "/v1/apps/my-app/invoke-async/v1/chat/completions",
		},
		{
			name:         "one segment",
			endpointPath: "infer",
			url:          "https://example.invalid/v1/apps/my-app/invoke-sync/infer",
			want:         "/v1/apps/my-app/invoke-sync/infer",
		},
		{
			name:         "an escaped slash in the base url is left alone",
			endpointPath: testNested,
			url:          "https://proxy.invalid/tenant%2Fname/v1/apps/my-app/invoke-sync/v1%2Fchat%2Fcompletions",
			want:         "/tenant%2Fname/v1/apps/my-app/invoke-sync/v1/chat/completions",
		},
		{
			// The editor is given to the two operations that carry an endpoint
			// path, and rewrites only that parameter. `invoke-sync` is a valid
			// app id, so a route recognized by its text alone would rewrite the
			// escaped secret name on this request.
			name:         "another operation on an app named like the route",
			endpointPath: testNested,
			url:          "https://example.invalid/v1/apps/invoke-sync/secrets/a%2Fb",
			want:         "/v1/apps/invoke-sync/secrets/a%2Fb",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, tc.url, nil)
			if err != nil {
				t.Fatalf("NewRequest(%q): %v", tc.url, err)
			}
			if err := endpointPathEditor(tc.endpointPath)(context.Background(), req); err != nil {
				t.Fatalf("endpointPathEditor(%q) on %q: %v", tc.endpointPath, tc.url, err)
			}
			if got := req.URL.EscapedPath(); got != tc.want {
				t.Errorf("EscapedPath for %q = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

// endpointPathSchema is the EndpointPath rule the vendored API spec states.
type endpointPathSchema struct {
	Pattern   string `yaml:"pattern"`
	MaxLength int    `yaml:"maxLength"`
}

type specComponents struct {
	Schemas map[string]endpointPathSchema `yaml:"schemas"`
}

type specDocument struct {
	Components specComponents `yaml:"components"`
}

// TestEndpointPathRuleMatchesTheSpec keeps the copy above honest. The bug this
// file fixes was a stale copy of this rule, so a spec change that no table row
// covers has to fail here.
func TestEndpointPathRuleMatchesTheSpec(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "serverless", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var spec specDocument
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}

	schema, ok := spec.Components.Schemas["EndpointPath"]
	if !ok {
		t.Fatal("the spec has no EndpointPath schema")
	}
	if schema.Pattern == "" {
		t.Fatal("the spec has no EndpointPath pattern")
	}
	if got := endpointPathPattern.String(); got != schema.Pattern {
		t.Errorf("endpointPathPattern = %q, the spec says %q", got, schema.Pattern)
	}
	if schema.MaxLength != endpointPathMaxLen {
		t.Errorf("endpointPathMaxLen = %d, the spec says %d", endpointPathMaxLen, schema.MaxLength)
	}
}

// TestInvokeSendsANestedPathUnescaped drives both operations through the
// generated client, which is where the escaping happens. The editor is attached
// per operation, so a call that stops passing it fails here and nowhere else.
func TestInvokeSendsANestedPathUnescaped(t *testing.T) {
	cases := []struct {
		name   string
		verb   string
		invoke func(*Client) error
	}{
		{
			name: "sync",
			verb: "invoke-sync",
			invoke: func(c *Client) error {
				_, err := c.InvokeSync(context.Background(), testAppID, testNested, testTaskID, nil)
				return err
			},
		},
		{
			name: "async",
			verb: "invoke-async",
			invoke: func(c *Client) error {
				_, err := c.InvokeAsync(context.Background(), testAppID, testNested, testTaskID, nil)
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := "/v1/apps/" + testAppID + "/" + tc.verb + "/" + testNested
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.EscapedPath()
				w.Header().Set("Content-Type", "application/json")
				if tc.verb == "invoke-async" {
					w.WriteHeader(http.StatusAccepted)
				}
				_, _ = w.Write([]byte(testTaskDone))
			}))
			defer srv.Close()

			c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
			if err := tc.invoke(c); err != nil {
				t.Fatalf("invoke: %v", err)
			}
			if got != want {
				t.Errorf("the server saw %q, want %q", got, want)
			}
		})
	}
}
