package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// leakyEvent writes a pull_request event whose title, body and branch name
// hold leaks, for the pull request gitRepo made in dir.
func leakyEvent(t *testing.T, dir string) string {
	t.Helper()
	ev := prEvent{
		PullRequest: prPayload{
			Title: "fix: " + leak,
			Body:  "Details\n\n" + leak,
			Base: prRef{
				Ref: "main",
				SHA: gitIn(t, dir, "rev-parse", "origin/main"),
			},
			Head: prRef{
				Ref: "fix/" + clusterHost,
				SHA: gitIn(t, dir, "rev-parse", "HEAD"),
			},
		},
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	event := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(event, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return event
}

// TestPRModeRedactsInCI runs the pr mode as the workflow does, with no flags:
// in GitHub Actions the log and the step summary are public, so neither may
// show the matched text.
func TestPRModeRedactsInCI(t *testing.T) {
	dir := gitRepo(t, leak, "feat: add b")
	summary := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	t.Setenv("GITHUB_EVENT_PATH", leakyEvent(t, dir))
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"pr", "-C", dir}, strings.NewReader(""), &stdout, &stderr); code != exitFindings {
		t.Fatalf("exit %d, want %d: %s", code, exitFindings, stderr.String())
	}
	got, err := os.ReadFile(summary) //nolint:gosec // a test file
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "finding(s)") {
		t.Errorf("summary has no report:\n%s", got)
	}
	for name, text := range map[string]string{
		"stdout":  stdout.String(),
		"stderr":  stderr.String(),
		"summary": string(got),
	} {
		if strings.Contains(text, "some-private-repo") || strings.Contains(text, clusterHost) {
			t.Errorf("the %s shows matched text:\n%s", name, text)
		}
	}
}

// TestPRModeReadsEventFile checks that the pr mode reads the title, body and
// branch name from the event file, so the workflow passes no author text
// through env, and that it hides the matched text.
func TestPRModeReadsEventFile(t *testing.T) {
	dir := gitRepo(t, leak, "feat: add b")
	event := leakyEvent(t, dir)
	summary := filepath.Join(t.TempDir(), "summary.md")

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"pr", "-C", dir, "-event", event, "-redact", "-summary", summary}, strings.NewReader(""), &stdout, &stderr)
	if code != exitFindings {
		t.Fatalf("exit %d, want %d: %s", code, exitFindings, stderr.String())
	}
	got, err := os.ReadFile(summary) //nolint:gosec // a test file
	if err != nil {
		t.Fatal(err)
	}
	for _, where := range []string{"branch name", "b.txt:1", "PR title", "PR body line 3"} {
		if !strings.Contains(stdout.String(), where+":") {
			t.Errorf("no finding at %q in:\n%s", where, stdout.String())
		}
	}
	for name, text := range map[string]string{
		"log":     stdout.String() + stderr.String(),
		"summary": string(got),
	} {
		if strings.Contains(text, "some-private-repo") || strings.Contains(text, clusterHost) {
			t.Errorf("the %s shows matched text:\n%s", name, text)
		}
	}

	stderr.Reset()
	if code := run(context.Background(), []string{"pr", "-C", dir, "-event", ""}, strings.NewReader(""), &stdout, &stderr); code != exitError {
		t.Errorf("exit %d without an event file, want %d", code, exitError)
	}
}

