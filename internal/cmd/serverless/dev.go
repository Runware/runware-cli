package serverless

import "github.com/spf13/cobra"

func newDevCmd() *cobra.Command {
	return stubLeaf(
		"dev [file]",
		"Run a serverless application locally for development",
		`  # run the project entrypoint locally
  runware serverless dev

  # run a specific Python file
  runware serverless dev ./app.py`,
		cobra.MaximumNArgs(1),
	)
}
