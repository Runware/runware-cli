package serverless

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/output"
)

func TestAppErrorParams(t *testing.T) {
	params, err := appErrorParams(testAppID, appErrorFlags{
		window:      "1h",
		statusClass: "5xx",
		limit:       50,
		cursor:      testLogCursor,
	})
	if err != nil {
		t.Fatalf("appErrorParams: %v", err)
	}
	if params.Window == nil || string(*params.Window) != "1h" {
		t.Errorf("window = %v", params.Window)
	}
	if params.StatusClass == nil || string(*params.StatusClass) != "5xx" {
		t.Errorf("statusClass = %v", params.StatusClass)
	}
	if params.Limit == nil || *params.Limit != 50 || params.Cursor == nil || *params.Cursor != testLogCursor {
		t.Errorf("page = limit %v cursor %v", params.Limit, params.Cursor)
	}
}

func TestAppErrorParams_DefaultOmitsStatusClass(t *testing.T) {
	params, err := appErrorParams(testAppID, appErrorFlags{window: appErrorWindowDefault})
	if err != nil {
		t.Fatalf("appErrorParams: %v", err)
	}
	if params.Window == nil || string(*params.Window) != appErrorWindowDefault {
		t.Errorf("window = %v", params.Window)
	}
	if params.StatusClass != nil || params.Limit != nil || params.Cursor != nil {
		t.Errorf("params = %+v", params)
	}
}

func TestAppErrorParams_Rejects(t *testing.T) {
	cases := []struct {
		name  string
		appID string
		flags appErrorFlags
		want  string
	}{
		{name: "blank app", appID: "  ", flags: appErrorFlags{window: appErrorWindowDefault}, want: "appId is required"},
		{name: "window", appID: testAppID, flags: appErrorFlags{window: "2h"}, want: "invalid --window"},
		{name: "empty window", appID: testAppID, flags: appErrorFlags{}, want: "--window is required"},
		{name: "status", appID: testAppID, flags: appErrorFlags{window: appErrorWindowDefault, statusClass: "2xx"}, want: "invalid --status-class"},
		{name: "limit", appID: testAppID, flags: appErrorFlags{window: appErrorWindowDefault, limit: 101}, want: "--limit must be between 1 and 100"},
	}
	for _, tc := range cases {
		_, err := appErrorParams(tc.appID, tc.flags)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}

func TestExtraAppErrorCursorFlags(t *testing.T) {
	got := extraAppErrorCursorFlags(appErrorFlags{
		window:      appErrorWindowDefault,
		statusClass: "4xx",
		limit:       50,
	})
	want := "--window " + appErrorWindowDefault + " --status-class 4xx --limit 50"
	if got != want {
		t.Errorf("hint = %q, want %q", got, want)
	}
}

func TestAppsErrors_RequestsErrorPage(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"time":1750000000,"level":"error","body":"request failed"}],"nextCursor":"page-2"}`))
	}))
	defer srv.Close()

	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)

	var out, errOut bytes.Buffer
	cmd := newAppsErrorsCmd(log.New(io.Discard))
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{testAppID, "--window", "1h", "--status-class", "5xx", "--limit", "50"})
	execErr := cmd.Execute()

	if execErr != nil {
		t.Fatalf("apps errors: %v", execErr)
	}
	if gotPath != "/v1/apps/"+testAppID+"/errors" {
		t.Errorf("path = %s", gotPath)
	}
	for _, want := range []string{"window=1h", "statusClass=5xx", "limit=50"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
	if !strings.Contains(out.String(), "request failed") || !strings.Contains(out.String(), "ERROR") {
		t.Errorf("stdout = %q", out.String())
	}
	if !strings.Contains(errOut.String(), "--window 1h") || !strings.Contains(errOut.String(), "--status-class 5xx") || !strings.Contains(errOut.String(), "--cursor "+testLogCursor) {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestPrintAppErrors_JSONIsThePage(t *testing.T) {
	next := testLogCursor
	level := "error"
	page := serverlessapi.Page[serverlessapi.LogEntry]{
		Data: []serverlessapi.LogEntry{{
			Time:  1750000000,
			Level: &level,
			Body:  "request failed",
		}},
		NextCursor: &next,
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	printErr := printAppErrors(output.FormatJSON, page, io.Discard, io.Discard, "")
	_ = w.Close()
	os.Stdout = old
	var out bytes.Buffer
	_, _ = out.ReadFrom(r)
	if printErr != nil {
		t.Fatalf("printAppErrors: %v", printErr)
	}
	if !strings.Contains(out.String(), `"data"`) || !strings.Contains(out.String(), `"nextCursor"`) || strings.Contains(out.String(), `"entries"`) {
		t.Errorf("stdout = %q", out.String())
	}
}
