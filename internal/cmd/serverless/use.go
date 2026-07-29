package serverless

import "github.com/spf13/cobra"

func newUseCmd() *cobra.Command {
	return stubLeaf(
		"use <name>",
		"Set the default serverless application",
		`  # set my-app as the default deployment for subsequent commands
  runware serverless use my-app`,
		cobra.ExactArgs(1),
	)
}
