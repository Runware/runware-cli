package serverless

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/spf13/cobra"
)

const (
	testUsageDate  = "2026-09-01"
	testCurrency   = "USD"
	testPAYGSpend  = "0.231707"
	testPAYGAmount = testPAYGSpend + " " + testCurrency
)

// testUsageNow is 2026-03-15T08:30:00Z expressed in a +02:00 zone, so a range
// that forgets to convert to UTC picks a different calendar day.
var testUsageNow = time.Date(2026, 3, 15, 10, 30, 0, 0, time.FixedZone("EET", 2*60*60))

func newUsageFlagCmd() (*cobra.Command, *usageFlags) {
	flags := &usageFlags{}
	cmd := &cobra.Command{Use: "usage"}
	addUsageFlags(cmd, flags)
	return cmd, flags
}

func TestUsageParamsFromFlags_ExplicitWindow(t *testing.T) {
	cmd, flags := newUsageFlagCmd()
	args := []string{
		"--from", testUsageDate,
		"--to", "2026-09-15T12:00:00+02:00",
		testGPUTypeFlag, testGPUType,
		"--group-by", "app,day",
		"--group-by", "coverage",
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	params, err := usageParamsFromFlags(*flags, testAppID, testUsageNow)
	if err != nil {
		t.Fatalf("usageParamsFromFlags: %v", err)
	}

	wantFrom := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	if params.From == nil || !params.From.Equal(wantFrom) || params.From.Location() != time.UTC {
		t.Errorf("From = %v, want %v", params.From, wantFrom)
	}
	if params.To == nil || !params.To.Equal(wantTo) || params.To.Location() != time.UTC {
		t.Errorf("To = %v, want %v", params.To, wantTo)
	}
	if params.AppId == nil || *params.AppId != testAppID {
		t.Errorf("AppId = %v, want %s", params.AppId, testAppID)
	}
	if params.GpuType == nil || *params.GpuType != testGPUType {
		t.Errorf("GpuType = %v, want %s", params.GpuType, testGPUType)
	}
	wantGroupBy := []serverlessapi.UsageDimension{
		serverlessapi.UsageDimensionApp,
		serverlessapi.UsageDimensionDay,
		serverlessapi.UsageDimensionCoverage,
	}
	if params.GroupBy == nil || strings.Join(dimsToStrings(*params.GroupBy), ",") != strings.Join(dimsToStrings(wantGroupBy), ",") {
		t.Errorf("GroupBy = %v, want %v", params.GroupBy, wantGroupBy)
	}
}

func TestUsageParamsFromFlags_DefaultsLeaveQueryEmpty(t *testing.T) {
	params, err := usageParamsFromFlags(usageFlags{}, "", testUsageNow)
	if err != nil {
		t.Fatalf("usageParamsFromFlags: %v", err)
	}
	if params.From != nil || params.To != nil || params.AppId != nil || params.GpuType != nil || params.GroupBy != nil {
		raw, _ := json.Marshal(params)
		t.Errorf("expected no query params, got %s", raw)
	}
}

func TestUsageParamsFromFlags_ForRanges(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	cases := []struct {
		name     string
		now      time.Time
		wantFrom time.Time
		wantTo   *time.Time
	}{
		{"today", testUsageNow, day(2026, 3, 15), nil},
		{"yesterday", testUsageNow, day(2026, 3, 14), new(day(2026, 3, 15))},
		{"this-month", testUsageNow, day(2026, 3, 1), nil},
		{"last-month", testUsageNow, day(2026, 2, 1), new(day(2026, 3, 1))},
		// Year boundary: last month of January is December of the year before.
		{"last-month", day(2027, 1, 10), day(2026, 12, 1), new(day(2027, 1, 1))},
		// 1 March: "yesterday" must land on 28 February, not a phantom 29th.
		{"yesterday", day(2027, 3, 1).Add(time.Hour), day(2027, 2, 28), new(day(2027, 3, 1))},
	}
	for _, tc := range cases {
		params, err := usageParamsFromFlags(usageFlags{forRange: tc.name}, "", tc.now)
		if err != nil {
			t.Fatalf("--for %s at %s: %v", tc.name, tc.now, err)
		}
		if params.From == nil || !params.From.Equal(tc.wantFrom) {
			t.Errorf("--for %s at %s: From = %v, want %v", tc.name, tc.now, params.From, tc.wantFrom)
		}
		switch {
		case tc.wantTo == nil && params.To != nil:
			t.Errorf("--for %s at %s: To = %v, want unset", tc.name, tc.now, *params.To)
		case tc.wantTo != nil && (params.To == nil || !params.To.Equal(*tc.wantTo)):
			t.Errorf("--for %s at %s: To = %v, want %v", tc.name, tc.now, params.To, *tc.wantTo)
		}
	}
}

func TestUsageParamsFromFlags_Errors(t *testing.T) {
	cases := []struct {
		name  string
		flags usageFlags
		want  string
	}{
		{"for with from", usageFlags{forRange: "today", from: testUsageDate}, "--for cannot be combined"},
		{"for with to", usageFlags{forRange: "today", to: testUsageDate}, "--for cannot be combined"},
		{"unknown for", usageFlags{forRange: "last-week"}, `invalid --for "last-week"`},
		{"bad from", usageFlags{from: "yesterday"}, `invalid --from "yesterday"`},
		{"bad to", usageFlags{to: "2026-13-01"}, `invalid --to "2026-13-01"`},
		{"zero from", usageFlags{from: "0001-01-01T00:00:00Z"}, "--from must not be the zero timestamp"},
		{"from after to", usageFlags{from: "2026-09-02", to: testUsageDate}, "--from must be before --to"},
		{"from equals to", usageFlags{from: testUsageDate, to: testUsageDate}, "--from must be before --to"},
		{"unknown dimension", usageFlags{groupBy: []string{"region"}}, `invalid --group-by "region"`},
		{"trailing comma in dimensions", usageFlags{groupBy: []string{"app", ""}}, `invalid --group-by ""`},
		{"repeated dimension", usageFlags{groupBy: []string{"app", "day", "app"}}, `--group-by "app" given more than once`},
	}
	for _, tc := range cases {
		_, err := usageParamsFromFlags(tc.flags, "", testUsageNow)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want containing %q", tc.name, err, tc.want)
		}
	}
}

