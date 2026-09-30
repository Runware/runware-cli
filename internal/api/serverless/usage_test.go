package serverless

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/runware/runware-cli/internal/api/transport"
)

const testUsageSummaryBody = `{
	"from":"2026-09-01T00:00:00Z",
	"to":"2026-10-01T00:00:00Z",
	"calculatedAt":"2026-10-01T00:00:05Z",
	"buckets":[
		{"appId":"my-app","day":"2026-09-01","gpuMilliseconds":421000,
		 "paygSpend":{"amount":"0.000000","currency":"USD"},
		 "paygEquivalentValue":{"amount":"0.232813","currency":"USD"}},
		{"appId":"my-app","day":"2026-09-02","gpuMilliseconds":419000,
		 "paygSpend":{"amount":"0.231707","currency":"USD"},
		 "paygEquivalentValue":{"amount":"0.231707","currency":"USD"}}
	],
	"total":{"gpuMilliseconds":840000,
		 "paygSpend":{"amount":"0.231707","currency":"USD"},
		 "paygEquivalentValue":{"amount":"0.464520","currency":"USD"}}
}`

func TestGetUsageSummary(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/usage/summary" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if got, err := time.Parse(time.RFC3339Nano, q.Get("from")); err != nil || !got.Equal(from) {
			t.Errorf("from query = %q, want %s", q.Get("from"), from.Format(time.RFC3339))
		}
		if got, err := time.Parse(time.RFC3339Nano, q.Get("to")); err != nil || !got.Equal(to) {
			t.Errorf("to query = %q, want %s", q.Get("to"), to.Format(time.RFC3339))
		}
		if got := q.Get("appId"); got != testAppID {
			t.Errorf("appId query = %q, want %s", got, testAppID)
		}
		if got := q.Get("gpuType"); got != testGPUType {
			t.Errorf("gpuType query = %q, want %s", got, testGPUType)
		}
		// groupBy is form style, not exploded: one comma-separated value.
		if got := q["groupBy"]; len(got) != 1 || got[0] != "app,day" {
			t.Errorf("groupBy query = %q, want [app,day]", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testUsageSummaryBody))
	}))
	defer srv.Close()

	appID := testAppID
	gpuType := testGPUType
	groupBy := []UsageDimension{UsageDimensionApp, UsageDimensionDay}
	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	summary, err := c.GetUsageSummary(context.Background(), &GetUsageSummaryParams{
		From:    &from,
		To:      &to,
		AppId:   &appID,
		GpuType: &gpuType,
		GroupBy: &groupBy,
	})
	if err != nil {
		t.Fatalf("GetUsageSummary: %v", err)
	}
	if !summary.From.Equal(from) || !summary.To.Equal(to) {
		t.Errorf("window = %s..%s, want %s..%s", summary.From, summary.To, from, to)
	}
	if len(summary.Buckets) != 2 {
		t.Fatalf("unexpected buckets: %+v", summary.Buckets)
	}
	b := summary.Buckets[1]
	if b.AppId == nil || *b.AppId != testAppID || b.Day == nil || b.Day.Format(time.DateOnly) != "2026-09-02" {
		t.Errorf("unexpected bucket key: %+v", b)
	}
	if b.GpuMilliseconds != 419000 || b.PaygSpend.Amount != "0.231707" || b.PaygSpend.Currency != "USD" {
		t.Errorf("unexpected bucket amounts: %+v", b)
	}
	if summary.Total.GpuMilliseconds != 840000 || summary.Total.PaygEquivalentValue.Amount != "0.464520" {
		t.Errorf("unexpected total: %+v", summary.Total)
	}
}

func TestGetUsageSummary_NoFiltersSendsNoQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("unexpected query %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testUsageSummaryBody))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	if _, err := c.GetUsageSummary(context.Background(), &GetUsageSummaryParams{}); err != nil {
		t.Fatalf("GetUsageSummary: %v", err)
	}
}

