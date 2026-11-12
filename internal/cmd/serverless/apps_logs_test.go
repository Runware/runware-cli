package serverless

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/output"
)

const (
	logsEntriesPath = "/v1/logs/queries/runtime/entries"
	logsTailPath    = "/v1/logs/queries/runtime_tail/tail"
)

const (
	testLogWindow6h  = "6h"
	testLogCursor    = "page-2"
	testLogBodyReady = "ready"
	testLogBodySlow  = "slow"
)

func TestLogEntriesParams_MapsFlags(t *testing.T) {
	params, err := logEntriesParams(testAppID, logsFlags{
		window: testLogWindow6h,
		limit:  50,
		cursor: testLogCursor,
	})
	if err != nil {
		t.Fatalf("logEntriesParams: %v", err)
	}
	if string(params.Window) != testLogWindow6h || params.Deployment == nil || *params.Deployment != testAppID {
		t.Errorf("window/deployment = %#v", params)
	}
	if params.Limit == nil || *params.Limit != 50 || params.Cursor == nil || *params.Cursor != testLogCursor {
		t.Errorf("limit/cursor = %#v", params)
	}
}

func TestLogEntriesParams_OmitsUnsetOptionalFlags(t *testing.T) {
	params, err := logEntriesParams(testAppID, logsFlags{window: "1h"})
	if err != nil {
		t.Fatalf("logEntriesParams: %v", err)
	}
	if params.Limit != nil || params.Cursor != nil || params.Sort != nil {
		t.Errorf("optional params must be nil: %#v", params)
	}
}

// badFlagsCase pairs a rejected flag set with the error text it must produce.
type badFlagsCase struct {
	flags logsFlags
	want  string
}