func TestValidateUsageAppID(t *testing.T) {
	cases := []struct {
		appID   string
		wantErr bool
	}{
		{testAppID, false},
		{"", true},
		{" ", true},
		{"\t\n", true},
	}
	for _, tc := range cases {
		err := validateUsageAppID(tc.appID)
		if tc.wantErr && err == nil {
			t.Errorf("validateUsageAppID(%q) = nil, want an error", tc.appID)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("validateUsageAppID(%q) = %v, want nil", tc.appID, err)
		}
	}
}

func TestUsageResult_Grouped(t *testing.T) {
	commitment := serverlessapi.UsageCoverage("commitment")
	payg := serverlessapi.UsageCoverage("payg")
	appID := testAppID
	summary := &serverlessapi.UsageSummary{
		Buckets: []serverlessapi.UsageBucket{
			{
				AppId:               &appID,
				Day:                 &openapi_types.Date{Time: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
				Coverage:            &commitment,
				GpuMilliseconds:     421000,
				PaygSpend:           serverlessapi.MoneyAmount{Amount: "0.000000", Currency: testCurrency},
				PaygEquivalentValue: serverlessapi.MoneyAmount{Amount: "0.232813", Currency: testCurrency},
			},
			{
				AppId:               &appID,
				Day:                 &openapi_types.Date{Time: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)},
				Coverage:            &payg,
				GpuMilliseconds:     419000,
				PaygSpend:           serverlessapi.MoneyAmount{Amount: testPAYGSpend, Currency: testCurrency},
				PaygEquivalentValue: serverlessapi.MoneyAmount{Amount: testPAYGSpend, Currency: testCurrency},
			},
		},
		Total: serverlessapi.UsageBucket{
			GpuMilliseconds:     840000,
			PaygSpend:           serverlessapi.MoneyAmount{Amount: testPAYGSpend, Currency: testCurrency},
			PaygEquivalentValue: serverlessapi.MoneyAmount{Amount: "0.464520", Currency: testCurrency},
		},
	}

	// Dimensions are named out of display order; columns must still come out
	// app, day, coverage.
	r := usageResult{
		summary: summary,
		dims: []serverlessapi.UsageDimension{
			serverlessapi.UsageDimensionCoverage,
			serverlessapi.UsageDimensionApp,
			serverlessapi.UsageDimensionDay,
		},
	}

	wantHeaders := []string{colApp, colDay, colCoverage, colGPUTime, colPAYGSpend, colPAYGValue}
	if strings.Join(r.Headers(), ",") != strings.Join(wantHeaders, ",") {
		t.Fatalf("headers = %v, want %v", r.Headers(), wantHeaders)
	}

	rows := r.Rows()
	if len(rows) != 3 {
		t.Fatalf("row count %d, want 2 buckets + total", len(rows))
	}
	if rows[0][0] != testAppID || rows[0][1] != testUsageDate || rows[0][2] != "commitment" {
		t.Errorf("first row key = %v", rows[0])
	}
	if rows[0][3] != "7m1s" || rows[0][4] != "0.000000 USD" || rows[0][5] != "0.232813 USD" {
		t.Errorf("first row amounts = %v", rows[0])
	}
	if rows[1][2] != "payg" || rows[1][3] != "6m59s" || rows[1][4] != testPAYGAmount {
		t.Errorf("second row = %v", rows[1])
	}
	if rows[2][0] != "Total" || rows[2][1] != "" || rows[2][2] != "" {
		t.Errorf("total row key = %v", rows[2])
	}
	if rows[2][3] != "14m0s" || rows[2][4] != testPAYGAmount || rows[2][5] != "0.464520 USD" {
		t.Errorf("total row amounts = %v", rows[2])
	}
}

