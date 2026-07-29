package serverless

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDeployCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deploy [file]",
		Short: "Deploy a new serverless application",
		Long: `Deploy a new serverless application from a Python file.

The application settings come from the project configuration (via the
dashboard, or 'runware serverless init' when available).`,
		Example: `  # deploy the application in the current project
  runware serverless deploy

  # deploy a specific Python file
  runware serverless deploy ./app.py`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return fmt.Errorf("serverless deploy is not implemented yet")
		},
	}
}
