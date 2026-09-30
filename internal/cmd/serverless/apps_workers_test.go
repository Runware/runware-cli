package serverless

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
)

type workerReasonCase struct {
	status string
	reason string
}

func TestWorkersListShowsFailureReason(t *testing.T) {
	for _, tc := range []workerReasonCase{
		{
			status: "unhealthy",
			reason: "CrashLoopBackOff",
		},
		{
			status: "loading",
			reason: "Error",
		},
		{
			status: "loading",
			reason: "OOMKilled",
		},
		{
			status: testLogBodyReady,
		},
	} {
		t.Run(tc.status+"/"+tc.reason, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/apps/"+testAppID+"/workers" {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, workerReasonResponse(tc.status, tc.reason))
			}))
			defer srv.Close()
			t.Setenv("RUNWARE_API_KEY", "test-key")
			t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)
			cmd := newAppsWorkersCmd(log.New(io.Discard))
			cmd.SetArgs([]string{testAppID})
			cmd.SetErr(io.Discard)
			out := captureServerlessCommandOutput(t, cmd)
			for _, text := range []string{"STATUS REASON", tc.status, tc.reason, "worker-0"} {
				if !strings.Contains(out, text) {
					t.Fatalf("table missing %q: %s", text, out)
				}
			}
		})
	}
}

func workerReasonResponse(status, reason string) string {
	value := "null"
	if reason != "" {
		value = fmt.Sprintf("%q", reason)
	}
	return fmt.Sprintf(`{"data":[{"id":"44444444-4444-4444-4444-444444444444","appId":"my-app","status":%q,"statusReason":%s,"podName":"worker-0","nodeName":null,"versionId":"22222222-2222-2222-2222-222222222222","createdAt":"2026-09-30T12:00:00Z","statusOccurredAt":"2026-09-30T12:01:00Z"}]}`, status, value)
}
