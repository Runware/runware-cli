package serverless

import (
	"fmt"
	"log/slog"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// newAppsCmd returns the "serverless apps" command group for managing
// deployed serverless applications (API: deployments).
func newAppsCmd(logger *log.Logger) *cobra.Command {
	cmd := stubGroup("apps", "Manage deployed serverless applications")
	cmd.Long = "Manage deployed serverless applications on the Runware platform."
	cmd.AddCommand(
		newAppsListCmd(logger),
		newAppsShowCmd(logger),
		newAppsEndpointsCmd(logger),
		newAppsVersionsCmd(logger),
		newAppsBuildsCmd(logger),
		newAppsLogsCmd(),
		newAppsWorkersCmd(logger),
		newAppsScaleCmd(),
		newAppsUsageCmd(),
		newAppsStopCmd(),
		newAppsResumeCmd(),
		newAppsDeleteCmd(),
	)
	return cmd
}

func newAppsListCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		cursor string
		status string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List serverless applications",
		Example: `  # list all serverless applications
  runware serverless apps list

  # filter by status
  runware serverless apps list --status active

  # page through results
  runware serverless apps list --limit 20 --cursor <nextCursor>`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListLimit(limit); err != nil {
				return err
			}
			var params *serverlessapi.ListDeploymentsParams
			if limit > 0 || cursor != "" || status != "" {
				params = &serverlessapi.ListDeploymentsParams{}
				params.Limit, params.Cursor = listPageParams(limit, cursor)
				if status != "" {
					s := serverlessapi.DeploymentStatus(status)
					if !s.Valid() {
						return fmt.Errorf("invalid --status %q (want active, initializing, stopping, stopped, deleting, deleted, or failed)", status)
					}
					params.Status = &s
				}
			}

			spin := cmdutil.NewSpinner("Fetching applications...")
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.ListDeployments(cmd.Context(), params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printPage(cmdutil.FormatFor(cmd), page, deploymentsResult(page.Data), cmd.ErrOrStderr(), "")
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of applications to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (active, initializing, stopped, …)")

	return cmd
}

func newAppsShowCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "show <appId>",
		Short: "Show details for a serverless application",
		Example: `  # show details for an application
  runware serverless apps show my-app`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching application %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			dep, err := client.GetDeployment(cmd.Context(), id)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), deploymentResult(*dep))
		},
	}
}

func newAppsEndpointsCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		cursor string
	)

	cmd := &cobra.Command{
		Use:   "endpoints <appId>",
		Short: "List endpoints for a serverless application",
		Example: `  # list endpoints for an application
  runware serverless apps endpoints my-app

  # page through results
  runware serverless apps endpoints my-app --limit 20 --cursor <nextCursor>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListLimit(limit); err != nil {
				return err
			}
			id := args[0]
			var params *serverlessapi.ListEndpointsParams
			if limit > 0 || cursor != "" {
				params = &serverlessapi.ListEndpointsParams{}
				params.Limit, params.Cursor = listPageParams(limit, cursor)
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching endpoints for %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.ListEndpoints(cmd.Context(), id, params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printPage(cmdutil.FormatFor(cmd), page, endpointsResult(page.Data), cmd.ErrOrStderr(), "")
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of endpoints to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor")
	return cmd
}

func newAppsVersionsCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		cursor string
	)

	cmd := &cobra.Command{
		Use:   "versions <appId>",
		Short: "List versions of a serverless application",
		Example: `  # list deployed versions
  runware serverless apps versions my-app

  # page through results
  runware serverless apps versions my-app --limit 20 --cursor <nextCursor>`,
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

func newAppsLogsCmd() *cobra.Command {
	return stubLeaf(
		"logs <appId>",
		"Show logs for a serverless application",
		`  # fetch or stream logs for an application
  runware serverless apps logs my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsWorkersCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		cursor string
		status string
	)

	cmd := &cobra.Command{
		Use:   "workers <appId>",
		Short: "List workers for a serverless application",
		Example: `  # list workers for an application
  runware serverless apps workers my-app

  # filter by status
  runware serverless apps workers my-app --status ready

  # page through results
  runware serverless apps workers my-app --limit 20 --cursor <nextCursor>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListLimit(limit); err != nil {
				return err
			}
			id := args[0]
			var params *serverlessapi.ListWorkersParams
			if limit > 0 || cursor != "" || status != "" {
				params = &serverlessapi.ListWorkersParams{}
				params.Limit, params.Cursor = listPageParams(limit, cursor)
				if status != "" {
					s := serverlessapi.WorkerStatus(status)
					if !s.Valid() {
						return fmt.Errorf("invalid --status %q (want pending, pulling, loading, ready, busy, draining, stopping, or stopped)", status)
					}
					params.Status = &s
				}
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching workers for %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.ListWorkers(cmd.Context(), id, params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printPage(cmdutil.FormatFor(cmd), page, workersResult(page.Data), cmd.ErrOrStderr(), "")
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of workers to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (ready, busy, pending, …)")
	return cmd
}

// validateListLimit checks --limit when set. Zero means unset (API default).
func validateListLimit(limit int) error {
	if limit == 0 {
		return nil
	}
	if limit < 1 || limit > 100 {
		return fmt.Errorf("--limit must be between 1 and 100")
	}
	return nil
}

// listPageParams builds shared limit/cursor query values for cursor-paginated list commands.
func listPageParams(limit int, cursor string) (*serverlessapi.Limit, *serverlessapi.Cursor) {
	var (
		limitOut  *serverlessapi.Limit
		cursorOut *serverlessapi.Cursor
	)
	if limit > 0 {
		l := serverlessapi.Limit(limit)
		limitOut = &l
	}
	if cursor != "" {
		c := cursor
		cursorOut = &c
	}
	return limitOut, cursorOut
}

func newAppsScaleCmd() *cobra.Command {
	return stubLeaf(
		"scale <appId>",
		"Scale a serverless application",
		`  # update scaling configuration for an application
  runware serverless apps scale my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsUsageCmd() *cobra.Command {
	return stubLeaf(
		"usage <appId>",
		"Show usage for a serverless application",
		`  # show usage events for an application
  runware serverless apps usage my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsStopCmd() *cobra.Command {
	return stubLeaf(
		"stop <appId>",
		"Stop a serverless application",
		`  # stop a running application
  runware serverless apps stop my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsResumeCmd() *cobra.Command {
	return stubLeaf(
		"resume <appId>",
		"Resume a stopped serverless application",
		`  # resume a stopped application
  runware serverless apps resume my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsDeleteCmd() *cobra.Command {
	return stubLeaf(
		"delete <appId>",
		"Delete a serverless application",
		`  # delete an application
  runware serverless apps delete my-app`,
		cobra.ExactArgs(1),
	)
}
