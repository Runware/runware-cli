package installtest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/runware/runware-cli/internal/installtest"
)

// minimalInstaller stands in for the Serverless install script: it fetches
// the latest archive, unpacks it into the directory given as its first
// argument and runs the CLI it contains.
const minimalInstaller = `#!/bin/sh
set -e
tag=$(curl -fsS "$RUNWARE_RELEASE_BASE_URL/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
curl -fsS "$RUNWARE_RELEASE_BASE_URL/download/$tag/runware_linux_amd64.tar.gz" | tar -xzf - -C "$1"
"$1/runware" serverless init "my app"
`

// TestRunSh_ASecondScriptCanInstallFromTheSameReleaseAndRunTheStub checks the
// harness from the side a new install script sees it: the release answers the
// lookup and the download, records both requests, and the stub CLI it serves
// records its argv with spaces kept inside one argument.
func TestRunSh_ASecondScriptCanInstallFromTheSameReleaseAndRunTheStub(t *testing.T) {
	t.Parallel()

	release := installtest.NewRelease(t, installtest.ReleaseConfig{
		Tag: "v1.2.3",
	})
	script := filepath.Join(t.TempDir(), "install.sh")
	if err := os.WriteFile(script, []byte(minimalInstaller), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	installDir := t.TempDir()

	res := installtest.RunSh(t, installtest.Invocation{
		Script: script,
		Args: []string{
			installDir,
		},
		Env: map[string]string{
			"RUNWARE_RELEASE_BASE_URL": release.URL,
		},
	})

	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\n%s", res.ExitCode, res.Output)
	}
	wantRequests := []string{
		"/latest",
		"/download/v1.2.3/runware_linux_amd64.tar.gz",
	}
	if got := release.Requests(); !slices.Equal(got, wantRequests) {
		t.Errorf("requests:\n got %q\nwant %q", got, wantRequests)
	}
	calls := res.StubCalls(t)
	wantCall := []string{
		"serverless",
		"init",
		"my app",
	}
	if len(calls) != 1 || !slices.Equal(calls[0], wantCall) {
		t.Errorf("stub calls = %q, want one call %q", calls, wantCall)
	}
}

// TestRunSh_TimeoutStopsAScriptWhoseChildHangs runs a script whose child
// never finishes, as a stalled curl would, and checks that the run fails the
// test at its timeout instead of waiting for the child.
func TestRunSh_TimeoutStopsAScriptWhoseChildHangs(t *testing.T) {
	t.Parallel()

	script := filepath.Join(t.TempDir(), "hang.sh")
	if err := os.WriteFile(script, []byte("sleep 10\n"), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	rec := &fatalRecorder{
		TB: t,
	}
	start := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		installtest.RunSh(rec, installtest.Invocation{
			Script:  script,
			Timeout: 200 * time.Millisecond,
		})
	}()
	<-done

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("run returned after %s, want soon after its 200ms timeout", elapsed)
	}
	if !strings.Contains(rec.fatal, "did not finish within 200ms") {
		t.Errorf("fatal message = %q, want it to report the timeout", rec.fatal)
	}
}

// fatalRecorder stands in for the test in a harness call that is expected to
// fail it. It records the message and stops the calling goroutine, as
// t.Fatalf does, but leaves the real test running.
type fatalRecorder struct {
	testing.TB

	fatal string
}

func (r *fatalRecorder) Fatalf(format string, args ...any) {
	r.fatal = fmt.Sprintf(format, args...)
	runtime.Goexit()
}
