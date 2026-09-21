package serverless

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

const errorStatusClasses = "4xx or 5xx"

func newAppsErrorsCmd(logger *log.Logger) *cobra.Command {
	var (
		limit       int
		cursor      string
		window      string
		statusClass string
	)

	cmd := &cobra.Command{
		Use:   "errors <appId>",
		Short: "List failed inference requests for a serverless application",
		Long: `List failed inference requests for an application, newest first.

Omit --status-class to include both 4xx and 5xx. The cursor is only valid with
the same --window and --status-class it was issued under.`,
		Example: `  # list recent request errors
  runware serverless apps errors my-app

  # last 24 hours of 5xx only
  runware serverless apps errors my-app --window 24h --status-class 5xx

  # page through results
  runware serverless apps errors my-app --limit 50 --cursor <nextCursor>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListLimit(limit); err != nil {
				return err
			}
			windowVal, err := parseValidFlag[serverlessapi.ListAppErrorsParamsWindow]("--window", window, logWindows)
			if err != nil {
				return err
			}
			classVal, err := parseValidFlag[serverlessapi.ListAppErrorsParamsStatusClass]("--status-class", statusClass, errorStatusClasses)
			if err != nil {
				return err
			}

			id := args[0]
			var params *serverlessapi.ListAppErrorsParams
			if limit > 0 || cursor != "" || window != "" || statusClass != "" {
				params = &serverlessapi.ListAppErrorsParams{}
				params.Limit, params.Cursor = listPageParams(limit, cursor)
				params.Window = windowVal
				params.StatusClass = classVal
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching errors for %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.ListAppErrors(cmd.Context(), id, params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printPage(cmdutil.FormatFor(cmd), page, requestErrorsResult(page.Data), cmd.ErrOrStderr(), extraErrorsCursorFlags(window, statusClass))
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of errors to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor (reuse the same --window/--status-class)")
	cmd.Flags().StringVar(&window, "window", "", "Time window ("+logWindows+")")
	cmd.Flags().StringVar(&statusClass, "status-class", "", "Filter by status class ("+errorStatusClasses+")")
	return cmd
}

func extraErrorsCursorFlags(window, statusClass string) string {
	parts := appendFlag(nil, "--window", window)
	return strings.Join(appendFlag(parts, "--status-class", statusClass), " ")
}
