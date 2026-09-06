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
	cmd := stubGroup("versions", "Manage application versions")
	cmd.Long = "List, inspect, activate, and delete immutable versions of a serverless application."
	cmd.AddCommand(
		newAppsVersionsListCmd(logger),
		newAppsVersionsShowCmd(logger),
		newAppsVersionsActivateCmd(logger),
		newAppsVersionsDeleteCmd(logger),
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

func newAppsVersionsActivateCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "activate <appId> <versionNumber>",
		Short: "Activate a ready application version",
		Long: `Activate a ready version by number, including rollback to an older version.

The server accepts the deploy and returns immediately with the updated app.
Worker rollout is asynchronous; this command does not wait until workers are
healthy. Re-activating the currently active version is permitted and re-applies
it. On a stopped app the version is recorded and applied on resume.

A missing app is 404. A missing version, a version that is not ready, or an
app that is deleting is 409.`,
		Example: `  # list versions, then activate one
  runware serverless apps versions list my-app
  runware serverless apps versions activate my-app 2

  # roll back to an older ready version
  runware serverless apps versions activate my-app 1`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID := args[0]
			n, err := parseVersionNumber(args[1])
			if err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Activating version %d on %s...", n, appID))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			app, err := client.DeployVersion(cmd.Context(), appID, n)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), appResult(*app))
		},
	}
}

func newAppsVersionsDeleteCmd(logger *log.Logger) *cobra.Command {
	var (
		yes   bool
		force bool
	)

	cmd := &cobra.Command{
		Use:   "delete <appId> <versionNumber>",
		Short: "Delete an unused application version",
		Long: `Delete an unused version while retaining its immutable history.

Deleted versions are omitted from version lists, return 404 from version
reads, and cannot be activated. Returns 409 while the app is deleting, or
when the version is active, is the app's only remaining version, has a
non-stopped worker, or is targeted by a live rollout. This does not remove
the version's OCI image.

Confirmation is required unless --yes or --force is passed.`,
		Example: `  # delete an unused version (prompts for confirmation)
  runware serverless apps versions delete my-app 2

  # skip the confirmation prompt
  runware serverless apps versions delete my-app 2 --yes`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID := args[0]
			n, err := parseVersionNumber(args[1])
			if err != nil {
				return err
			}
			if err := confirmDelete(
				fmt.Sprintf("version %d of application %s", n, appID),
				yes || force,
				cmd.InOrStdin(),
				cmd.ErrOrStderr(),
				stdinIsTerminal(cmd.InOrStdin()),
				config.GetAPIKey(),
			); err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Deleting version %d on %s...", n, appID))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			if err := client.DeleteVersion(cmd.Context(), appID, n); err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), versionDeletedResult{
				AppID:         appID,
				VersionNumber: n,
			})
		},
	}

	addDeleteConfirmFlags(cmd, &yes, &force)
	return cmd
}

func parseVersionNumber(s string) (int32, error) {
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid versionNumber %q", s)
	}
	return int32(n), nil
}
