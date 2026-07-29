package serverless

import "github.com/spf13/cobra"

func newDeployCmd() *cobra.Command {
	cmd := stubLeaf(
		"deploy [file]",
		"Deploy a new serverless application",
		`  # deploy the application in the current project
  runware serverless deploy

  # deploy a specific Python file
  runware serverless deploy ./app.py`,
		cobra.MaximumNArgs(1),
	)
	cmd.Long = `Deploy a new serverless application from a Python file.

Application settings come from project configuration managed in the
Runware dashboard. Local project scaffolding via 'runware serverless init'
is planned and not available yet.`
	return cmd
}
