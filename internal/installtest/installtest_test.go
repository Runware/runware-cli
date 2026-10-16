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
"$1/runware" serverless init -- "my app"
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
	want := []installtest.StubCall{
		{
			Stub: installtest.ReleaseStubName,
			Args: installtest.InitArgs("my app"),
		},
	}
	if got := res.StubCalls(t); !slices.EqualFunc(got, want, stubCallEqual) {
		t.Errorf("stub calls = %q, want %q", got, want)
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

// TestStub_AnswersAsACLIWithOrWithoutInit runs stub CLIs directly and checks
// that each one records its calls under its own name and answers the init
// probe the way a real CLI of that age does: a CLI from before init prints
// the serverless group's help and still exits 0, as Cobra does for an
// unknown subcommand.
func TestStub_AnswersAsACLIWithOrWithoutInit(t *testing.T) {
	t.Parallel()

	tests := []stubCase{
		{
			name: "init help",
			cfg: installtest.StubConfig{
				Name: "new",
			},
			args:    initHelp,
			wantOut: "runware serverless init [dir] [flags]",
		},
		{
			name: "init help before init existed",
			cfg: installtest.StubConfig{
				Name:        "old",
				WithoutInit: true,
			},
			args:    initHelp,
			wantOut: "runware serverless [command]",
		},
		{
			name: "init that fails",
			cfg: installtest.StubConfig{
				Name:     "failing",
				InitExit: 3,
			},
			args:     installtest.InitArgs("my app"),
			wantExit: 3,
			wantOut:  "stub serverless init my app",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := installtest.WriteStub(t, tt.cfg)
			script := filepath.Join(t.TempDir(), "run.sh")
			body := `exec runware "$@"` + "\n"
			if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
				t.Fatalf("write script: %v", err)
			}

			res := installtest.RunSh(t, installtest.Invocation{
				Script: script,
				Args:   tt.args,
				PathDirs: []string{
					dir,
				},
			})

			if res.ExitCode != tt.wantExit {
				t.Errorf("exit code = %d, want %d\n%s", res.ExitCode, tt.wantExit, res.Output)
			}
			if !strings.Contains(res.Output, tt.wantOut) {
				t.Errorf("output does not contain %q:\n%s", tt.wantOut, res.Output)
			}
			want := []installtest.StubCall{
				{
					Stub: tt.cfg.Name,
					Args: tt.args,
				},
			}
			if got := res.StubCalls(t); !slices.EqualFunc(got, want, stubCallEqual) {
				t.Errorf("stub calls = %q, want %q", got, want)
			}
		})
	}
}

// initHelp is the argv of the probe that asks a CLI for the help of init.
var initHelp = []string{
	"serverless",
	"init",
	"--help",
}

// stubCase is one call to a stub CLI and what it must answer.
type stubCase struct {
	name     string
	cfg      installtest.StubConfig
	args     []string
	wantExit int
	wantOut  string
}

// TestTripwires_RecordEveryToolThatRan puts tripwires for two tools on PATH,
// runs one of them, and checks that the run fails and that only that tool is
// reported.
func TestTripwires_RecordEveryToolThatRan(t *testing.T) {
	t.Parallel()

	tw := installtest.NewTripwires(t, "pip", "uv")
	script := filepath.Join(t.TempDir(), "run.sh")
	if err := os.WriteFile(script, []byte("uv add runware-serverless\n"), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	res := installtest.RunSh(t, installtest.Invocation{
		Script: script,
		PathDirs: []string{
			tw.Dir,
		},
	})

	if res.ExitCode == 0 {
		t.Errorf("exit code = 0, want non-zero from the tripwire\n%s", res.Output)
	}
	if got, want := tw.Calls(t), []string{"uv add runware-serverless"}; !slices.Equal(got, want) {
		t.Errorf("tripwire calls = %q, want %q", got, want)
	}
}

// stubCallEqual reports whether two stub calls came from the same stub with
// the same argv.
func stubCallEqual(a, b installtest.StubCall) bool {
	return a.Stub == b.Stub && slices.Equal(a.Args, b.Args)
}
