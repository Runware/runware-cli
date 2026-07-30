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
		newAppsLogsCmd(),
		newAppsWorkersCmd(logger),
		newAppsScaleCmd(),
		newAppsUsageCmd(),
		newAppsSecretCmd(),
		newAppsStopCmd(),
		newAppsResumeCmd(),
		newAppsDeleteCmd(),
	)
	return cmd
}

func newAppsListCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		status string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List serverless applications",
		Example: `  # list all serverless applications
  runware serverless apps list

  # filter by status
  runware serverless apps list --status active`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var params *serverlessapi.ListDeploymentsParams
			if limit > 0 || status != "" {
				params = &serverlessapi.ListDeploymentsParams{}
				if limit > 0 {
					l := serverlessapi.Limit(limit)
					params.Limit = &l
				}
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
			deps, err := client.ListDeployments(cmd.Context(), params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), deploymentsResult(deps))
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of applications to return (1-100)")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (active, initializing, stopped, …)")

	return cmd
}

func newAppsShowCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "show <deploymentId>",
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
	var limit int

	cmd := &cobra.Command{
		Use:   "endpoints <deploymentId>",
		Short: "List endpoints for a serverless application",
		Example: `  # list endpoints for an application
  runware serverless apps endpoints my-app`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			params := listLimitParams(limit)

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching endpoints for %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			items, err := client.ListEndpoints(cmd.Context(), id, params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), endpointsResult(items))
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of endpoints to return (1-100)")
	return cmd
}

func newAppsVersionsCmd(logger *log.Logger) *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "versions <deploymentId>",
		Short: "List versions of a serverless application",
		Example: `  # list deployed versions
  runware serverless apps versions my-app`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			var params *serverlessapi.ListVersionsParams
			if limit > 0 {
				l := serverlessapi.Limit(limit)
				params = &serverlessapi.ListVersionsParams{Limit: &l}
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching versions for %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			items, err := client.ListVersions(cmd.Context(), id, params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), versionsResult(items))
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of versions to return (1-100)")
	return cmd
}

func newAppsLogsCmd() *cobra.Command {
	return stubLeaf(
		"logs <name>",
		"Show logs for a serverless application",
		`  # fetch or stream logs for an application
  runware serverless apps logs my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsWorkersCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		status string
	)

	cmd := &cobra.Command{
		Use:   "workers <deploymentId>",
		Short: "List workers for a serverless application",
		Example: `  # list workers for an application
  runware serverless apps workers my-app

  # filter by status
  runware serverless apps workers my-app --status ready`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			var params *serverlessapi.ListWorkersParams
			if limit > 0 || status != "" {
				params = &serverlessapi.ListWorkersParams{}
				if limit > 0 {
					l := serverlessapi.Limit(limit)
					params.Limit = &l
				}
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
			items, err := client.ListWorkers(cmd.Context(), id, params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), workersResult(items))
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of workers to return (1-100)")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (ready, busy, pending, …)")
	return cmd
}

// listLimitParams builds ListEndpointsParams when --limit is set.
func listLimitParams(limit int) *serverlessapi.ListEndpointsParams {
	if limit <= 0 {
		return nil
	}
	l := serverlessapi.Limit(limit)
	return &serverlessapi.ListEndpointsParams{Limit: &l}
}

func newAppsScaleCmd() *cobra.Command {
	return stubLeaf(
		"scale <name>",
		"Scale a serverless application",
		`  # update scaling configuration for an application
  runware serverless apps scale my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsUsageCmd() *cobra.Command {
	return stubLeaf(
		"usage <name>",
		"Show usage for a serverless application",
		`  # show usage events for an application
  runware serverless apps usage my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsStopCmd() *cobra.Command {
	return stubLeaf(
		"stop <name>",
		"Stop a serverless application",
		`  # stop a running application
  runware serverless apps stop my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsResumeCmd() *cobra.Command {
	return stubLeaf(
		"resume <name>",
		"Resume a stopped serverless application",
		`  # resume a stopped application
  runware serverless apps resume my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsDeleteCmd() *cobra.Command {
	return stubLeaf(
		"delete <name>",
		"Delete a serverless application",
		`  # delete an application
  runware serverless apps delete my-app`,
		cobra.ExactArgs(1),
	)
}
