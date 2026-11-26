package serverless

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

func TestParseTaskStatus_Canceled(t *testing.T) {
	got, err := parseTaskStatus("canceled")
	if err != nil || got == nil || *got != serverlessapi.TaskStatusCanceled {
		t.Fatalf("canceled: got=%v err=%v", got, err)
	}
}

func TestAppsTasksCancel_NeedsATaskIDOrAll(t *testing.T) {
	for name, args := range map[string][]string{
		"neither": {testAppID},
		"both":    {testAppID, "7c9e6679-7425-40de-944b-e07fc1f90ae7", "--all"},
	} {
		t.Run(name, func(t *testing.T) {
			cmd := newAppsTasksCancelCmd(log.New(io.Discard))
			cmd.SetArgs(args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "either a task id or --all") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestTasksCanceledResult_Rows(t *testing.T) {
	r := tasksCanceledResult{
		AppID:    testAppID,
		Canceled: 4,
	}
	if got := r.Headers(); len(got) != 2 || got[1] != colCanceled {
		t.Fatalf("headers: %v", got)
	}
	if rows := r.Rows(); len(rows) != 1 || rows[0][0] != testAppID || rows[0][1] != int64(4) {
		t.Fatalf("rows: %#v", rows)
	}
}

func TestConfirmCancelAll_NonTTYRequiresYes(t *testing.T) {
	err := confirmCancelAll(testAppID, false, strings.NewReader("y\n"), io.Discard, false, testDeleteKey)
	if !errors.Is(err, errCancelAllNeedsConfirm) {
		t.Fatalf("expected errCancelAllNeedsConfirm, got %v", err)
	}
}

func TestConfirmCancelAll_PromptsAndAcceptsYes(t *testing.T) {
	var out bytes.Buffer
	if err := confirmCancelAll(testAppID, false, strings.NewReader("yes\n"), &out, true, testDeleteKey); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.HasPrefix(got, "Cancel ") || !strings.Contains(got, testAppID) {
		t.Fatalf("prompt = %q", got)
	}
}

func TestConfirmCancelAll_DeclinedAndSkipped(t *testing.T) {
	for _, input := range []string{"n\n", "\n", "maybe\n"} {
		err := confirmCancelAll(testAppID, false, strings.NewReader(input), io.Discard, true, testDeleteKey)
		if !errors.Is(err, errCancelAllDeclined) {
			t.Fatalf("input %q: expected errCancelAllDeclined, got %v", input, err)
		}
	}
	if err := confirmCancelAll(testAppID, true, strings.NewReader(""), io.Discard, false, testDeleteKey); err != nil {
		t.Fatalf("--yes must skip the prompt: %v", err)
	}
}
