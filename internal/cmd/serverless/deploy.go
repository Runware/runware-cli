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

Application settings come from project configuration managed in the
Runware dashboard. Local project scaffolding via 'runware serverless init'
is planned and not available yet.`,
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
