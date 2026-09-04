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

const (
	testAppID    = "my-app"
	testEnvKey   = "MY_KEY"
	testEnvValue = "hello"
	testGPUType  = "h100"
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

func TestParseAppSort(t *testing.T) {
	got, err := parseAppSort("")
	if err != nil || got != nil {
		t.Fatalf("unset sort: got=%v err=%v", got, err)
	}

	got, err = parseAppSort("activity")
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	if got == nil || *got != "activity" {
		t.Fatalf("activity: got %+v", got)
	}

	got, err = parseAppSort("createdAt")
	if err != nil || got == nil || *got != "createdAt" {
		t.Fatalf("createdAt: got=%v err=%v", got, err)
	}

	_, err = parseAppSort("nope")
	if err == nil {
		t.Fatal("expected error for sort nope")
	}
	if !strings.Contains(err.Error(), "invalid --sort") {
		t.Fatalf("error %q should mention invalid --sort", err)
	}
}

func TestParseAppStatus(t *testing.T) {
	got, err := parseAppStatus("")
	if err != nil || got != nil {
		t.Fatalf("unset status: got=%v err=%v", got, err)
	}

	got, err = parseAppStatus("active")
	if err != nil || got == nil || *got != "active" {
		t.Fatalf("active: got=%v err=%v", got, err)
	}

	_, err = parseAppStatus("nope")
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
	got := extraListCursorFlags("demo", testGPUType, "name", "active")
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

func TestExtraStatusCursorFlag(t *testing.T) {
	if got := extraStatusCursorFlag("ready"); got != "--status ready" {
		t.Fatalf("got %q", got)
	}
	if got := extraStatusCursorFlag(""); got != "" {
		t.Fatalf("empty: got %q", got)
	}
}

func TestAppResult_IncludesConfiguration(t *testing.T) {
	gpu := testGPUType
	created := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	r := appResult{
		AppId:     testAppID,
		AppName:   "My App",
		Status:    "active",
		CreatedAt: created,
		UpdatedAt: created,
		Configuration: serverlessapi.WorkerConfig{
			ComputeType:      "gpu",
			GpuType:          &gpu,
			GpusPerWorker:    1,
			MinWorkers:       0,
			MaxWorkers:       2,
			IdleTtlSecs:      60,
			ScalingDelaySecs: 10,
			Concurrency:      1,
		},
	}
	if got := r.Headers(); len(got) != 2 || got[0] != colField || got[1] != colValue {
		t.Fatalf("headers: %v", got)
	}
	rows := r.Rows()
	createdAt := created.Format(time.RFC3339)
	want := map[string]any{
		colID:                  testAppID,
		colName:                "My App",
		colStatus:              "active",
		colCreated:             createdAt,
		colUpdated:             createdAt,
		colComputeType:         "gpu",
		colGPUType:             testGPUType,
		colFallbackGPUType:     "",
		colGPUsPerWorker:       int32(1),
		colMinWorkers:          int32(0),
		colMaxWorkers:          int32(2),
		colMinAvailableWorkers: "",
		colAvailableWorkersPct: "",
		colIdleTTL:             int32(60),
		colScalingDelay:        int32(10),
		colConcurrency:         int32(1),
	}
	got := make(map[string]any, len(rows))
	for _, row := range rows {
		got[row[0].(string)] = row[1]
	}
	if len(got) != len(want) {
		t.Fatalf("row count %d, want %d: %v", len(got), len(want), got)
	}
	for field, value := range want {
		if got[field] != value {
			t.Errorf("%s: got %#v, want %#v", field, got[field], value)
		}
	}
}

func TestPrintPage_TableWritesNextCursorToErrOut(t *testing.T) {
	next := "page-2"
	page := serverlessapi.Page[serverlessapi.App]{
		Data:       nil,
		NextCursor: &next,
	}

	var errOut bytes.Buffer
	if err := printPage(output.FormatTable, page, appsResult(page.Data), &errOut, ""); err != nil {
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
		secretAttachResult{AppID: testAppID, Name: testSecretName, EnvVarName: "BAR"},
		secretDetachResult{AppID: testAppID, Name: testSecretName},
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

func TestEnvVarsResult_ShowsValue(t *testing.T) {
	created := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	ev := serverlessapi.EnvironmentVariable{
		Key:       testEnvKey,
		Value:     testEnvValue,
		CreatedAt: &created,
		UpdatedAt: &created,
	}

	tables := []output.Tabular{
		envVarsResult{ev},
		envVarResult(ev),
	}
	for _, table := range tables {
		hasValue := false
		for _, h := range table.Headers() {
			if h == colValue {
				hasValue = true
				break
			}
		}
		if !hasValue {
			t.Fatalf("%T table must include a Value column: %v", table, table.Headers())
		}
		rows := table.Rows()
		if len(rows) != 1 {
			t.Fatalf("%T: expected 1 row, got %d", table, len(rows))
		}
		if rows[0][0] != testEnvKey || rows[0][1] != testEnvValue {
			t.Fatalf("%T: unexpected row %#v", table, rows[0])
		}
	}

	unset := envUnsetResult{AppID: testAppID, Key: testEnvKey}
	for _, h := range unset.Headers() {
		if strings.EqualFold(h, "value") {
			t.Fatalf("envUnsetResult must not include a Value column: %v", unset.Headers())
		}
	}
	rows := unset.Rows()
	if len(rows) != 1 || rows[0][0] != testAppID || rows[0][1] != testEnvKey {
		t.Fatalf("envUnsetResult: unexpected row %#v", rows)
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

func TestVersionResult_NilBuildID(t *testing.T) {
	rows := (versionResult{
		Id:            uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		AppId:         testAppID,
		VersionNumber: 1,
		CreatedAt:     time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
	}).Rows()
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	if rows[3][1] != "" {
		t.Fatalf("nil BuildId should render empty, got %#v", rows[3][1])
	}
	if rows[0][1] != int32(1) {
		t.Fatalf("version number: got %#v", rows[0][1])
	}
	if rows[2][1] != testAppID {
		t.Fatalf("appId: got %#v", rows[2][1])
	}
}

func TestParseVersionNumber(t *testing.T) {
	got, err := parseVersionNumber("1")
	if err != nil || got != 1 {
		t.Fatalf("1: got=%d err=%v", got, err)
	}

	for _, in := range []string{"foo", "1.5", "0", "-1"} {
		_, err = parseVersionNumber(in)
		if err == nil {
			t.Fatalf("expected error for %q", in)
		}
		if !strings.Contains(err.Error(), "invalid versionNumber") {
			t.Fatalf("%q: error %q should mention invalid versionNumber", in, err)
		}
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