func TestLogEntriesParams_RejectsBadFlags(t *testing.T) {
	cases := map[string]badFlagsCase{
		"window": {
			flags: logsFlags{window: "2h"},
			want:  "invalid --window",
		},
		"limit": {
			flags: logsFlags{
				window: "1h",
				limit:  101,
			},
			want: "--limit must be between 1 and 100",
		},
		"nowindow": {
			flags: logsFlags{window: ""},
			want:  "--window is required",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := logEntriesParams(testAppID, tc.flags)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestFormatLogLine(t *testing.T) {
	level := "info"
	got := formatLogLine(serverlessapi.LogEntry{
		Time:  1750000000,
		Level: &level,
		Body:  testLogBodyReady,
	})
	if got != "2025-06-15T15:06:40Z INFO  ready" {
		t.Errorf("line = %q", got)
	}
	fields := map[string]string{severityField: "ERROR"}
	got = formatLogLine(serverlessapi.LogEntry{
		Time:   1750000000,
		Fields: &fields,
		Body:   "from fields",
	})
	if got != "2025-06-15T15:06:40Z ERROR from fields" {
		t.Errorf("line = %q", got)
	}
	got = formatLogLine(serverlessapi.LogEntry{Body: "no time, no level"})
	if got != "-                    -     no time, no level" {
		t.Errorf("line = %q", got)
	}

	level = "error"
	exception := map[string]string{"log.exception.message": "\x1b[31mdisk full\x1b[0m"}
	t.Run("ansi body", func(t *testing.T) {
		got := formatLogLine(serverlessapi.LogEntry{Body: "\x1b[31mready\x1b[0m"})
		if got != "-                    -     ready" {
			t.Fatalf("line = %q", got)
		}
	})
	t.Run("appends log.exception.message", func(t *testing.T) {
		got := formatLogLine(serverlessapi.LogEntry{
			Time:   1750000000,
			Level:  &level,
			Body:   "worker failed",
			Fields: &exception,
		})
		if got != "2025-06-15T15:06:40Z ERROR worker failed: disk full" {
			t.Fatalf("line = %q", got)
		}
	})
	t.Run("exception already in the body", func(t *testing.T) {
		got := formatLogLine(serverlessapi.LogEntry{
			Time:   1750000000,
			Level:  &level,
			Body:   "worker failed: \x1b[31mdisk full\x1b[0m",
			Fields: &exception,
		})
		if got != "2025-06-15T15:06:40Z ERROR worker failed: disk full" {
			t.Fatalf("line = %q", got)
		}
	})
	t.Run("ignores exception.message", func(t *testing.T) {
		bare := map[string]string{"exception.message": "disk full"}
		got := formatLogLine(serverlessapi.LogEntry{
			Time:   1750000000,
			Level:  &level,
			Body:   "worker failed",
			Fields: &bare,
		})
		if got != "2025-06-15T15:06:40Z ERROR worker failed" {
			t.Fatalf("line = %q", got)
		}
	})
}

func TestIsErrorLog(t *testing.T) {
	level := func(s string) *string { return &s }
	fields := func(key, value string) *map[string]string {
		m := map[string]string{key: value}
		return &m
	}
	cases := []struct {
		name  string
		entry serverlessapi.LogEntry
		keep  bool
	}{
		{name: "level error", entry: serverlessapi.LogEntry{Level: level("error")}, keep: true},
		{name: "level fatal", entry: serverlessapi.LogEntry{Level: level("fatal")}, keep: true},
		{name: "severity ERROR", entry: serverlessapi.LogEntry{Fields: fields(severityField, "ERROR")}, keep: true},
		{name: "severity FATAL", entry: serverlessapi.LogEntry{Fields: fields(severityField, "FATAL")}, keep: true},
		{name: "exception message", entry: serverlessapi.LogEntry{Fields: fields("log.exception.message", "disk full")}, keep: true},
		{name: "traceback", entry: serverlessapi.LogEntry{Body: "Traceback (most recent call last):\n  File \"app.py\""}, keep: true},
		{name: "warn", entry: serverlessapi.LogEntry{Level: level("warn"), Body: "careful"}, keep: false},
		{name: "bare exception.message", entry: serverlessapi.LogEntry{Fields: fields("exception.message", "disk full")}, keep: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isErrorLog(tc.entry); got != tc.keep {
				t.Fatalf("isErrorLog() = %v, want %v", got, tc.keep)
			}
		})
	}
}

func TestPrintLogPage_TablePrintsAPIOrderAndCursorHints(t *testing.T) {
	next := testLogCursor
	prev := "prev-cursor"
	page := serverlessapi.LogEntryPage{
		Entries: []serverlessapi.LogEntry{
			{
				Time: 1750000000,
				Body: testLogBodyReady,
			},
			{
				Time: 1750000001,
				Body: testLogBodySlow,
			},
		},
		NextCursor: &next,
		PrevCursor: &prev,
	}
	var out, errOut bytes.Buffer
	flags := logsFlags{
		window: testLogWindow6h,
		limit:  50,
		sort:   "newest",
	}
	if err := printLogPage(output.FormatTable, page, &out, &errOut, extraLogsCursorFlags(flags)); err != nil {
		t.Fatalf("printLogPage: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], testLogBodyReady) || !strings.HasSuffix(lines[1], testLogBodySlow) {
		t.Fatalf("stdout = %q", out.String())
	}
	wantNext := "Next page: --window 6h --limit 50 --sort newest --cursor " + testLogCursor
	wantPrev := "Previous page: --window 6h --limit 50 --sort newest --cursor " + prev
	if !strings.Contains(errOut.String(), wantNext) || !strings.Contains(errOut.String(), wantPrev) {
		t.Fatalf("stderr = %q, want %q and %q", errOut.String(), wantNext, wantPrev)
	}
}

func TestPrintLogPage_ErrorsCursorHint(t *testing.T) {
	next := testLogCursor
	page := serverlessapi.LogEntryPage{
		Entries:    []serverlessapi.LogEntry{{Body: "boom"}},
		NextCursor: &next,
	}
	var out, errOut bytes.Buffer
	flags := logsFlags{
		window: testLogWindow6h,
		limit:  50,
		sort:   "newest",
		errors: true,
	}
	extra := extraLogsCursorFlags(flags)
	if !strings.Contains(extra, "--errors") {
		t.Fatalf("extra flags = %q", extra)
	}
	if err := printLogPage(output.FormatTable, page, &out, &errOut, extra); err != nil {
		t.Fatalf("printLogPage: %v", err)
	}
	want := "Next page: --window 6h --limit 50 --sort newest --errors --cursor " + testLogCursor
	if !strings.Contains(errOut.String(), want) {
		t.Fatalf("stderr = %q, want %q", errOut.String(), want)
	}
}

func TestParseLogSort(t *testing.T) {
	got, err := parseLogSort("")
	if err != nil || got != nil {
		t.Fatalf("unset: got=%v err=%v", got, err)
	}
	got, err = parseLogSort("oldest")
	if err != nil || got == nil || *got != serverlessapi.LogSortOldest {
		t.Fatalf("oldest: got=%v err=%v", got, err)
	}
	got, err = parseLogSort("newest")
	if err != nil || got == nil || *got != serverlessapi.LogSortNewest {
		t.Fatalf("newest: got=%v err=%v", got, err)
	}
	_, err = parseLogSort("activity")
	if err == nil || !strings.Contains(err.Error(), "oldest or newest") {
		t.Fatalf("expected invalid --sort, got %v", err)
	}
}

func TestChronologicalLogEntries_ReversesTheDefaultNewestPage(t *testing.T) {
	newestFirst := []serverlessapi.LogEntry{
		{Body: testLogBodySlow},
		{Body: testLogBodyReady},
	}
	got := chronologicalLogEntries(newestFirst, "")
	if len(got) != 2 || got[0].Body != testLogBodyReady || got[1].Body != testLogBodySlow {
		t.Fatalf("default = %#v", got)
	}
	if newestFirst[0].Body != testLogBodySlow {
		t.Fatal("caller slice was reordered")
	}
	kept := chronologicalLogEntries(newestFirst, "newest")
	if kept[0].Body != testLogBodySlow {
		t.Fatalf("explicit newest = %#v", kept)
	}
	oldest := chronologicalLogEntries(newestFirst, "oldest")
	if oldest[0].Body != testLogBodySlow {
		t.Fatalf("explicit oldest = %#v", oldest)
	}
}

func TestLogEmitter_JSONWritesOneObjectPerLine(t *testing.T) {
	var out bytes.Buffer
	emit := logEmitter(output.FormatJSON, &out)
	level := "warn"
	if err := emit(serverlessapi.LogEntry{
		Time:  1,
		Level: &level,
		Body:  testLogBodySlow,
	}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if err := emit(serverlessapi.LogEntry{
		Time: 2,
		Body: testLogBodyReady,
	}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || lines[0] != `{"body":"slow","level":"warn","time":1}` || lines[1] != `{"body":"ready","time":2}` {
		t.Fatalf("ndjson = %q", out.String())
	}
}

func TestFollowLogs_ReconnectsAtOnceAfterALongStreamEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var attempts []time.Time
		tail := func(context.Context, func(serverlessapi.LogEntry) error) error {
			attempts = append(attempts, time.Now())
			if len(attempts) == 3 {
				cancel()
				return context.Canceled
			}
			time.Sleep(15 * time.Minute)
			return serverlessapi.ErrTailEnded
		}
		var errOut bytes.Buffer
		if err := followLogs(ctx, tail, func(serverlessapi.LogEntry) error { return nil }, &errOut); err != nil {
			t.Fatalf("followLogs: %v", err)
		}
		if len(attempts) != 3 || attempts[2].Sub(attempts[1]) != 15*time.Minute || errOut.Len() != 0 {
			t.Fatalf("attempts=%v stderr=%q", attempts, errOut.String())
		}
	})
}

func TestFollowLogs_WaitsAfterAShortStreamEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var attempts []time.Time
		tail := func(context.Context, func(serverlessapi.LogEntry) error) error {
			attempts = append(attempts, time.Now())
			if len(attempts) == 2 {
				cancel()
				return context.Canceled
			}
			return serverlessapi.ErrTailEnded
		}
		if err := followLogs(ctx, tail, func(serverlessapi.LogEntry) error { return nil }, &bytes.Buffer{}); err != nil {
			t.Fatalf("followLogs: %v", err)
		}
		if len(attempts) != 2 || attempts[1].Sub(attempts[0]) != tailReconnectDelay {
			t.Fatalf("attempts = %v", attempts)
		}
	})
}

