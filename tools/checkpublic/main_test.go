package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
				SHA: gitIn(t, dir, "rev-parse", originMain),
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

// TestPrePushRedactsTagName checks that a redacted report does not show
// the name of a tag whose message has a finding, and keeps the line number.
func TestPrePushRedactsTagName(t *testing.T) {
	dir := cleanRepo(t)
	tag := "v1-" + clusterHost
	gitIn(t, dir, "tag", "-a", tag, "-m", "release\n\n"+leak, originMain)
	line := "refs/tags/" + tag + " " + gitIn(t, dir, "rev-parse", "refs/tags/"+tag) + " refs/tags/" + tag + " " + strings.Repeat("0", 40) + "\n"
	summary := filepath.Join(t.TempDir(), "summary.md")
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"prepush", "-C", dir, "-redact", "-summary", summary, remoteOrigin}, strings.NewReader(line), &stdout, &stderr); code != exitFindings {
		t.Fatalf("exit %d, want %d: %s", code, exitFindings, stderr.String())
	}
	got, err := os.ReadFile(summary) //nolint:gosec // a test file
	if err != nil {
		t.Fatal(err)
	}
	for _, where := range []string{"tag name:", "tag #1 message line 3:"} {
		if !strings.Contains(stdout.String(), where) {
			t.Errorf("no finding at %q in:\n%s", where, stdout.String())
		}
	}
	for name, text := range map[string]string{
		"log":     stdout.String() + stderr.String(),
		"summary": string(got),
	} {
		if strings.Contains(text, clusterHost) || strings.Contains(text, "some-private-repo") {
			t.Errorf("the %s shows the tag name or matched text:\n%s", name, text)
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

// modeCase runs a scanning mode with args, in the directory setup makes, on
// stdin, and expects the exit code want.
type modeCase struct {
	name  string
	args  []string
	setup func(t *testing.T) string
	stdin string
	want  int
}

// TestScanModes runs the range and text modes as make check-public
// and people run them.
func TestScanModes(t *testing.T) {
	leakyFile := func(t *testing.T) string {
		return gitRepo(t, leak, "feat: add b")
	}
	cases := []modeCase{
		{
			name:  "range with a leak in a commit message",
			args:  []string{modeRange},
			setup: leakyCommit,
			want:  exitFindings,
		},
		{
			name:  "range with a leak in the diff",
			args:  []string{modeRange},
			setup: leakyFile,
			want:  exitFindings,
		},
		{
			name:  "clean range",
			args:  []string{modeRange},
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
