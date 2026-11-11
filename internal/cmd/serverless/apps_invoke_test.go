package serverless

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
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
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(520)
			_, _ = io.WriteString(w, "<html>error code: 520</html>")
		case r.Method == http.MethodGet && s.taskID() != "" && r.URL.Path == testAppsPath+"/"+testAppID+"/tasks/"+s.taskID():
			w.Header().Set("Content-Type", "application/json")
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
	for _, waitFlag := range []string{"--sync", "--wait"} {
		t.Run(waitFlag, func(t *testing.T) {
			stub := &lostInvokeStub{}
			run := runServerlessAgainst(t, stub.serve(t),
				testAppsGroup, testInvokeCommand, testAppID, testInferEndpoint, waitFlag, "--poll-interval", "10ms")
			if run.err != nil {
				t.Fatalf("err = %v, want the completed task read by id", run.err)
			}
			id := stub.taskID()
			if !strings.Contains(run.cobraOutput, "No response arrived for task "+id) || !strings.Contains(run.cobraOutput, "Polling it.") {
				t.Fatalf("stderr = %q, want it to name task %s and say it polls it", run.cobraOutput, id)
			}
			if !strings.Contains(run.stdout, id) || !strings.Contains(run.stdout, string(serverlessapi.TaskStatusCompleted)) {
				t.Fatalf("stdout = %q, want completed task %s", run.stdout, id)
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

func TestInvoke_NamesTheTaskWhenTheResponseIsLost(t *testing.T) {
	stub := &lostInvokeStub{}
	run := runServerlessAgainst(t, stub.serve(t),
		testAppsGroup, testInvokeCommand, testAppID, testInferEndpoint, testEnvFlag, testEnvironment)
	if run.err == nil {
		t.Fatal("expected the lost response to fail an async invoke that does not wait")
	}
	id := stub.taskID()
	if id == "" {
		t.Fatal("no invocation reached the stub")
	}
	showCmd := "runware serverless apps tasks show " + testAppID + " " + id + " " + testStagingHint
	if !strings.Contains(run.cobraOutput, showCmd) {
		t.Fatalf("stderr = %q, want %q", run.cobraOutput, showCmd)
	}
	if !strings.Contains(run.cobraOutput, "--task-id "+id) {
		t.Fatalf("stderr = %q, want the --task-id retry for %s", run.cobraOutput, id)
	}
	if strings.Contains(run.cobraOutput, "Polling it.") {
		t.Fatalf("stderr = %q, want no polling without --wait or --sync", run.cobraOutput)
	}
	if gets := countRequests(run.requests, http.MethodGet, testAppsPath+"/"+testAppID+"/tasks/"); gets != 0 {
		t.Fatalf("requests = %v, want no task poll", run.requests)
	}
}

func TestLostResponseMessage(t *testing.T) {
	const show = "runware serverless apps tasks show my-app " + testUUID
	got := lostResponseMessage(testUUID, show, false)
	want := "No response arrived for task " + testUUID + ", so it may have started.\n" +
		"Read it with: " + show + "\n" +
		"Or run the same invoke with --task-id " + testUUID + ": it returns that task instead of starting a second run."
	if got != want {
		t.Fatalf("message =\n%s\nwant\n%s", got, want)
	}
	if polling := lostResponseMessage(testUUID, show, true); !strings.Contains(polling, "may have started. Polling it.\n") {
		t.Fatalf("polling message = %q", polling)
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
