package serverless

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
)

func TestActivateWait_ActiveRollsInBackground(t *testing.T) {
	assertActivateWait(t, "active", "rollout continues in the background")
}

func TestActivateWait_StoppedIsRecorded(t *testing.T) {
	assertActivateWait(t, "stopped", "applied on resume")
}

func TestActivateWait_StoppingIsRecorded(t *testing.T) {
	assertActivateWait(t, "stopping", "applied on resume")
}

func assertActivateWait(t *testing.T, status, note string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/apps/"+testAppID+"/deploy" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(strings.Replace(activeAppBody(""), `"status":"active"`, `"status":"`+status+`"`, 1)))
	}))
	defer srv.Close()

	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	cmd := newAppsVersionsActivateCmd(log.New(io.Discard))
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{testAppID, "2", testWaitFlag, testTimeoutFlag, "50ms"})
	execErr := cmd.Execute()

	_ = w.Close()
	os.Stdout = old
	_, _ = io.Copy(io.Discard, r)

	if execErr != nil {
		t.Fatalf("activate: %v", execErr)
	}
	if !strings.Contains(stderr.String(), note) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), note)
	}
	if strings.Contains(stderr.String(), "ended in status") {
		t.Fatalf("stderr treated the rollout as a failure: %s", stderr.String())
	}
}