func TestUsageResult_UngroupedIsTotalAlone(t *testing.T) {
	summary := &serverlessapi.UsageSummary{
		Buckets: []serverlessapi.UsageBucket{
			{
				GpuMilliseconds:     1235000,
				PaygSpend:           serverlessapi.MoneyAmount{Amount: testPAYGSpend, Currency: testCurrency},
				PaygEquivalentValue: serverlessapi.MoneyAmount{Amount: "0.682955", Currency: testCurrency},
			},
		},
		Total: serverlessapi.UsageBucket{
			GpuMilliseconds:     1235000,
			PaygSpend:           serverlessapi.MoneyAmount{Amount: testPAYGSpend, Currency: testCurrency},
			PaygEquivalentValue: serverlessapi.MoneyAmount{Amount: "0.682955", Currency: testCurrency},
		},
	}
	r := usageResult{summary: summary}

	wantHeaders := []string{colGPUTime, colPAYGSpend, colPAYGValue}
	if strings.Join(r.Headers(), ",") != strings.Join(wantHeaders, ",") {
		t.Fatalf("headers = %v, want %v", r.Headers(), wantHeaders)
	}
	rows := r.Rows()
	if len(rows) != 1 {
		t.Fatalf("row count %d, want 1", len(rows))
	}
	if rows[0][0] != "20m35s" || rows[0][1] != testPAYGAmount || rows[0][2] != "0.682955 USD" {
		t.Errorf("row = %v", rows[0])
	}
}

func TestUsageWindowLine(t *testing.T) {
	summary := &serverlessapi.UsageSummary{
		From:         time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		To:           time.Date(2026, 9, 15, 12, 0, 0, 0, time.FixedZone("EET", 2*60*60)),
		CalculatedAt: time.Date(2026, 9, 15, 10, 0, 5, 0, time.UTC),
	}
	want := "Window: 2026-09-01T00:00:00Z to 2026-09-15T10:00:00Z (end exclusive), calculated at 2026-09-15T10:00:05Z"
	if got := usageWindowLine(summary); got != want {
		t.Errorf("usageWindowLine = %q, want %q", got, want)
	}
}

func TestFormatGPUTime(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{0, "0s"},
		{500, "500ms"},
		{4000, "4s"},
		{1235000, "20m35s"},
		{3723500, "1h2m3.5s"},
		{3599999, "59m59.999s"},
		{14400000, "4h0m0s"},
		{86399999, "23h59m59.999s"},
		{86400000, "1d0h0m0s"},
		{16840000000, "194d21h46m40s"},
		// GPU time is summed per GPU, so an account-wide month reaches the
		// nanosecond range of time.Duration at a few thousand concurrent GPUs.
		// Past 9223372036854ms it wrapped negative and reported ~0 usage.
		{9223372036854, "106751d23h47m16.854s"},
		{9223372036855, "106751d23h47m16.855s"},
		{18446744073708, "213503d23h34m33.708s"},
	}
	for _, tc := range cases {
		if got := formatGPUTime(tc.ms); got != tc.want {
			t.Errorf("formatGPUTime(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

func dimsToStrings(dims []serverlessapi.UsageDimension) []string {
	out := make([]string, len(dims))
	for i, d := range dims {
		out[i] = string(d)
	}
	return out
}
