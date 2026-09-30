package serverless

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

func TestUsageEventParamsFromFlags(t *testing.T) {
	params, err := usageEventParamsFromFlags(usageEventFlags{
		app:    testAppID,
		from:   testUsageDate,
		to:     "2026-09-15T12:00:00+02:00",
		limit:  10,
		cursor: testLogCursor,
	})
	if err != nil {
		t.Fatalf("usageEventParamsFromFlags: %v", err)
	}
	wantFrom := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	if params.From == nil || !params.From.Equal(wantFrom) || params.To == nil || !params.To.Equal(wantTo) {
		t.Errorf("window = %v..%v", params.From, params.To)
	}
	if params.AppId == nil || *params.AppId != testAppID {
		t.Errorf("AppId = %v", params.AppId)
	}
	if params.Limit == nil || *params.Limit != 10 || params.Cursor == nil || *params.Cursor != testLogCursor {
		t.Errorf("page = limit %v cursor %v", params.Limit, params.Cursor)
	}
}

func TestUsageEventParamsFromFlags_TrimsApp(t *testing.T) {
	params, err := usageEventParamsFromFlags(usageEventFlags{app: " " + testAppID + " "})
	if err != nil {
		t.Fatalf("usageEventParamsFromFlags: %v", err)
	}
	if params == nil || params.AppId == nil || *params.AppId != testAppID {
		t.Fatalf("AppId = %v, want %s", params, testAppID)
	}
}

func TestUsageEventParamsFromFlags_Empty(t *testing.T) {
	params, err := usageEventParamsFromFlags(usageEventFlags{})
	if err != nil {
		t.Fatalf("usageEventParamsFromFlags: %v", err)
	}
	if params != nil {
		t.Errorf("params = %+v, want nil", params)
	}
}

func TestUsageEventParamsFromFlags_Rejects(t *testing.T) {
	cases := []struct {
		name  string
		flags usageEventFlags
		want  string
	}{
		{name: "blank app", flags: usageEventFlags{app: "  "}, want: "invalid --app"},
		{name: "zero from", flags: usageEventFlags{from: "0001-01-01T00:00:00Z"}, want: "zero timestamp"},
		{name: "bad from", flags: usageEventFlags{from: "not-a-date"}, want: "invalid --from"},
		{name: "from after to", flags: usageEventFlags{from: "2026-10-02", to: testUsageDate}, want: "--from must be before --to"},
		{name: "limit", flags: usageEventFlags{limit: 101}, want: "--limit must be between 1 and 100"},
	}
	for _, tc := range cases {
		_, err := usageEventParamsFromFlags(tc.flags)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}

func TestUsageEventsResult(t *testing.T) {
	gpu := testGPUType
	price := serverlessapi.MoneyAmount{Amount: "0.000767", Currency: "USD"}
	when := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	worker := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	r := usageEventsResult{
		{
			AppId:          testAppID,
			WorkerId:       worker,
			EventType:      testLogBodyReady,
			GpuCount:       1,
			GpuType:        &gpu,
			PricePerSecond: &price,
			OccurredAt:     when,
		},
		{
			AppId:      testAppID,
			WorkerId:   worker,
			EventType:  "stopped",
			GpuCount:   0,
			OccurredAt: when.Add(time.Hour),
		},
	}
	wantHeaders := []string{colTime, colApp, colWorker, colEvent, colGPUs, colGPUType, colPricePerGPU}
	if strings.Join(r.Headers(), ",") != strings.Join(wantHeaders, ",") {
		t.Fatalf("headers = %v", r.Headers())
	}
	rows := r.Rows()
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0][3] != testLogBodyReady || rows[0][4] != int32(1) || rows[0][5] != testGPUType || rows[0][6] != "0.000767 USD" {
		t.Errorf("priced row = %#v", rows[0])
	}
	if rows[1][3] != "stopped" || rows[1][5] != "" || rows[1][6] != "" {
		t.Errorf("unpriced row = %#v", rows[1])
	}
}

func TestUsageEvents_RequestsLedgerPage(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"11111111-1111-1111-1111-111111111111","appId":"my-app","workerId":"22222222-2222-2222-2222-222222222222","eventType":"ready","gpuCount":1,"gpuType":"h100","pricePerSecond":{"amount":"0.000767","currency":"USD"},"occurredAt":"2026-09-01T00:00:00Z"}],"nextCursor":"page-2"}`))
	}))
	defer srv.Close()

	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	var errOut bytes.Buffer
	cmd := newUsageEventsCmd(log.New(io.Discard))
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"--app", testAppID, "--from", testUsageDate, "--to", "2026-10-02", "--limit", "10"})
	execErr := cmd.Execute()

	_ = w.Close()
	os.Stdout = old
	var out bytes.Buffer
	_, _ = out.ReadFrom(r)

	if execErr != nil {
		t.Fatalf("usage events: %v", execErr)
	}
	if gotPath != "/v1/usage" {
		t.Errorf("path = %s", gotPath)
	}
	for _, want := range []string{"appId=my-app", "from=", "to=", "limit=10"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
	if strings.Contains(gotQuery, "cursor=") {
		t.Errorf("query %q set cursor", gotQuery)
	}
	if !strings.Contains(out.String(), testLogBodyReady) || !strings.Contains(out.String(), testGPUType) {
		t.Errorf("stdout = %q", out.String())
	}
	if !strings.Contains(errOut.String(), "--app "+testAppID) || !strings.Contains(errOut.String(), "--cursor "+testLogCursor) {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestUsageSummaryStillHitsSummary(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"from":"2026-03-15T00:00:00Z","to":"2026-03-15T12:00:00Z","calculatedAt":"2026-03-15T12:00:00Z","buckets":[],"total":{"gpuMilliseconds":0,"paygSpend":{"amount":"0.00","currency":"USD"},"paygEquivalentValue":{"amount":"0.00","currency":"USD"}}}`))
	}))
	defer srv.Close()

	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	cmd := newUsageCmd(log.New(io.Discard))
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--for", "this-month"})
	execErr := cmd.Execute()

	_ = w.Close()
	os.Stdout = old
	_, _ = io.Copy(io.Discard, r)

	if execErr != nil {
		t.Fatalf("usage: %v", execErr)
	}
	if gotPath != "/v1/usage/summary" {
		t.Fatalf("path = %s, want /v1/usage/summary", gotPath)
	}
}

func TestExtraUsageEventCursorFlags(t *testing.T) {
	got := extraUsageEventCursorFlags(usageEventFlags{
		app:   testAppID,
		from:  testUsageDate,
		to:    "2026-10-02",
		limit: 10,
	})
	want := "--app " + testAppID + " --from " + testUsageDate + " --to 2026-10-02 --limit 10"
	if got != want {
		t.Errorf("hint = %q, want %q", got, want)
	}

	padded := extraUsageEventCursorFlags(usageEventFlags{app: " " + testAppID + " "})
	if padded != "--app "+testAppID {
		t.Errorf("padded hint = %q, want trimmed app", padded)
	}
}
