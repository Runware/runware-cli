package serverless

import "github.com/spf13/cobra"

func newUseCmd() *cobra.Command {
	return stubLeaf(
		"use <app>",
		"Set the default serverless application",
		`  # set my-app as the default application for subsequent commands
  runware serverless use my-app`,
		cobra.ExactArgs(1),
	)
}
