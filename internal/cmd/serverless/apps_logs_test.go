package serverless

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/output"
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
	if params.Limit != nil || params.Cursor != nil {
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
}

func TestPrintLogPage_TablePrintsOldestFirstAndCursorHint(t *testing.T) {
	next := testLogCursor
	page := serverlessapi.LogEntryPage{
		Entries: []serverlessapi.LogEntry{
			{
				Time: 1750000001,
				Body: testLogBodySlow,
			},
			{
				Time: 1750000000,
				Body: testLogBodyReady,
			},
		},
		NextCursor: &next,
	}
	var out, errOut bytes.Buffer
	flags := logsFlags{
		window: testLogWindow6h,
		limit:  50,
	}
	if err := printLogPage(output.FormatTable, page, &out, &errOut, extraLogsCursorFlags(flags)); err != nil {
		t.Fatalf("printLogPage: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], testLogBodyReady) || !strings.HasSuffix(lines[1], testLogBodySlow) {
		t.Fatalf("stdout = %q", out.String())
	}
	want := "Next page: --window 6h --limit 50 --cursor " + testLogCursor
	if !strings.Contains(errOut.String(), want) {
		t.Fatalf("stderr = %q, want %q", errOut.String(), want)
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