func TestFollowLogs_WaitsBeforeReconnectingAfterStreamFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var attempts []time.Time
		tail := func(context.Context, func(serverlessapi.LogEntry) error) error {
			attempts = append(attempts, time.Now())
			if len(attempts) == 2 {
				cancel()
				return context.Canceled
			}
			return &serverlessapi.TailStreamError{Detail: "the log stream became unavailable"}
		}
		var errOut bytes.Buffer
		if err := followLogs(ctx, tail, func(serverlessapi.LogEntry) error { return nil }, &errOut); err != nil {
			t.Fatalf("followLogs: %v", err)
		}
		if len(attempts) != 2 || attempts[1].Sub(attempts[0]) != tailReconnectDelay {
			t.Fatalf("attempts = %v", attempts)
		}
		if !strings.Contains(errOut.String(), "the log stream became unavailable; reconnecting") {
			t.Fatalf("stderr = %q", errOut.String())
		}
	})
}

func TestFollowLogs_WaitsForTheStreamToOpenWhenAGatewayAnswers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var attempts []time.Time
		var emitted []string
		tail := func(_ context.Context, emit func(serverlessapi.LogEntry) error) error {
			attempts = append(attempts, time.Now())
			// Three gateway answers, as a quiet application produces, then the
			// stream opens and carries one entry.
			if len(attempts) <= 3 {
				return &serverlessapi.TailUnavailableError{
					StatusCode: http.StatusGatewayTimeout,
					Err:        errors.New("the origin did not respond in time"),
				}
			}
			if err := emit(serverlessapi.LogEntry{Body: testLogBodyReady}); err != nil {
				return err
			}
			cancel()
			return context.Canceled
		}
		var errOut bytes.Buffer
		emit := func(entry serverlessapi.LogEntry) error {
			emitted = append(emitted, entry.Body)
			return nil
		}
		if err := followLogs(ctx, tail, emit, &errOut); err != nil {
			t.Fatalf("followLogs: %v", err)
		}
		if len(attempts) != 4 {
			t.Fatalf("attempts = %v", attempts)
		}
		for i := 1; i < len(attempts); i++ {
			if attempts[i].Sub(attempts[i-1]) != tailReconnectDelay {
				t.Fatalf("attempt %d waited %v", i, attempts[i].Sub(attempts[i-1]))
			}
		}
		if len(emitted) != 1 || emitted[0] != testLogBodyReady {
			t.Fatalf("emitted = %v", emitted)
		}
		// The notice names the condition once, however long the wait lasts.
		if got := strings.Count(errOut.String(), "waiting for the stream to open"); got != 1 {
			t.Fatalf("notice count = %d, stderr = %q", got, errOut.String())
		}
		if !strings.Contains(errOut.String(), "log stream unavailable: the origin did not respond in time") {
			t.Fatalf("stderr = %q", errOut.String())
		}
	})
}

