package serverless

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// tailReconnectDelay separates two connection attempts after the server
// reports a stream failure, or after a clean end of a stream that lived
// shorter than this, so a server that closes at once is not hammered.
const tailReconnectDelay = 2 * time.Second

// logWindows lists the accepted --window values, in the order they are documented.
const logWindows = "1h, 6h, 24h, 7d, or 30d"

// logTailer opens one live log stream and hands each entry to emit until the
// stream ends or ctx is cancelled.
type logTailer func(ctx context.Context, emit func(serverlessapi.LogEntry) error) error

// logsFlags is the flag set of apps logs.
type logsFlags struct {
	window string
	limit  int
	cursor string
	follow bool
}

func newAppsLogsCmd(logger *log.Logger) *cobra.Command {
	var flags logsFlags

	cmd := &cobra.Command{
		Use:   "logs <appId>",
		Short: "Show or follow logs for a serverless application",
		Long: `Show recent application logs, oldest first, and optionally follow new ones.

The recent page is read from the runtime log query over --window (default 1h),
and --limit and --cursor page through it. With --follow the command prints the
recent page, then streams new entries until interrupted; the stream reconnects
when the server ends it. The live stream has no window, so --window, --limit
and --cursor apply to the recent page only, and --cursor cannot be combined
with --follow. Entries written between the recent page and the start of the
stream, or while the stream reconnects, can be missed or repeated.

In table format each entry is one line: time, level and message. In json or
yaml format the recent page is printed as one document; with --follow every
entry is printed as one JSON object per line.`,
		Example: `  # show the last hour of logs
  runware serverless apps logs my-app

  # show the last six hours
  runware serverless apps logs my-app --window 6h

  # follow new log entries until Ctrl-C
  runware serverless apps logs my-app --follow

  # page through older entries
  runware serverless apps logs my-app --limit 50 --cursor <nextCursor>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID := args[0]
			params, err := logEntriesParams(appID, flags)
			if err != nil {
				return err
			}
			if flags.follow && flags.cursor != "" {
				return fmt.Errorf("--cursor cannot be combined with --follow")
			}
			format := cmdutil.FormatFor(cmd)
			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching logs for %s...", appID))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.GetLogEntries(cmd.Context(), serverlessapi.LogQueryRuntime, params)
			spin.Stop()
			if err != nil {
				if flags.follow && cmd.Context().Err() != nil {
					return nil //nolint:nilerr // Ctrl-C ends a follow normally, before the stream as well as during it.
				}
				return err
			}

			if !flags.follow {
				return printLogPage(format, page, out, errOut, extraLogsCursorFlags(flags))
			}

			emit := logEmitter(format, out)
			for _, entry := range slices.Backward(page.Entries) {
				if err := emit(entry); err != nil {
					return err
				}
			}
			tail := func(ctx context.Context, emit func(serverlessapi.LogEntry) error) error {
				return client.TailLogs(ctx, serverlessapi.LogQueryRuntimeTail, appID, emit)
			}
			return followLogs(cmd.Context(), tail, emit, errOut)
		},
	}

	cmd.Flags().StringVar(&flags.window, "window", "1h", "Time window for the recent page ("+logWindows+")")
	cmd.Flags().IntVar(&flags.limit, "limit", 0, "Maximum number of entries on the recent page (1-100, default 20)")
	cmd.Flags().StringVar(&flags.cursor, "cursor", "", "Pagination cursor from a previous nextCursor")
	cmd.Flags().BoolVarP(&flags.follow, "follow", "f", false, "Stream new log entries until interrupted")
	return cmd
}

// logEntriesParams validates the page flags locally and maps them onto the API.
func logEntriesParams(appID string, flags logsFlags) (serverlessapi.GetLogEntriesParams, error) {
	if err := validateListLimit(flags.limit); err != nil {
		return serverlessapi.GetLogEntriesParams{}, err
	}
	window, err := parseValidFlag[serverlessapi.LogWindow]("--window", flags.window, logWindows)
	if err != nil {
		return serverlessapi.GetLogEntriesParams{}, err
	}
	if window == nil {
		return serverlessapi.GetLogEntriesParams{}, fmt.Errorf("--window is required (want %s)", logWindows)
	}
	params := serverlessapi.GetLogEntriesParams{
		Window:     *window,
		Deployment: &appID,
	}
	params.Limit, params.Cursor = listPageParams(flags.limit, flags.cursor)
	return params, nil
}

// extraLogsCursorFlags repeats the filters a next-page --cursor is bound to.
func extraLogsCursorFlags(flags logsFlags) string {
	parts := appendFlag(nil, "--window", flags.window)
	if flags.limit > 0 {
		parts = appendFlag(parts, "--limit", fmt.Sprint(flags.limit))
	}
	return strings.Join(parts, " ")
}

// printLogPage prints one page, oldest first: as a document in json or yaml,
// as one line per entry in table format.
func printLogPage(format output.Format, page serverlessapi.LogEntryPage, out, errOut io.Writer, extraCursorFlags string) error {
	page.Entries = slices.Clone(page.Entries)
	slices.Reverse(page.Entries)
	switch format {
	case output.FormatJSON, output.FormatYAML:
		return output.Print(format, page)
	default:
		for _, entry := range page.Entries {
			if err := writeLogLine(out, entry); err != nil {
				return err
			}
		}
		return printNextCursor(errOut, page.NextCursor, extraCursorFlags)
	}
}

// logEmitter returns the per-entry writer a live stream uses for format.
func logEmitter(format output.Format, out io.Writer) func(serverlessapi.LogEntry) error {
	switch format {
	case output.FormatJSON, output.FormatYAML:
		return func(entry serverlessapi.LogEntry) error {
			line, err := json.Marshal(entry)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(out, "%s\n", line)
			return err
		}
	default:
		return func(entry serverlessapi.LogEntry) error {
			return writeLogLine(out, entry)
		}
	}
}

// followLogs keeps a live stream open until ctx is cancelled. A clean end of a
// long-lived stream reconnects at once; a reported stream failure, or a clean
// end of a short-lived stream, reconnects after tailReconnectDelay. Any other
// error is returned. Cancellation is a normal exit.
func followLogs(ctx context.Context, tail logTailer, emit func(serverlessapi.LogEntry) error, errOut io.Writer) error {
	for {
		started := time.Now()
		err := tail(ctx, emit)
		if ctx.Err() != nil {
			return nil //nolint:nilerr // Cancellation is the normal way a follow ends.
		}
		_, streamFailed := errors.AsType[*serverlessapi.TailStreamError](err)
		switch {
		case streamFailed:
			_, _ = fmt.Fprintf(errOut, "%v; reconnecting\n", err)
		case errors.Is(err, serverlessapi.ErrTailEnded):
			if time.Since(started) >= tailReconnectDelay {
				continue
			}
		default:
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(tailReconnectDelay):
		}
	}
}

// writeLogLine renders one entry as "time level message".
func writeLogLine(out io.Writer, entry serverlessapi.LogEntry) error {
	_, err := fmt.Fprintln(out, formatLogLine(entry))
	return err
}

func formatLogLine(entry serverlessapi.LogEntry) string {
	ts := "-"
	if entry.Time != 0 {
		ts = time.Unix(entry.Time, 0).UTC().Format(time.RFC3339)
	}
	level := "-"
	if l := entryLevel(entry); l != "" {
		level = strings.ToUpper(l)
	}
	return fmt.Sprintf("%-20s %-5s %s", ts, level, entry.Body)
}

// severityField is the structured-log field the store keeps the level under
// when the API leaves the level member empty.
const severityField = "log.severity_text"

func entryLevel(entry serverlessapi.LogEntry) string {
	if entry.Level != nil && *entry.Level != "" {
		return *entry.Level
	}
	if entry.Fields != nil {
		return (*entry.Fields)[severityField]
	}
	return ""
}
