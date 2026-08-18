package serverless

import (
	"fmt"
	"log/slog"
	"strconv"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

func newAppsVersionsCmd(logger *log.Logger) *cobra.Command {
	cmd := stubGroup("versions", "Inspect application versions")
	cmd.Long = "List and inspect immutable versions of a serverless application."
	cmd.AddCommand(
		newAppsVersionsListCmd(logger),
		newAppsVersionsShowCmd(logger),
	)
	return cmd
}

func newAppsVersionsListCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		cursor string
	)

	cmd := &cobra.Command{
		Use:   "list <appId>",
		Short: "List versions of a serverless application",
		Long: `List immutable versions of an application.

The Build column is empty for container-sourced versions.`,
		Example: `  # list deployed versions
  runware serverless apps versions list my-app

  # page through results
  runware serverless apps versions list my-app --limit 20 --cursor <nextCursor>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListLimit(limit); err != nil {
				return err
			}
			id := args[0]
			var params *serverlessapi.ListVersionsParams
			if limit > 0 || cursor != "" {
				params = &serverlessapi.ListVersionsParams{}
				params.Limit, params.Cursor = listPageParams(limit, cursor)
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching versions for %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.ListVersions(cmd.Context(), id, params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printPage(cmdutil.FormatFor(cmd), page, versionsResult(page.Data), cmd.ErrOrStderr(), "")
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of versions to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor")
	return cmd
}

func newAppsVersionsShowCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <appId> <versionNumber>",
		Short: "Show a version of a serverless application",
		Long:  "Show a single immutable version by number.",
		Example: `  # show a version
  runware serverless apps versions show my-app 1`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID := args[0]
			n, err := parseVersionNumber(args[1])
			if err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching version %d...", n))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			v, err := client.GetVersion(cmd.Context(), appID, n)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), versionResult(*v))
		},
	}
	return cmd
}

func parseVersionNumber(s string) (int32, error) {
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid versionNumber %q", s)
	}
	return int32(n), nil
}
