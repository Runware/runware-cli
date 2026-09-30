package serverless

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

func newAppsRenameCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <appId> <name>",
		Short: "Rename a serverless application",
		Long: `Change the display name of a serverless application.

The application ID is immutable. A name-only update records a version and does
not pin or roll workers. deploy --name still applies on create only.`,
		Example: `  # rename an application
  runware serverless apps rename my-app "Image generator"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			name := args[1]
			if err := validateAppName(name); err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Renaming application %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			app, err := client.UpdateApp(cmd.Context(), id, serverlessapi.AppUpdate{
				AppName: &name,
			})
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), appResult(*app))
		},
	}
}

func validateAppName(name string) error {
	if name == "" || strings.TrimSpace(name) != name {
		return fmt.Errorf("name must start and end with a non-whitespace character")
	}
	return nil
}
