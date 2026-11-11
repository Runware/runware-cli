package serverless

import (
	"os"
	"path/filepath"
	"strings"
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
