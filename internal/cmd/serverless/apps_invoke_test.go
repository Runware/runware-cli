package serverless

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
)

func TestParseInvokeInput_RejectsLeadingSlash(t *testing.T) {
	_, _, err := parseInvokeInput("/infer", "", strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "leading slash") {
		t.Fatalf("expected leading-slash error, got %v", err)
	}
}

func TestParseInvokeInput_AcceptsNestedPath(t *testing.T) {
	path, _, err := parseInvokeInput("v1/chat/completions", "", strings.NewReader(""))
	if err != nil {
		t.Fatalf("parseInvokeInput: %v", err)
	}
	if path != "v1/chat/completions" {
		t.Fatalf("path = %q", path)
	}
}

func TestParseInvokeInput_DefaultEmptyObject(t *testing.T) {
	path, payload, err := parseInvokeInput("infer", "", strings.NewReader(""))
	if err != nil {
		t.Fatalf("parseInvokeInput: %v", err)
	}
	if path != "infer" {
		t.Fatalf("path = %q", path)
	}
	if payload == nil || len(payload) != 0 {
		t.Fatalf("expected empty object, got %#v", payload)
	}
}

func TestReadTaskPayload_FromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(path, []byte("{\"prompt\":\"hi\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := readTaskPayload(path, strings.NewReader(""))
	if err != nil {
		t.Fatalf("readTaskPayload: %v", err)
	}
	if payload["prompt"] != "hi" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestReadTaskPayload_FromStdin(t *testing.T) {
	payload, err := readTaskPayload("-", strings.NewReader(`{"n":1}`))
	if err != nil {
		t.Fatalf("readTaskPayload: %v", err)
	}
	if payload["n"] != float64(1) {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestReadTaskPayload_InvalidJSON(t *testing.T) {
	_, err := readTaskPayload("-", strings.NewReader("not-json"))
	if err == nil || !strings.Contains(err.Error(), "parse body JSON") {
		t.Fatalf("expected JSON error, got %v", err)
	}
}

func TestInvokeWaitMessage(t *testing.T) {
	depth := int64(4)
	zero := int64(0)
	const taskID = "7c9e6679-7425-40de-944b-e07fc1f90ae7"
	again := "To wait on it again, run the same command with --sync or --wait and --task-id " + taskID + "."
	cases := []struct {
		name  string
		depth *int64
		want  string
	}{
		{
			name: "unknown depth",
			want: "Task " + taskID + " is pending (queued or running). Polling it. " + again,
		},
		{
			name:  "known depth",
			depth: &depth,
			want:  "Task " + taskID + " is pending (queued or running). Polling it. App queue depth: 4. " + again,
		},
		{
			name:  "empty queue",
			depth: &zero,
			want:  "Task " + taskID + " is pending (queued or running). Polling it. App queue depth: 0. " + again,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := invokeWaitMessage(taskID, tc.depth); got != tc.want {
				t.Fatalf("invokeWaitMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}

// lostInvokeStub answers every invocation with a Cloudflare 520 and serves
// the task the invocation carried as completed, as the platform does when the
// worker finished but the response never reached the caller.
type lostInvokeStub struct {
	mu   sync.Mutex
	sent string
	// pending serves the task as still running, so polling never ends.
	pending bool
	// hold keeps the invocation open until the client gives up on it, in
	// place of the 520.
	hold bool
}

func (s *lostInvokeStub) taskID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent
}

func (s *lostInvokeStub) serve(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			var invocation map[string]any
			if err := json.NewDecoder(r.Body).Decode(&invocation); err != nil {
				t.Errorf("decode invocation: %v", err)
			}
			id, _ := invocation["taskId"].(string)
			s.mu.Lock()
			s.sent = id
			s.mu.Unlock()
			if s.hold {
				<-r.Context().Done()
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(520)
			_, _ = io.WriteString(w, "<html>error code: 520</html>")
		case r.Method == http.MethodGet && s.taskID() != "" && r.URL.Path == testAppsPath+"/"+testAppID+"/tasks/"+s.taskID():
			w.Header().Set("Content-Type", "application/json")
			if s.pending {
				_, _ = fmt.Fprintf(w, `{"id":%q,"status":"pending","appId":%q,"endpointPath":%q,"createdAt":"2026-11-09T08:00:00Z"}`, s.taskID(), testAppID, testInferEndpoint)
				return
			}
			_, _ = fmt.Fprintf(w, `{"id":%q,"status":"completed","appId":%q,"endpointPath":%q,"createdAt":"2026-11-09T08:00:00Z","completedAt":"2026-11-09T08:01:00Z","output":{"ok":true}}`, s.taskID(), testAppID, testInferEndpoint)
		default:
			writeNotFound(w)
		}
	}
}

// countRequests counts the recorded requests with method whose path has prefix.
func countRequests(requests []recordedRequest, method, prefix string) int {
	n := 0
	for _, r := range requests {
		if r.method == method && strings.HasPrefix(r.path, prefix) {
			n++
		}
	}
	return n
}

func TestInvoke_PollsTheTaskWhenTheResponseIsLost(t *testing.T) {
	for _, format := range []output.Format{"", output.FormatJSON} {
		for _, waitFlag := range []string{"--sync", "--wait"} {
			t.Run(waitFlag+"/"+string(cmp.Or(format, output.FormatTable)), func(t *testing.T) {
				stub := &lostInvokeStub{}
				run := runServerlessFormatted(t, stub.serve(t), strings.NewReader(""), format,
					testAppsGroup, testInvokeCommand, testAppID, testInferEndpoint, waitFlag, "--poll-interval", "10ms")
				if run.err != nil {
					t.Fatalf("err = %v, want the completed task read by id", run.err)
				}
				id := stub.taskID()
				if format == output.FormatJSON {
					if run.cobraOutput != "" {
						t.Fatalf("stderr = %q, want nothing beside json output", run.cobraOutput)
					}
					var task map[string]any
					if err := json.Unmarshal([]byte(run.stdout), &task); err != nil || task["id"] != id || task["status"] != string(serverlessapi.TaskStatusCompleted) {
						t.Fatalf("stdout = %q (%v), want completed task %s as json", run.stdout, err, id)
					}
				} else {
					if !strings.Contains(run.cobraOutput, "Task "+id+" may have started") || !strings.Contains(run.cobraOutput, "Polling it") {
						t.Fatalf("stderr = %q, want it to name task %s and say it polls it", run.cobraOutput, id)
					}
					if !strings.Contains(run.stdout, id) || !strings.Contains(run.stdout, string(serverlessapi.TaskStatusCompleted)) {
						t.Fatalf("stdout = %q, want completed task %s", run.stdout, id)
					}
				}
				if posts := countRequests(run.requests, http.MethodPost, testAppsPath); posts != 1 {
					t.Fatalf("requests = %v, want one invocation and no resubmit", run.requests)
				}
				if gets := countRequests(run.requests, http.MethodGet, testAppsPath+"/"+testAppID+"/tasks/"+id); gets == 0 {
					t.Fatalf("requests = %v, want task %s read by id", run.requests, id)
				}
			})
		}
	}
}

// structuredError is err as the CLI prints it with --format json.
func structuredError(t *testing.T, err error) map[string]any {
	t.Helper()
	var out bytes.Buffer
	cmdutil.PrintErrorTo(log.New(io.Discard), &out, output.FormatJSON, err)
	var got errorOutput
	if jsonErr := json.Unmarshal(out.Bytes(), &got); jsonErr != nil || len(got.Errors) != 1 {
		t.Fatalf("json error output %q: %v", out.String(), jsonErr)
	}
	return got.Errors[0]
}

func TestInvoke_ReportsALostResponseAsStructuredError(t *testing.T) {
	stub := &lostInvokeStub{}
	run := runServerlessFormatted(t, stub.serve(t), strings.NewReader(""), output.FormatJSON,
		testAppsGroup, testInvokeCommand, testAppID, testInferEndpoint, testEnvFlag, testEnvironment)
	id := stub.taskID()
	if id == "" {
		t.Fatal("no invocation reached the stub")
	}
	if run.cobraOutput != "" {
		t.Fatalf("stderr = %q, want only the structured error the CLI prints", run.cobraOutput)
	}
	if gets := countRequests(run.requests, http.MethodGet, testAppsPath+"/"+testAppID+"/tasks/"); gets != 0 {
		t.Fatalf("requests = %v, want no task poll without --wait or --sync", run.requests)
	}

	item := structuredError(t, run.err)
	if item["code"] != lostResponseCode || item["taskUUID"] != id {
		t.Fatalf("json error = %v, want code %s and taskUUID %s", item, lostResponseCode, id)
	}
	msg, _ := item["message"].(string)
	showCmd := "runware serverless apps tasks show " + testAppID + " " + id + " " + testStagingHint
	for _, want := range []string{"HTTP 520", showCmd, "--task-id " + id} {
		if !strings.Contains(msg, want) {
			t.Errorf("message = %q, want %q", msg, want)
		}
	}

	var text bytes.Buffer
	cmdutil.PrintErrorTo(log.New(&text), io.Discard, output.FormatTable, run.err)
	for _, want := range []string{"code=" + lostResponseCode, "taskUUID=" + id, showCmd} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text error = %q, want %q", text.String(), want)
		}
	}
}

func TestInvoke_ReportsAFailedPollAfterALostResponse(t *testing.T) {
	stub := &lostInvokeStub{
		pending: true,
	}
	run := runServerlessAgainst(t, stub.serve(t),
		testAppsGroup, testInvokeCommand, testAppID, testInferEndpoint, "--sync", "--timeout", "200ms", "--poll-interval", "10ms")
	id := stub.taskID()
	item := structuredError(t, run.err)
	if item["code"] != lostResponseCode || item["taskUUID"] != id {
		t.Fatalf("json error = %v, want code %s and taskUUID %s", item, lostResponseCode, id)
	}
	msg, _ := item["message"].(string)
	if want := "polling it failed: timed out waiting for task " + id + " after 200ms"; !strings.Contains(msg, want) {
		t.Fatalf("message = %q, want %q", msg, want)
	}
}

// TestInvoke_ReportsATimeoutDuringTheCallAsALostResponse pins a --timeout
// that runs out while the sync call itself is in flight: the task may have
// started, so the error names it, and with no wait budget left the command
// reports it rather than polling.
func TestInvoke_ReportsATimeoutDuringTheCallAsALostResponse(t *testing.T) {
	stub := &lostInvokeStub{
		hold: true,
	}
	run := runServerlessAgainst(t, stub.serve(t),
		testAppsGroup, testInvokeCommand, testAppID, testInferEndpoint, "--sync", "--timeout", "100ms", "--poll-interval", "10ms")
	id := stub.taskID()
	if id == "" {
		t.Fatal("no invocation reached the stub")
	}
	item := structuredError(t, run.err)
	if item["code"] != lostResponseCode || item["taskUUID"] != id {
		t.Fatalf("json error = %v, want code %s and taskUUID %s", item, lostResponseCode, id)
	}
	msg, _ := item["message"].(string)
	if want := "no response confirmed it: timed out waiting for sync invocation after 100ms"; !strings.Contains(msg, want) {
		t.Fatalf("message = %q, want %q", msg, want)
	}
	if strings.Contains(msg, "polling it failed") || strings.Contains(run.cobraOutput, "Polling it") {
		t.Fatalf("message = %q, stderr = %q; want no poll once the wait budget is spent", msg, run.cobraOutput)
	}
	if gets := countRequests(run.requests, http.MethodGet, testAppsPath+"/"+testAppID+"/tasks/"); gets != 0 {
		t.Fatalf("requests = %v, want no task poll once the wait budget is spent", run.requests)
	}
}

func TestLostInvokeError(t *testing.T) {
	const show = "runware serverless apps tasks show my-app " + testUUID
	const badGateway = "https://docs.runware.ai/serverless/errors#bad-gateway"
	apiErr := transport.CreateRunwareError("internalServerError", "an upstream service was unreachable", transport.RunwareErrorDetails{
		StatusCode: http.StatusBadGateway,
	})
	apiErr.ProblemType = badGateway
	err := newLostInvokeError(testUUID, show, apiErr, nil)

	want := "task " + testUUID + " may have started, but no response confirmed it: an upstream service was unreachable\n" +
		"Read it with: " + show + "\n" +
		"Or run the same invoke with --task-id " + testUUID + ": it returns that task instead of starting a second run."
	if err.Error() != want {
		t.Fatalf("message =\n%s\nwant\n%s", err, want)
	}
	re, ok := errors.AsType[*transport.RunwareError](err)
	if !ok || re.RawCode != lostResponseCode || re.TaskUUID != testUUID || re.StatusCode != http.StatusBadGateway || re.ProblemType != badGateway {
		t.Fatalf("api error = %+v, want code %s, task %s, the cause's status and problem type", re, lostResponseCode, testUUID)
	}

	interrupted := newLostInvokeError(testUUID, show, fmt.Errorf("invoke sync: %w", context.Canceled), nil)
	if !errors.Is(interrupted, context.Canceled) {
		t.Fatal("an interrupted invoke must still unwrap to context.Canceled")
	}
	pollInterrupted := newLostInvokeError(testUUID, show, apiErr, context.Canceled)
	if !errors.Is(pollInterrupted, context.Canceled) {
		t.Fatal("an interrupted poll must still unwrap to context.Canceled")
	}
}

func TestTaskFailedErr(t *testing.T) {
	if err := taskFailedErr(&serverlessapi.Task{Status: serverlessapi.TaskStatusCompleted}); err != nil {
		t.Fatalf("completed: %v", err)
	}
	msg := "oom killed"
	err := taskFailedErr(&serverlessapi.Task{
		Status: serverlessapi.TaskStatusFailed,
		Error:  &msg,
	})
	if err == nil || err.Error() != msg {
		t.Fatalf("failed: got %v, want %q", err, msg)
	}
	err = taskFailedErr(&serverlessapi.Task{Status: serverlessapi.TaskStatusFailed})
	if err == nil || err.Error() != "task failed" {
		t.Fatalf("failed without string: %v", err)
	}
}
