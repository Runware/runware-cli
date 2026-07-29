package serverless

import "github.com/spf13/cobra"

func newUsageCmd() *cobra.Command {
	return stubLeaf(
		"usage",
		"Show serverless usage and billing events",
		`  # list usage events for the authenticated organisation
  runware serverless usage`,
		cobra.NoArgs,
	)
}
