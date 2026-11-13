package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
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
