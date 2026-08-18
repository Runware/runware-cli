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

func newAppsEnvCmd(logger *log.Logger) *cobra.Command {
	cmd := stubGroup("env", "Manage plain-text environment variables for an application")
	cmd.Long = `Manage plain-text environment variables on a serverless application.

These are not organisation secrets. Values are returned by list and set.
Use 'serverless secrets' for encrypted secrets attached as env vars.`
	cmd.AddCommand(
		newAppsEnvListCmd(logger),
		newAppsEnvSetCmd(logger),
		newAppsEnvUnsetCmd(logger),
	)
	return cmd
}

func newAppsEnvListCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		cursor string
	)

	cmd := &cobra.Command{
		Use:   "list <appId>",
		Short: "List environment variables for a serverless application",
		Long: `List plain-text environment variables for an application, including values.

To list encrypted secrets attached to an application, use 'serverless secrets attachments'.`,
		Example: `  # list environment variables
  runware serverless apps env list my-app

  # page through results
  runware serverless apps env list my-app --limit 20 --cursor <nextCursor>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListLimit(limit); err != nil {
				return err
			}
			id := args[0]
			var params *serverlessapi.ListDeploymentEnvironmentVariablesParams
			if limit > 0 || cursor != "" {
				params = &serverlessapi.ListDeploymentEnvironmentVariablesParams{}
				params.Limit, params.Cursor = listPageParams(limit, cursor)
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching environment variables for %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.ListDeploymentEnvironmentVariables(cmd.Context(), id, params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printPage(cmdutil.FormatFor(cmd), page, envVarsResult(page.Data), cmd.ErrOrStderr(), "")
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of environment variables to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor")
	return cmd
}

func newAppsEnvSetCmd(logger *log.Logger) *cobra.Command {
	var (
		value     string
		valueFile string
	)

	cmd := &cobra.Command{
		Use:   "set <appId> <key>",
		Short: "Create or update an environment variable",
		Long: `Create or update one plain-text environment variable.

Prefer --value-file so the value is not visible in process lists; use
--value-file - to read from stdin.

The server rejects (HTTP 422) reserved platform names, names that collide
with an attached secret's injected env var, and adding a binding past the
100-variable-plus-secret ceiling. Overwriting an existing key is always
allowed.`,
		Example: `  # set an environment variable
  runware serverless apps env set my-app MY_KEY --value hello

  # read the value from a file
  runware serverless apps env set my-app MY_KEY --value-file ./value.txt

  # read the value from stdin
  printf '%s' "$MY_VALUE" | runware serverless apps env set my-app MY_KEY --value-file -`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := args[0]
			key := args[1]
			v, err := readValueFlag(value, valueFile, cmd.InOrStdin())
			if err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Saving environment variable %s...", key))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			ev, err := client.UpdateDeploymentEnvironmentVariable(cmd.Context(), app, key, serverlessapi.EnvironmentVariableUpdate{
				Value: v,
			})
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), envVarResult(*ev))
		},
	}

	cmd.Flags().StringVar(&value, "value", "", "Variable value (visible in process lists; prefer --value-file)")
	cmd.Flags().StringVar(&valueFile, "value-file", "", "Read variable value from a file, or - for stdin")
	cmd.MarkFlagsMutuallyExclusive("value", "value-file")
	cmd.MarkFlagsOneRequired("value", "value-file")
	return cmd
}

func newAppsEnvUnsetCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "unset <appId> <key>",
		Short: "Remove an environment variable",
		Long:  "Remove one plain-text environment variable from an application.",
		Example: `  # remove an environment variable
  runware serverless apps env unset my-app MY_KEY`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := args[0]
			key := args[1]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Removing environment variable %s...", key))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			if err := client.DeleteDeploymentEnvironmentVariable(cmd.Context(), app, key); err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), envUnsetResult{
				DeploymentID: app,
				Key:          key,
			})
		},
	}
}
