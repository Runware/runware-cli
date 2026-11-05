package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