// TestHookFindingsBeforeDenyWarning checks that a blocked hook leads with its
// findings: an agent shows the first line of the hook's stderr as the reason.
func TestHookFindingsBeforeDenyWarning(t *testing.T) {
	saved := newDenySource
	t.Cleanup(func() {
		newDenySource = saved
	})
	newDenySource = func(_ bool, _ string, stderr io.Writer) denySource {
		gh := &fakeGH{
			err: errors.New("no access"),
		}
		return denySource{
			repo:     testRepo,
			run:      gh.run,
			now:      func() time.Time { return testNow },
			cacheDir: t.TempDir(),
			stderr:   stderr,
		}
	}
	payload, err := json.Marshal(map[string]any{
		"tool_name": "Bash",
		"tool_input": map[string]any{
			"command": "gh pr comment 1 -R Runware/runware-cli --body '" + leak + "'",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{modeHook}, bytes.NewReader(payload), &stdout, &stderr); code != exitBlock {
		t.Fatalf("exit %d, want %d; stderr:\n%s", code, exitBlock, stderr.String())
	}
	out := stderr.String()
	if !strings.HasPrefix(out, "checkpublic: blocked") {
		t.Fatalf("stderr does not lead with the block:\n%s", out)
	}
	if !strings.Contains(out, "warning: the repository deny list is unavailable") {
		t.Fatalf("deny list warning missing:\n%s", out)
	}
}

// fakeTool is a stand-in for tools/checkpublic that the wrapper script builds:
// it copies its stdin to $FAKE_OUT and exits with $FAKE_EXIT.
const fakeTool = `package main

import (
	"io"
	"os"
	"strconv"
)

func main() {
	b, _ := io.ReadAll(os.Stdin)
	_ = os.WriteFile(os.Getenv("FAKE_OUT"), b, 0o600)
	code, _ := strconv.Atoi(os.Getenv("FAKE_EXIT"))
	os.Exit(code)
}
`

// wrapperCase runs the wrapper script in mode with a fake tool that exits
// with toolExit (or does not build, when broken) and expects it to exit with
// want.
type wrapperCase struct {
	name     string
	mode     string
	payload  string
	toolExit string
	broken   bool
	want     int
}

// TestWrapperScript checks .githooks/checkpublic: it hands every payload to
// the tool, and in hook mode it exits 2, the status that blocks a tool call,
// when the tool fails or does not build.
func TestWrapperScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the wrapper is a POSIX shell script")
	}
	script, err := os.ReadFile(filepath.Join("..", "..", ".githooks", "checkpublic"))
	if err != nil {
		t.Fatal(err)
	}
	quotedName := `{"tool_name":"Bash","tool_input":{"command":"g'h' pr comment 1 --body x"}}`
	cases := []wrapperCase{
		{
			name:     "quoted command name reaches the tool",
			mode:     modeHook,
			payload:  quotedName,
			toolExit: "0",
			want:     0,
		},
		{
			name:     "tool blocks",
			mode:     modeHook,
			payload:  quotedName,
			toolExit: "2",
			want:     exitBlock,
		},
		{
			name:     "tool error in hook mode blocks",
			mode:     modeHook,
			payload:  quotedName,
			toolExit: "3",
			want:     exitBlock,
		},
		{
			name:     "tool exit 1 in hook mode blocks",
			mode:     modeHook,
			payload:  quotedName,
			toolExit: "1",
			want:     exitBlock,
		},
		{
			name:    "build failure in hook mode blocks",
			mode:    modeHook,
			payload: quotedName,
			broken:  true,
			want:    exitBlock,
		},
		{
			name:     "findings in pre-push mode",
			mode:     "prepush",
			toolExit: "1",
			want:     exitFindings,
		},
		{
			name:   "build failure in pre-push mode blocks",
			mode:   "prepush",
			broken: true,
			want:   exitBlock,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gitIn(t, dir, "init", "-q")
			src := fakeTool
			if tc.broken {
				src = "package main\n\nfunc main() {"
			}
			files := map[string]string{
				".githooks/checkpublic":     string(script),
				"go.mod":                    "module fake\n\ngo 1.22\n",
				"tools/checkpublic/main.go": src,
			}
			for name, text := range files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(text), 0o700); err != nil { //nolint:gosec // the script must be executable
					t.Fatal(err)
				}
			}
			out := filepath.Join(dir, "stdin.txt")
			cmd := exec.CommandContext(context.Background(), filepath.Join(dir, ".githooks", "checkpublic"), tc.mode) //nolint:gosec // test script
			cmd.Stdin = strings.NewReader(tc.payload)
			cmd.Env = append(os.Environ(), "FAKE_OUT="+out, "FAKE_EXIT="+tc.toolExit)
			b, err := cmd.CombinedOutput()
			code := 0
			if ee, ok := errors.AsType[*exec.ExitError](err); ok {
				code = ee.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if code != tc.want {
				t.Fatalf("exit %d, want %d: %s", code, tc.want, b)
			}
			if tc.broken {
				return
			}
			if got, err := os.ReadFile(out); err != nil || string(got) != tc.payload {
				t.Errorf("tool read %q (%v), want the payload %q", got, err, tc.payload)
			}
		})
	}
}
