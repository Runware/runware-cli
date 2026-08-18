package serverless

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/output"
)

func TestListPageParams(t *testing.T) {
	limit, cursor := listPageParams(0, "")
	if limit != nil || cursor != nil {
		t.Fatalf("expected nil params, got limit=%v cursor=%v", limit, cursor)
	}

	limit, cursor = listPageParams(25, "abc")
	if limit == nil || *limit != 25 {
		t.Fatalf("unexpected limit: %+v", limit)
	}
	if cursor == nil || *cursor != "abc" {
		t.Fatalf("unexpected cursor: %+v", cursor)
	}
}

func TestValidateListLimit(t *testing.T) {
	if err := validateListLimit(0); err != nil {
		t.Fatalf("unset limit: %v", err)
	}
	if err := validateListLimit(1); err != nil {
		t.Fatalf("limit 1: %v", err)
	}
	if err := validateListLimit(100); err != nil {
		t.Fatalf("limit 100: %v", err)
	}
	if err := validateListLimit(-1); err == nil {
		t.Fatal("expected error for limit -1")
	}
	if err := validateListLimit(101); err == nil {
		t.Fatal("expected error for limit 101")
	}
}

func TestParseDeploymentSort(t *testing.T) {
	got, err := parseDeploymentSort("")
	if err != nil || got != nil {
		t.Fatalf("unset sort: got=%v err=%v", got, err)
	}

	got, err = parseDeploymentSort("activity")
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	if got == nil || *got != "activity" {
		t.Fatalf("activity: got %+v", got)
	}

	got, err = parseDeploymentSort("createdAt")
	if err != nil || got == nil || *got != "createdAt" {
		t.Fatalf("createdAt: got=%v err=%v", got, err)
	}

	_, err = parseDeploymentSort("nope")
	if err == nil {
		t.Fatal("expected error for sort nope")
	}
	if !strings.Contains(err.Error(), "invalid --sort") {
		t.Fatalf("error %q should mention invalid --sort", err)
	}
}

func TestParseDeploymentStatus(t *testing.T) {
	got, err := parseDeploymentStatus("")
	if err != nil || got != nil {
		t.Fatalf("unset status: got=%v err=%v", got, err)
	}

	got, err = parseDeploymentStatus("active")
	if err != nil || got == nil || *got != "active" {
		t.Fatalf("active: got=%v err=%v", got, err)
	}

	_, err = parseDeploymentStatus("nope")
	if err == nil {
		t.Fatal("expected error for status nope")
	}
	if !strings.Contains(err.Error(), "invalid --status") {
		t.Fatalf("error %q should mention invalid --status", err)
	}
}

func TestParseWorkerStatus(t *testing.T) {
	got, err := parseWorkerStatus("")
	if err != nil || got != nil {
		t.Fatalf("unset status: got=%v err=%v", got, err)
	}

	got, err = parseWorkerStatus("ready")
	if err != nil || got == nil || *got != "ready" {
		t.Fatalf("ready: got=%v err=%v", got, err)
	}

	_, err = parseWorkerStatus("nope")
	if err == nil {
		t.Fatal("expected error for status nope")
	}
	if !strings.Contains(err.Error(), "invalid --status") {
		t.Fatalf("error %q should mention invalid --status", err)
	}
}

func TestExtraListCursorFlags(t *testing.T) {
	got := extraListCursorFlags("demo", "h100", "name", "active")
	want := "--query demo --gpu-type h100 --sort name --status active"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	got = extraListCursorFlags("", "", "", "active")
	if got != "--status active" {
		t.Fatalf("status only: got %q", got)
	}

	got = extraListCursorFlags("", "", "", "")
	if got != "" {
		t.Fatalf("empty: got %q", got)
	}

	got = extraListCursorFlags("my app", "", "", "")
	if got != `--query "my app"` {
		t.Fatalf("quoted query: got %q", got)
	}

	got = extraListCursorFlags("foo;bar", "", "", "")
	if got != `--query "foo;bar"` {
		t.Fatalf("quoted metacharacters: got %q", got)
	}
}

func TestExtraCursorFlag(t *testing.T) {
	if got := extraCursorFlag("--status", "ready"); got != "--status ready" {
		t.Fatalf("got %q", got)
	}
	if got := extraCursorFlag("--status", ""); got != "" {
		t.Fatalf("empty: got %q", got)
	}
}

func TestPrintPage_TableWritesNextCursorToErrOut(t *testing.T) {
	next := "page-2"
	page := serverlessapi.Page[serverlessapi.Deployment]{
		Data:       nil,
		NextCursor: &next,
	}

	var errOut bytes.Buffer
	if err := printPage(output.FormatTable, page, deploymentsResult(page.Data), &errOut, ""); err != nil {
		t.Fatalf("printPage: %v", err)
	}
	if !strings.Contains(errOut.String(), "--cursor page-2") {
		t.Fatalf("errOut missing next-page hint: %q", errOut.String())
	}
}

func TestPrintPage_TableIncludesExtraCursorFlags(t *testing.T) {
	next := "page-2"
	page := serverlessapi.Page[serverlessapi.SecretAttachment]{
		Data:       nil,
		NextCursor: &next,
	}

	var errOut bytes.Buffer
	if err := printPage(output.FormatTable, page, secretAttachmentsResult(page.Data), &errOut, "--status ready"); err != nil {
		t.Fatalf("printPage: %v", err)
	}
	if !strings.Contains(errOut.String(), "--status ready --cursor page-2") {
		t.Fatalf("errOut missing extra flags in next-page hint: %q", errOut.String())
	}
}

func TestSecretsResult_NoValueColumn(t *testing.T) {
	tables := []output.Tabular{
		secretsResult{},
		secretAttachmentsResult{},
		secretResult{},
		secretRemovedResult{Name: testSecretName},
		secretAttachResult{DeploymentID: "my-app", Name: testSecretName, EnvVarName: "BAR"},
		secretDetachResult{DeploymentID: "my-app", Name: testSecretName},
	}
	for _, table := range tables {
		headers := table.Headers()
		for _, h := range headers {
			if strings.EqualFold(h, "value") {
				t.Fatalf("%T table must not include a Value column: %v", table, headers)
			}
		}
	}
}

func TestVersionsResult_NilBuildID(t *testing.T) {
	rows := (versionsResult{{
		Id:            uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		VersionNumber: 1,
		CreatedAt:     time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
	}}).Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][2] != "" {
		t.Fatalf("nil BuildId should render empty, got %#v", rows[0][2])
	}
}

func TestBuildsResult_OmitsLogTailColumn(t *testing.T) {
	tables := []output.Tabular{
		buildsResult{},
		buildResult{},
	}
	for _, table := range tables {
		for _, h := range table.Headers() {
			if strings.Contains(strings.ToLower(h), "log") {
				t.Fatalf("%T table must not include a log column: %v", table, table.Headers())
			}
		}
	}
}

func TestBuildResult_NilOptionalFields(t *testing.T) {
	rows := (buildResult{
		Id:     uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		Status: "queued",
	}).Rows()
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	if rows[2][1] != "" {
		t.Fatalf("nil Error should render empty, got %#v", rows[2][1])
	}
	if rows[3][1] != "" {
		t.Fatalf("nil ExitCode should render empty, got %#v", rows[3][1])
	}
}

func TestWorkersResult_NilNodeName(t *testing.T) {
	rows := (workersResult{{
		Id:      uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		Status:  "ready",
		PodName: "worker-0",
	}}).Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][3] != "" {
		t.Fatalf("nil NodeName should render empty, got %#v", rows[0][3])
	}
}
