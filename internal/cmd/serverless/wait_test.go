package serverless

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/spf13/cobra"
)

func TestWaitFlagsRegistered(t *testing.T) {
	cmds := []*cobra.Command{
		newDeployCmd(nil),
		newAppsInvokeCmd(nil),
		newAppsResumeCmd(nil),
		newAppsVersionsActivateCmd(nil),
	}
	for _, cmd := range cmds {
		if cmd.Flags().Lookup("wait") == nil {
			t.Errorf("%s is missing --wait", cmd.Name())
		}
		if cmd.Flags().Lookup("timeout") == nil {
			t.Errorf("%s is missing --timeout", cmd.Name())
		}
		if cmd.Flags().Lookup("poll-interval") == nil {
			t.Errorf("%s is missing --poll-interval", cmd.Name())
		}
	}
}

func TestAddAppWaitFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "resume"}
	var (
		wait         bool
		timeout      time.Duration
		pollInterval time.Duration
	)
	addAppWaitFlags(cmd, &wait, &timeout, &pollInterval)
	if err := cmd.ParseFlags([]string{"--wait", "--timeout", "30s", "--poll-interval", "1s"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if !wait || timeout != 30*time.Second || pollInterval != time.Second {
		t.Fatalf("wait=%v timeout=%s poll=%s", wait, timeout, pollInterval)
	}
}

func TestWaitContext_NoTimeoutUsesParent(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, stop := waitContext(parent, 0)
	defer stop()
	if ctx != parent {
		t.Fatal("zero timeout should reuse the parent context")
	}
}

func TestWaitContext_TimeoutCancels(t *testing.T) {
	ctx, cancel := waitContext(context.Background(), time.Millisecond)
	defer cancel()
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("err = %v", ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("expected the wait context to expire")
	}
}

func TestWaitTimeoutErr(t *testing.T) {
	if err := waitTimeoutErr(nil, "application my-app", time.Second); err != nil {
		t.Fatalf("nil: %v", err)
	}
	if err := waitTimeoutErr(errors.New("boom"), "application my-app", time.Second); err == nil || err.Error() != "boom" {
		t.Fatalf("passthrough: %v", err)
	}

	err := waitTimeoutErr(context.DeadlineExceeded, "application my-app", 30*time.Second)
	if err == nil || !strings.Contains(err.Error(), "timed out waiting for application my-app after 30s") {
		t.Fatalf("timeout: %v", err)
	}
	err = waitTimeoutErr(context.DeadlineExceeded, "task t1", 0)
	if err == nil || !strings.Contains(err.Error(), "timed out waiting for task t1") {
		t.Fatalf("zero timeout: %v", err)
	}
}

func TestWaitFlagsNotOnStopOrDelete(t *testing.T) {
	for _, cmd := range []*cobra.Command{newAppsStopCmd(nil), newAppsDeleteCmd(nil)} {
		if cmd.Flags().Lookup("wait") != nil || cmd.Flags().Lookup("timeout") != nil {
			t.Errorf("%s should not have --wait/--timeout", cmd.Name())
		}
	}
}

func TestWaitForApp_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"appId":"` + testAppID + `","appName":"My App","status":"initializing","configuration":{"maxWorkers":1,"idleTtlSecs":60,"scalingDelaySecs":10,"computeType":"gpu","gpuType":"h100"},"environmentVariables":[],"secrets":[],"createdAt":"2026-07-30T12:00:00Z","updatedAt":"2026-07-30T12:00:00Z"}`))
	}))
	defer srv.Close()

	client := serverlessapi.NewClient("test-key", srv.URL, slog.Default())
	_, err := waitForApp(context.Background(), client, testAppID, time.Millisecond, 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out waiting for application "+testAppID+" after 20ms") {
		t.Fatalf("timeout: %v", err)
	}
}