func TestFollowLogs_NoticesTheGatewayAgainAfterAStreamOpened(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		attempts := 0
		unavailable := func() error {
			return &serverlessapi.TailUnavailableError{
				StatusCode: http.StatusBadGateway,
				Err:        errors.New("bad gateway"),
			}
		}
		tail := func(context.Context, func(serverlessapi.LogEntry) error) error {
			attempts++
			switch attempts {
			case 1, 3:
				return unavailable()
			case 2:
				return serverlessapi.ErrTailEnded
			default:
				cancel()
				return context.Canceled
			}
		}
		var errOut bytes.Buffer
		if err := followLogs(ctx, tail, func(serverlessapi.LogEntry) error { return nil }, &errOut); err != nil {
			t.Fatalf("followLogs: %v", err)
		}
		if got := strings.Count(errOut.String(), "waiting for the stream to open"); got != 2 {
			t.Fatalf("notice count = %d, stderr = %q", got, errOut.String())
		}
	})
}

func TestFollowLogs_ReturnsOtherErrors(t *testing.T) {
	boom := errors.New("boom")
	tail := func(context.Context, func(serverlessapi.LogEntry) error) error { return boom }
	err := followLogs(context.Background(), tail, func(serverlessapi.LogEntry) error { return nil }, &bytes.Buffer{})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestLogsCmd_RejectsCursorWithFollow(t *testing.T) {
	cmd := newAppsLogsCmd(nil)
	cmd.SetArgs([]string{testAppID, "--follow", "--cursor", testLogCursor})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--cursor cannot be combined with --follow") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrintLogPage_LeavesTheCallerPageUntouched(t *testing.T) {
	page := serverlessapi.LogEntryPage{
		Entries: []serverlessapi.LogEntry{
			{
				Time: 2,
				Body: testLogBodySlow,
			},
			{
				Time: 1,
				Body: testLogBodyReady,
			},
		},
	}
	if err := printLogPage(output.FormatTable, page, &bytes.Buffer{}, &bytes.Buffer{}, ""); err != nil {
		t.Fatalf("printLogPage: %v", err)
	}
	if page.Entries[0].Body != testLogBodySlow {
		t.Fatalf("caller page was reordered: %#v", page.Entries)
	}
}

// executeLogs runs apps logs against baseURL. Silence keeps a stub failure
// off stderr so the test can read the command's own notes.
func executeLogs(t *testing.T, baseURL string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", baseURL)

	var out, errOut bytes.Buffer
	cmd := newAppsLogsCmd(log.New(io.Discard))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{testAppID}, args...))
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestAppsLogs_ErrorsFiltersThePage(t *testing.T) {
	// page-info is printed when the page filter is removed. This path does
	// not use the follow wrapper.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != logsEntriesPath {
			t.Errorf("path = %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"entries":[`+
			`{"time":1750000000,"level":"info","body":"page-info"},`+
			`{"time":1750000001,"level":"error","body":"page-error"},`+
			`{"time":1750000002,"level":"fatal","body":"page-fatal"},`+
			`{"time":1750000003,"level":"warn","body":"page-warn"}`+
			`],"nextCursor":"`+testLogCursor+`"}`)
	}))
	t.Cleanup(srv.Close)

	stdout, stderr, err := executeLogs(t, srv.URL, "--errors")
	if err != nil {
		t.Fatalf("apps logs --errors: %v", err)
	}
	for _, want := range []string{"page-error", "page-fatal"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, missing %q", stdout, want)
		}
	}
	for _, drop := range []string{"page-info", "page-warn"} {
		if strings.Contains(stdout, drop) {
			t.Errorf("stdout = %q, kept %q", stdout, drop)
		}
	}
	wantHint := "Next page: --window 1h --errors --cursor " + testLogCursor
	if !strings.Contains(stderr, wantHint) {
		t.Fatalf("stderr = %q, want %q", stderr, wantHint)
	}
	if strings.Contains(stderr, "no error lines") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestAppsLogs_ErrorsFiltersTheFollowStream(t *testing.T) {
	// live-info is printed when the follow wrapper is removed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case logsEntriesPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"entries":[{"time":1,"level":"info","body":"page-info"},{"time":2,"level":"error","body":"page-error"}]}`)
		case logsTailPath:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"time\":3,\"level\":\"info\",\"body\":\"live-info\"}\n\n")
			_, _ = io.WriteString(w, "data: {\"time\":4,\"level\":\"error\",\"body\":\"live-error\"}\n\n")
			_, _ = io.WriteString(w, "data: {\n\n")
		default:
			t.Errorf("path = %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	stdout, stderr, err := executeLogs(t, srv.URL, "--errors", "--follow")
	if err == nil || !strings.Contains(err.Error(), "decode entry") {
		t.Fatalf("err = %v, stdout = %q", err, stdout)
	}
	for _, want := range []string{"page-error", "live-error"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, missing %q", stdout, want)
		}
	}
	for _, drop := range []string{"page-info", "live-info"} {
		if strings.Contains(stdout, drop) {
			t.Errorf("stdout = %q, kept %q", stdout, drop)
		}
	}
	if strings.Contains(stderr, "no error lines") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestAppsLogs_ErrorsNotesWhenThePageHasNone(t *testing.T) {
	const infoPage = `{"entries":[{"time":1,"level":"info","body":"one"},{"time":2,"level":"warn","body":"two"}]}`
	want := "no error lines in the 2 fetched entries\n"

	t.Run("page", func(t *testing.T) {
		srv := serveLogEntries(t, infoPage)
		stdout, stderr, err := executeLogs(t, srv.URL, "--errors")
		if err != nil {
			t.Fatalf("apps logs --errors: %v", err)
		}
		if stdout != "" {
			t.Fatalf("stdout = %q", stdout)
		}
		if stderr != want {
			t.Fatalf("stderr = %q, want %q", stderr, want)
		}
	})

	t.Run("empty page", func(t *testing.T) {
		srv := serveLogEntries(t, `{"entries":[]}`)
		_, stderr, err := executeLogs(t, srv.URL, "--errors")
		if err != nil {
			t.Fatalf("apps logs --errors: %v", err)
		}
		if stderr != "" {
			t.Fatalf("stderr = %q", stderr)
		}
	})

	t.Run("follow", func(t *testing.T) {
		var tailed atomic.Bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case logsEntriesPath:
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, infoPage)
			case logsTailPath:
				tailed.Store(true)
				http.Error(w, "stop", http.StatusBadRequest)
			default:
				t.Errorf("path = %s", r.URL.Path)
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		stdout, stderr, err := executeLogs(t, srv.URL, "--errors", "--follow")
		if err == nil {
			t.Fatal("expected the tail request to end the command")
		}
		if !tailed.Load() {
			t.Fatal("follow did not open the stream")
		}
		if strings.Count(stderr, want) != 1 {
			t.Fatalf("stderr = %q, want one %q", stderr, want)
		}
		if stdout != "" {
			t.Fatalf("stdout = %q", stdout)
		}
	})
}

func serveLogEntries(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != logsEntriesPath {
			t.Errorf("path = %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}
