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

// addSensitiveFile commits a file whose name is itself text that must not be
// published, with a leak in it.
func addSensitiveFile(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, clusterHost+".md"), []byte(leak+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-q", "-m", "docs: add notes")
}

// TestPRModeRedactsInCI runs the pr mode as the workflow does, with no flags:
// in GitHub Actions the log and the step summary are public, so neither may
// show the matched text.
func TestPRModeRedactsInCI(t *testing.T) {
	dir := gitRepo(t, leak, "feat: add b")
	addSensitiveFile(t, dir)
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
	addSensitiveFile(t, dir)
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
	// With -redact the files are numbered: a path can be private too.
	for _, where := range []string{"branch name", "file #1:1", "file #2:1", "PR title", "PR body line 3"} {
		if !strings.Contains(stdout.String(), where+":") {
			t.Errorf("no finding at %q in:\n%s", where, stdout.String())
		}
	}
	for name, text := range map[string]string{
		"log":     stdout.String() + stderr.String(),
		"summary": string(got),
	} {
		if strings.Contains(text, "some-private-repo") || strings.Contains(text, clusterHost) || strings.Contains(text, "b.txt") {
			t.Errorf("the %s shows matched text or a path:\n%s", name, text)
		}
	}
	if !strings.Contains(string(got), "file #2:1") {
		t.Errorf("the summary does not number the files like the log:\n%s", got)
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

// failingCheck makes hookCheck fail for the rest of the test the way fail
// does: with an error, a panic, or a finding and an error.
func failingCheck(t *testing.T, fail string) {
	t.Helper()
	saved := hookCheck
	t.Cleanup(func() {
		hookCheck = saved
	})
	hookCheck = func(_ context.Context, _ *Scanner, _ []byte) ([]Finding, error) {
		switch fail {
		case "panic":
			panic("broken")
		case "finding":
			return []Finding{{
				Where:   "command line",
				Rule:    "test",
				Message: "test finding",
			}}, errors.New("broken")
		}
		return nil, errors.New("broken")
	}
}

// failOpenCase is a command run through the hook mode while the check
// fails, and the exit code the hook must give.
type failOpenCase struct {
	name    string
	fail    string
	command string
	want    int
}

// TestHookFailOpen checks the one path that lets a command run unchecked:
// when the check fails, only a command with no word that could publish runs.
func TestHookFailOpen(t *testing.T) {
	cases := []failOpenCase{
		{
			name:    "plain command after an error",
			fail:    "error",
			command: cmdList,
			want:    0,
		},
		{
			name:    "post after an error",
			fail:    "error",
			command: "gh pr comment 1 --body-file b.md",
			want:    exitBlock,
		},
		{
			name:    "plain command after a panic",
			fail:    "panic",
			command: cmdList,
			want:    0,
		},
		{
			name:    "post after a panic",
			fail:    "panic",
			command: "gh pr comment 1 --body-file b.md",
			want:    exitBlock,
		},
		{
			name:    "finding and an error",
			fail:    "finding",
			command: cmdList,
			want:    exitBlock,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failingCheck(t, tc.fail)
			var stderr bytes.Buffer
			if code := run(context.Background(), []string{modeHook}, bytes.NewReader(bashPayload(t, tc.command, t.TempDir())), &bytes.Buffer{}, &stderr); code != tc.want {
				t.Fatalf("exit %d, want %d: %s", code, tc.want, stderr.String())
			}
		})
	}
}

// modeCase runs a scanning mode with args, in the directory setup makes, on
// stdin, and expects the exit code want.
type modeCase struct {
	name  string
	args  []string
	setup func(t *testing.T) string
	stdin string
	want  int
}

// TestScanModes runs the range, text and files modes as make check-public
// and people run them.
func TestScanModes(t *testing.T) {
	leakyFile := func(t *testing.T) string {
		return gitRepo(t, leak, "feat: add b")
	}
	cases := []modeCase{
		{
			name:  "range with a leak in a commit message",
			args:  []string{"range"},
			setup: leakyCommit,
			want:  exitFindings,
		},
		{
			name:  "range with a leak in the diff",
			args:  []string{"range"},
			setup: leakyFile,
			want:  exitFindings,
		},
		{
			name:  "clean range",
			args:  []string{"range"},
			setup: cleanRepo,
			want:  0,
		},
		{
			name:  "text with a leak on stdin",
			args:  []string{"text", "-label", "PR body"},
			stdin: "Details\n\n" + leak + "\n",
			want:  exitFindings,
		},
		{
			name:  "clean text on stdin",
			args:  []string{"text", "-"},
			stdin: cleanText + "\n",
			want:  0,
		},
		{
			name:  "files with a leak",
			args:  []string{wordFiles},
			setup: leakyFile,
			want:  exitFindings,
		},
		{
			name:  "named clean file",
			args:  []string{wordFiles, "a.txt"},
			setup: leakyFile,
			want:  0,
		},
		{
			name:  "clean files",
			args:  []string{wordFiles},
			setup: cleanRepo,
			want:  0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.setup != nil {
				dir = tc.setup(t)
			}
			args := append([]string{tc.args[0], "-C", dir, "-redact=false"}, tc.args[1:]...)
			var stdout, stderr bytes.Buffer
			if code := run(context.Background(), args, strings.NewReader(tc.stdin), &stdout, &stderr); code != tc.want {
				t.Fatalf("exit %d, want %d:\n%s%s", code, tc.want, stdout.String(), stderr.String())
			}
			if tc.want == exitFindings && !strings.Contains(stdout.String(), "some-private-repo") {
				t.Errorf("the report does not show the finding:\n%s", stdout.String())
			}
		})
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
	// prebuilt runs the wrapper once with a tool that builds before the
	// source breaks.
	prebuilt bool
	want     int
}

// rawBashPayload returns a Bash hook payload as an agent sends it, with no
// HTML escaping of &, < and >.
func rawBashPayload(t *testing.T, command, description string) string {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(hookToolInput{
		Command: command,
	}); err != nil {
		t.Fatal(err)
	}
	input := strings.TrimSuffix(strings.TrimSpace(b.String()), "}") + `,"description":"` + description + `"}`
	return `{"tool_name":"Bash","tool_input":` + input + `}`
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
			name:    "build failure without a build lets a command that cannot publish run",
			mode:    modeHook,
			payload: `{"session_id":"s","cwd":"/src/github","tool_name":"Bash","tool_input":{"command":"ls -la","description":"List files"}}`,
			broken:  true,
			want:    0,
		},
		{
			name:    "build failure without a build blocks a JSON-escaped publication",
			mode:    modeHook,
			payload: `{"tool_name":"Bash","tool_input":{"command":"\u0067h pr comment 1 --body x"}}`,
			broken:  true,
			want:    exitBlock,
		},
		{
			name:    "build failure without a build lets a file tool run under a github.com path",
			mode:    modeHook,
			payload: `{"session_id":"s","cwd":"/src/github.com/o/r","tool_name":"Edit","tool_input":{"file_path":"/src/github.com/o/r/tools/checkpublic/x.go","old_string":"gh pr comment","new_string":"git push"}}`,
			broken:  true,
			want:    0,
		},
		{
			name:    "build failure without a build blocks a Bash publication beside a github.com path",
			mode:    modeHook,
			payload: `{"cwd":"/src/github.com/o/r","tool_name":"Bash","tool_input":{"command":"gh pr comment 1 --body x","description":"Post"}}`,
			broken:  true,
			want:    exitBlock,
		},
		{
			name:    "build failure without a build lets an opaque launcher run",
			mode:    modeHook,
			payload: `{"cwd":"/src/github.com/o/r","tool_name":"Bash","tool_input":{"command":"./deploy.sh","description":"Run the gh script"}}`,
			broken:  true,
			want:    0,
		},
		{
			name:     "build failure with a previous build uses it",
			mode:     modeHook,
			payload:  quotedName,
			toolExit: "0",
			broken:   true,
			prebuilt: true,
			want:     0,
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
	// With no build, the shell copy of clearlyPublishesNothing answers as it
	// does, also on a payload with a line continuation or a JSON escape, and
	// it reads no field that only describes the call.
	for command, clear := range clearCases {
		want := exitBlock
		if clear {
			want = 0
		}
		cases = append(cases, wrapperCase{
			name:    "no build: " + command,
			mode:    modeHook,
			payload: rawBashPayload(t, command, "Check the git status"),
			broken:  true,
			want:    want,
		})
	}
	cases = append(cases, wrapperCase{
		name:    "no build: another tool whose description mentions git",
		mode:    modeHook,
		payload: `{"tool_name":"WebFetch","tool_input":{"url":"https://example.com","description":"Read the git docs"}}`,
		broken:  true,
		want:    0,
	})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gitIn(t, dir, "init", "-q")
			src := fakeTool
			if tc.broken && !tc.prebuilt {
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
			if tc.prebuilt {
				build := exec.CommandContext(context.Background(), filepath.Join(dir, ".githooks", "checkpublic"), tc.mode) //nolint:gosec // test script
				build.Stdin = strings.NewReader(tc.payload)
				build.Env = append(os.Environ(), "FAKE_OUT="+out, "FAKE_EXIT=0")
				if b, err := build.CombinedOutput(); err != nil {
					t.Fatalf("first run: %v: %s", err, b)
				}
				source := filepath.Join(dir, "tools", "checkpublic", "main.go")
				if err := os.WriteFile(source, []byte("package main\n\nfunc main() {"), 0o600); err != nil {
					t.Fatal(err)
				}
				later := time.Now().Add(time.Hour)
				if err := os.Chtimes(source, later, later); err != nil {
					t.Fatal(err)
				}
			}
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
			if tc.prebuilt && !strings.Contains(string(b), "previous build") {
				t.Errorf("no warning that the previous build checked: %s", b)
			}
			if tc.broken && !tc.prebuilt {
				return
			}
			if got, err := os.ReadFile(out); err != nil || string(got) != tc.payload {
				t.Errorf("tool read %q (%v), want the payload %q", got, err, tc.payload)
			}
		})
	}
}
