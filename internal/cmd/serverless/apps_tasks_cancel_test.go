package serverless

import (
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
