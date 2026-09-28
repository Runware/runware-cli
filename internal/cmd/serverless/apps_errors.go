package serverless

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// appErrorStatusClasses lists the accepted --status-class values.
const appErrorStatusClasses = "4xx or 5xx"

// appErrorWindowDefault is the listAppErrors window when --window is omitted.
const appErrorWindowDefault = "24h"

// appErrorFlags is the flag set of apps errors.
type appErrorFlags struct {
	window      string
	statusClass string
	limit       int
	cursor      string
}

func newAppsErrorsCmd(logger *log.Logger) *cobra.Command {
	var flags appErrorFlags

	cmd := &cobra.Command{
		Use:   "errors <appId>",
		Short: "List failed requests for a serverless application",
		Long: `List failed inference requests for an application, newest first.

These are 4xx and 5xx responses. The control-plane audit trail is apps events;
worker stdout is apps logs. --window defaults to the last 24 hours. Omit
--status-class to include both 4xx and 5xx.

Replay a cursor with the same --window and --status-class.`,
		Example: `  # failed requests in the last 24 hours
  runware serverless apps errors my-app

  # server errors in the last hour
  runware serverless apps errors my-app --window 1h --status-class 5xx

  # page through results
  runware serverless apps errors my-app --window 24h --limit 50 --cursor <nextCursor>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			params, err := appErrorParams(args[0], flags)
			if err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching errors for %s...", args[0]))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.ListAppErrors(cmd.Context(), args[0], params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printAppErrors(cmdutil.FormatFor(cmd), page, cmd.OutOrStdout(), cmd.ErrOrStderr(), extraAppErrorCursorFlags(flags))
		},
	}

	cmd.Flags().StringVar(&flags.window, "window", appErrorWindowDefault, "Time window to search ("+logWindows+")")
	cmd.Flags().StringVar(&flags.statusClass, "status-class", "", "Error class to include ("+appErrorStatusClasses+"; default both)")
	cmd.Flags().IntVar(&flags.limit, "limit", 0, "Maximum number of errors to return (1-100)")
	cmd.Flags().StringVar(&flags.cursor, "cursor", "", "Pagination cursor from a previous nextCursor (reuse the same --window/--status-class/--limit)")
	return cmd
}

// appErrorParams validates the page flags and maps them onto listAppErrors.
// window is always sent. statusClass is omitted when unset, which asks for both 4xx and 5xx.
func appErrorParams(appID string, flags appErrorFlags) (*serverlessapi.ListAppErrorsParams, error) {
	if err := validateUsageAppID(appID); err != nil {
		return nil, err
	}
	if err := validateListLimit(flags.limit); err != nil {
		return nil, err
	}
	window, err := parseValidFlag[serverlessapi.ListAppErrorsParamsWindow]("--window", flags.window, logWindows)
	if err != nil {
		return nil, err
	}
	if window == nil {
		return nil, fmt.Errorf("--window is required (want %s)", logWindows)
	}
	statusClass, err := parseValidFlag[serverlessapi.ListAppErrorsParamsStatusClass]("--status-class", flags.statusClass, appErrorStatusClasses)
	if err != nil {
		return nil, err
	}
	params := &serverlessapi.ListAppErrorsParams{
		Window:      window,
		StatusClass: statusClass,
	}
	params.Limit, params.Cursor = listPageParams(flags.limit, flags.cursor)
	return params, nil
}

func extraAppErrorCursorFlags(flags appErrorFlags) string {
	parts := appendFlag(nil, "--window", flags.window)
	parts = appendFlag(parts, "--status-class", flags.statusClass)
	if flags.limit > 0 {
		parts = appendFlag(parts, "--limit", fmt.Sprint(flags.limit))
	}
	return strings.Join(parts, " ")
}

// printAppErrors prints one page. Table format is one log line per failed
// request. JSON and YAML are the API page.
func printAppErrors(format output.Format, page serverlessapi.Page[serverlessapi.LogEntry], out, errOut io.Writer, extraCursorFlags string) error {
	switch format {
	case output.FormatJSON, output.FormatYAML:
		return output.Print(format, page)
	default:
		for _, entry := range page.Data {
			if err := writeLogLine(out, entry); err != nil {
				return err
			}
		}
		return printNextCursor(errOut, page.NextCursor, extraCursorFlags)
	}
}
