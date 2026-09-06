package serverless

import "github.com/spf13/cobra"

func newUsageCmd() *cobra.Command {
	cmd := stubLeaf(
		"usage",
		"Show account-wide usage and cost",
		`  # show account-wide usage (not available yet)
  runware serverless usage`,
		cobra.NoArgs,
	)
	cmd.Long = `Show an aggregated usage and cost report for the authenticated organisation.

This command is not implemented yet. Billing rollups are not in the API, so
there is no report to list. Raw worker-transition rows (GET /v1/usage) are
not this command.

When the report API exists, this will cover an account-wide window with the
filters that endpoint exposes.`
	return cmd
}
