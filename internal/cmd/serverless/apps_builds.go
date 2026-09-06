package serverless

import (
	"fmt"
	"log/slog"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

func newAppsBuildsCmd(logger *log.Logger) *cobra.Command {
	cmd := stubGroup("builds", "Inspect application builds")
	cmd.Long = "List, inspect, and delete code builds and container validations for a serverless application."
	cmd.AddCommand(
		newAppsBuildsListCmd(logger),
		newAppsBuildsShowCmd(logger),
		newAppsBuildsDeleteCmd(logger),
	)
	return cmd
}

func newAppsBuildsListCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		cursor string
	)

	cmd := &cobra.Command{
		Use:   "list <appId>",
		Short: "List builds for a serverless application",
		Long: `List code builds and container validations for an application.

The table omits log tail; use 'builds show' for error detail and log tail.`,
		Example: `  # list builds for an application
  runware serverless apps builds list my-app

  # page through results
  runware serverless apps builds list my-app --limit 20 --cursor <nextCursor>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListLimit(limit); err != nil {
				return err
			}
			id := args[0]
			var params *serverlessapi.ListBuildsParams
			if limit > 0 || cursor != "" {
				params = &serverlessapi.ListBuildsParams{}
				params.Limit, params.Cursor = listPageParams(limit, cursor)
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching builds for %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.ListBuilds(cmd.Context(), id, params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printPage(cmdutil.FormatFor(cmd), page, buildsResult(page.Data), cmd.ErrOrStderr(), "")
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of builds to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor")
	return cmd
}

func newAppsBuildsShowCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <appId> <buildId>",
		Short: "Show a build for a serverless application",
		Long: `Show a single build, including status, error, and log tail.

Log tail is the trailing snapshot returned by the API; live streaming is not
supported.`,
		Example: `  # show a build
  runware serverless apps builds show my-app 33333333-3333-3333-3333-333333333333`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID := args[0]
			buildID, err := uuid.Parse(args[1])
			if err != nil {
				return fmt.Errorf("invalid buildId %q: %w", args[1], err)
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching build %s...", buildID))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			b, err := client.GetBuild(cmd.Context(), appID, buildID)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printBuild(cmd, *b)
		},
	}
	return cmd
}

func newAppsBuildsDeleteCmd(logger *log.Logger) *cobra.Command {
	var (
		yes   bool
		force bool
	)

	cmd := &cobra.Command{
		Use:   "delete <appId> <buildId>",
		Short: "Delete or cancel an application build",
		Long: `Cancel a queued or running build, or delete a terminal build.

Cancelling a queued or running build records it as superseded and ends its
current rollout without activating it, so any previous version keeps serving.
A terminal build can be deleted once no live rollout still needs it. Ready
builds remain while a version references them (409).

Confirmation is required unless --yes or --force is passed.`,
		Example: `  # cancel or delete a build (prompts for confirmation)
  runware serverless apps builds delete my-app 33333333-3333-3333-3333-333333333333

  # skip the confirmation prompt
  runware serverless apps builds delete my-app 33333333-3333-3333-3333-333333333333 --yes`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID := args[0]
			buildID, err := uuid.Parse(args[1])
			if err != nil {
				return fmt.Errorf("invalid buildId %q: %w", args[1], err)
			}
			if err := confirmDelete(
				fmt.Sprintf("build %s of application %s", buildID, appID),
				yes || force,
				cmd.InOrStdin(),
				cmd.ErrOrStderr(),
				stdinIsTerminal(cmd.InOrStdin()),
				config.GetAPIKey(),
			); err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Deleting build %s...", buildID))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			if err := client.DeleteBuild(cmd.Context(), appID, buildID); err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), buildDeletedResult{
				AppID:   appID,
				BuildID: buildID.String(),
			})
		},
	}

	addDeleteConfirmFlags(cmd, &yes, &force)
	return cmd
}

func printBuild(cmd *cobra.Command, b serverlessapi.Build) error {
	format := cmdutil.FormatFor(cmd)
	if err := output.Print(format, buildResult(b)); err != nil {
		return err
	}
	if format != output.FormatTable {
		return nil
	}
	if b.LogTail == nil || *b.LogTail == "" {
		return nil
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "\nLog tail:\n%s\n", *b.LogTail)
	return err
}
