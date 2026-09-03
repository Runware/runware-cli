package serverless

import (
	"strings"
	"testing"
	"time"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

const testTaskID = "task-1"

func TestParseTaskStatus(t *testing.T) {
	got, err := parseTaskStatus("")
	if err != nil || got != nil {
		t.Fatalf("unset status: got=%v err=%v", got, err)
	}

	got, err = parseTaskStatus("pending")
	if err != nil || got == nil || *got != serverlessapi.TaskStatusPending {
		t.Fatalf("pending: got=%v err=%v", got, err)
	}

	_, err = parseTaskStatus("running")
	if err == nil || !strings.Contains(err.Error(), "invalid --status") {
		t.Fatalf("expected invalid --status, got %v", err)
	}
}

func TestTaskResult_IncludesOutputAndError(t *testing.T) {
	created := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	completed := created.Add(5 * time.Second)
	errMsg := "oom killed"
	output := map[string]any{"ok": true}
	r := taskResult{
		Id:          testTaskID,
		AppId:       testAppID,
		Status:      serverlessapi.TaskStatusFailed,
		Error:       &errMsg,
		Output:      &output,
		CreatedAt:   created,
		CompletedAt: &completed,
	}
	rows := r.Rows()
	got := make(map[string]any, len(rows))
	for _, row := range rows {
		got[row[0].(string)] = row[1]
	}
	if got[colID] != testTaskID || got[colStatus] != "failed" || got[colError] != errMsg {
		t.Fatalf("rows = %#v", got)
	}
	if got["Output"] != `{"ok":true}` {
		t.Fatalf("output = %#v", got["Output"])
	}
}

func TestTasksResult_Headers(t *testing.T) {
	r := tasksResult{
		{Id: testTaskID, Status: serverlessapi.TaskStatusPending, CreatedAt: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)},
	}
	if got := r.Headers(); len(got) != 5 {
		t.Fatalf("headers: %v", got)
	}
	rows := r.Rows()
	if len(rows) != 1 || rows[0][0] != testTaskID || rows[0][1] != "pending" {
		t.Fatalf("rows: %#v", rows)
	}
}