func TestGetUsageSummary_ProblemStatuses(t *testing.T) {
	cases := []struct {
		status int
		body   string
	}{
		{http.StatusUnprocessableEntity, `{"type":"about:blank","title":"Unprocessable Entity","status":422,"detail":"window exceeds 31 days"}`},
		{http.StatusInternalServerError, `{"type":"about:blank","title":"Internal Server Error","status":500,"detail":"usage could not be priced"}`},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))

		c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
		_, err := c.GetUsageSummary(context.Background(), &GetUsageSummaryParams{})
		srv.Close()

		var re *transport.RunwareError
		if !errors.As(err, &re) {
			t.Fatalf("status %d: expected *transport.RunwareError, got %T: %v", tc.status, err, err)
		}
		if re.StatusCode != tc.status {
			t.Errorf("status %d: RunwareError.StatusCode = %d", tc.status, re.StatusCode)
		}
	}
}

func TestGetUsageSummary_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.GetUsageSummary(context.Background(), &GetUsageSummaryParams{}); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

const testUsageEventsBody = `{
	"data":[
		{
			"id":"11111111-1111-1111-1111-111111111111",
			"appId":"my-app",
			"workerId":"22222222-2222-2222-2222-222222222222",
			"eventType":"ready",
			"gpuCount":1,
			"gpuType":"h100",
			"pricePerSecond":{"amount":"0.000767","currency":"USD"},
			"occurredAt":"2026-09-01T00:00:00Z"
		},
		{
			"id":"33333333-3333-3333-3333-333333333333",
			"appId":"my-app",
			"workerId":"22222222-2222-2222-2222-222222222222",
			"eventType":"stopped",
			"gpuCount":0,
			"occurredAt":"2026-09-01T01:00:00Z"
		}
	],
	"nextCursor":"page-2"
}`

func TestListUsageEvents(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/usage" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if got, err := time.Parse(time.RFC3339Nano, q.Get("from")); err != nil || !got.Equal(from) {
			t.Errorf("from query = %q, want %s", q.Get("from"), from.Format(time.RFC3339))
		}
		if got, err := time.Parse(time.RFC3339Nano, q.Get("to")); err != nil || !got.Equal(to) {
			t.Errorf("to query = %q, want %s", q.Get("to"), to.Format(time.RFC3339))
		}
		if got := q.Get("appId"); got != testAppID {
			t.Errorf("appId query = %q, want %s", got, testAppID)
		}
		if got := q.Get("limit"); got != "10" {
			t.Errorf("limit query = %q, want 10", got)
		}
		if got := q.Get("cursor"); got != testCursorPage2 {
			t.Errorf("cursor query = %q, want %s", got, testCursorPage2)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testUsageEventsBody))
	}))
	defer srv.Close()

	appID := testAppID
	limit := Limit(10)
	cursor := Cursor(testCursorPage2)
	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListUsageEvents(context.Background(), &ListUsageEventsParams{
		From:   &from,
		To:     &to,
		AppId:  &appID,
		Limit:  &limit,
		Cursor: &cursor,
	})
	if err != nil {
		t.Fatalf("ListUsageEvents: %v", err)
	}
	if len(page.Data) != 2 {
		t.Fatalf("events = %+v", page.Data)
	}
	first := page.Data[0]
	if first.EventType != "ready" || first.GpuCount != 1 || first.GpuType == nil || *first.GpuType != testGPUType {
		t.Errorf("first event = %+v", first)
	}
	if first.PricePerSecond == nil || first.PricePerSecond.Amount != "0.000767" {
		t.Errorf("price = %+v", first.PricePerSecond)
	}
	second := page.Data[1]
	if second.EventType != "stopped" || second.GpuType != nil || second.PricePerSecond != nil {
		t.Errorf("second event = %+v", second)
	}
	if page.NextCursor == nil || *page.NextCursor != testCursorPage2 {
		t.Errorf("nextCursor = %v", page.NextCursor)
	}
}

func TestListUsageEvents_NoFiltersSendsNoQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/usage" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("unexpected query %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListUsageEvents(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListUsageEvents: %v", err)
	}
	if len(page.Data) != 0 || page.NextCursor != nil {
		t.Errorf("page = %+v", page)
	}
}

func TestListUsageEvents_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.ListUsageEvents(context.Background(), nil); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}
