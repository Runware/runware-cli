package serverless

import "github.com/spf13/cobra"

func newInitCmd() *cobra.Command {
	return stubLeaf(
		"init [name]",
		"Initialize a new serverless project",
		`  # scaffold a new serverless project in the current directory
  runware serverless init

  # scaffold a project with a specific name
  runware serverless init my-app`,
		cobra.MaximumNArgs(1),
	)
}
