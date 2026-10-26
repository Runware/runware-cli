package serverless

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
)

func TestParseInvokeInput_RejectsLeadingSlash(t *testing.T) {
	_, _, err := parseInvokeInput("/infer", "", strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "bare segment") {
		t.Fatalf("expected leading-slash error, got %v", err)
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

const (
	testInferEndpoint = "infer"
	testResumeCommand = "resume"
	testScaleCommand  = "scale"
)

// pausedErrors is the structured error output of a command.
type pausedErrors struct {
	Errors []map[string]any `json:"errors"`
}

func TestCommandsInAPausedEnvironmentReportThePause(t *testing.T) {
	const pausedType = "https://docs.runware.ai/serverless/errors#environment-paused"
	const detail = "environment staging is paused: its spend this month reached its monthly budget"
	tests := []struct {
		name   string
		status int
		args   []string
	}{
		{
			name:   "invoke",
			status: http.StatusTooManyRequests,
			args:   []string{testAppsGroup, "invoke", testAppID, testInferEndpoint, testEnvFlag, testEnvironment},
		},
		{
			name:   testResumeCommand,
			status: http.StatusConflict,
			args:   []string{testAppsGroup, testResumeCommand, testAppID, testEnvFlag, testEnvironment},
		},
		{
			name:   testScaleCommand,
			status: http.StatusConflict,
			args:   []string{testAppsGroup, testScaleCommand, testAppID, "--min-workers", "1", testEnvFlag, testEnvironment},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run := runServerlessAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprintf(w, `{"type":%q,"title":%q,"status":%d,"detail":%q}`, pausedType, http.StatusText(tc.status), tc.status, detail)
			}, tc.args...)
			if run.err == nil {
				t.Fatal("expected an error")
			}

			var structured bytes.Buffer
			cmdutil.PrintErrorTo(log.New(io.Discard), &structured, output.FormatJSON, run.err)
			var got pausedErrors
			if err := json.Unmarshal(structured.Bytes(), &got); err != nil || len(got.Errors) != 1 {
				t.Fatalf("json output %q: %v", structured.String(), err)
			}
			item := got.Errors[0]
			if item["code"] != "environmentPaused" || item["message"] != detail || item["problemType"] != pausedType {
				t.Errorf("json error = %v, want code environmentPaused, the detail and the problem type", item)
			}

			var text bytes.Buffer
			cmdutil.PrintErrorTo(log.New(&text), io.Discard, output.FormatTable, run.err)
			if !strings.Contains(text.String(), detail) || !strings.Contains(text.String(), "code=environmentPaused") {
				t.Errorf("text output %q, want the detail and code=environmentPaused", text.String())
			}
		})
	}
}
