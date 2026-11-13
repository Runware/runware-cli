package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// appIDValidationDetail is the getApp 422 for an appId outside the AppId schema.
const appIDValidationDetail = `parameter "appId" in path: minimum string length is 6`

// TestRunExits1WhenADeployIsRefused pins the status a deploy step in CI reads.
// The serverless suite asserts RunE's error; this is the process exit around it.
func TestRunExits1WhenADeployIsRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNWARE_ENV", "")
	t.Setenv("RUNWARE_API_KEY", "test-key")

	problem, err := json.Marshal(map[string]any{
		"type":   "https://docs.runware.ai/serverless/errors#unprocessable-entity",
		"title":  "Unprocessable Entity",
		"status": http.StatusUnprocessableEntity,
		"detail": appIDValidationDetail,
		"errors": []map[string]string{{"detail": appIDValidationDetail}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var lookups atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		lookups.Add(1)
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write(problem)
	}))
	defer srv.Close()
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.py"), []byte("x = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr := divertStderr(t)

	code := runArgs([]string{
		"serverless", "deploy", "app.py",
		"--src-dir", dir,
		"--id", "sdxl",
		"--gpu-type", "h100",
	})
	logged := stderr()

	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, logged)
	}
	if lookups.Load() != 1 {
		t.Fatalf("lookups = %d, want the one getApp refusal\n%s", lookups.Load(), logged)
	}
	if !strings.Contains(logged, appIDValidationDetail) {
		t.Fatalf("stderr = %q, want the refusal printed", logged)
	}
}

// interruptedTaskID is the client task id an interrupted invoke sends.
const interruptedTaskID = "7c9e6679-7425-40de-944b-e07fc1f90ae7"

// holdAndInterrupt reads the request, interrupts the process and holds the
// request open, so no response answers it. The server sees the client hang
// up only once the body has been read.
func holdAndInterrupt(_ http.ResponseWriter, r *http.Request, interrupt func()) {
	_, _ = io.Copy(io.Discard, r.Body)
	interrupt()
	<-r.Context().Done()
}

// interruptPhase is a point in an invoke where the process is interrupted.
// answer serves the stub API and calls interrupt once the run reaches it.
type interruptPhase struct {
	name   string
	answer func(w http.ResponseWriter, r *http.Request, interrupt func())
}

var interruptPhases = []interruptPhase{
	{
		name:   "invoke",
		answer: holdAndInterrupt,
	},
	{
		// A 520 loses the response, and the poll that recovers it is
		// interrupted in turn.
		name: "recovery poll",
		answer: func(w http.ResponseWriter, r *http.Request, interrupt func()) {
			if r.Method != http.MethodPost {
				holdAndInterrupt(w, r, interrupt)
				return
			}
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(520)
		},
	},
}

// interruptedRun runs args against a stub API that answers with answer, and
// returns the exit status and what was written to stderr.
func interruptedRun(t *testing.T, answer func(http.ResponseWriter, *http.Request, func()), args ...string) (int, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNWARE_ENV", "")
	t.Setenv("RUNWARE_API_KEY", "test-key")

	var once sync.Once
	interrupt := func() { once.Do(func() { signalSelf(t, os.Interrupt) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer(w, r, interrupt)
	}))
	defer srv.Close()
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)
	stderr := divertStderr(t)

	code := runArgs(args)
	return code, stderr()
}

// structuredErrors is the error document --format json writes to stderr.
type structuredErrors struct {
	Errors []map[string]any `json:"errors"`
}

// TestRunPrintsALostInvokeWhenInterrupted pins what an interrupt during an
// invoke, or during the poll that recovers a lost response, leaves the user:
// the interrupted status, and the task the invoke may have started, so they
// can read it rather than run it a second time. In json, stderr holds that
// error document alone.
func TestRunPrintsALostInvokeWhenInterrupted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interrupts the test process, which Windows cannot signal")
	}
	for _, phase := range interruptPhases {
		for _, format := range []string{"table", "json"} {
			t.Run(phase.name+"/"+format, func(t *testing.T) {
				code, logged := interruptedRun(t, phase.answer,
					"serverless", "apps", "invoke", "my-app", "infer",
					"--sync",
					"--poll-interval", "10ms",
					"--task-id", interruptedTaskID,
					"--format", format,
				)

				if code != exitInterrupted {
					t.Fatalf("exit = %d, want %d\n%s", code, exitInterrupted, logged)
				}
				if format == "json" {
					var got structuredErrors
					if err := json.Unmarshal([]byte(logged), &got); err != nil || len(got.Errors) != 1 {
						t.Fatalf("stderr = %q (%v), want one json error document", logged, err)
					}
					if item := got.Errors[0]; item["code"] != "responseLost" || item["taskUUID"] != interruptedTaskID {
						t.Fatalf("json error = %v, want code responseLost and taskUUID %s", item, interruptedTaskID)
					}
					return
				}
				for _, want := range []string{"code=responseLost", "taskUUID=" + interruptedTaskID} {
					if !strings.Contains(logged, want) {
						t.Fatalf("stderr = %q, want it to carry %q", logged, want)
					}
				}
			})
		}
	}
}

// TestRunStaysSilentWhenAnInterruptLeavesNothingToResume pins the other side:
// an interrupted command whose error carries nothing to resume exits with the
// interrupted status alone, in table and json.
func TestRunStaysSilentWhenAnInterruptLeavesNothingToResume(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interrupts the test process, which Windows cannot signal")
	}
	for _, format := range []string{"table", "json"} {
		t.Run(format, func(t *testing.T) {
			code, logged := interruptedRun(t, holdAndInterrupt,
				"serverless", "apps", "tasks", "show", "my-app", interruptedTaskID,
				"--format", format,
			)
			if code != exitInterrupted {
				t.Fatalf("exit = %d, want %d\n%s", code, exitInterrupted, logged)
			}
			if logged != "" {
				t.Fatalf("stderr = %q, want nothing beside the interrupted status", logged)
			}
		})
	}
}

// signalSelf sends sig to the test process.
func signalSelf(t *testing.T, sig os.Signal) {
	t.Helper()
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Error(err)
		return
	}
	if err := p.Signal(sig); err != nil {
		t.Error(err)
	}
}

// divertStderr points the process stderr at a file for the rest of the test
// and returns what was written. runArgs builds its logger from os.Stderr.
func divertStderr(t *testing.T) func() string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = f
	t.Cleanup(func() { os.Stderr = old })
	return func() string {
		name := f.Name()
		_ = f.Close()
		logged, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(logged)
	}
}
