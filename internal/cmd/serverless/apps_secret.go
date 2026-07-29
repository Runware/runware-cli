package serverless

import "github.com/spf13/cobra"

// newAppsSecretCmd returns the "serverless apps secret" command group.
func newAppsSecretCmd() *cobra.Command {
	cmd := stubGroup("secret", "Manage secrets for a serverless application")
	cmd.AddCommand(
		newAppsSecretSetCmd(),
		newAppsSecretListCmd(),
		newAppsSecretRemoveCmd(),
	)
	return cmd
}

func newAppsSecretSetCmd() *cobra.Command {
	return stubLeaf(
		"set <name>",
		"Set a secret on a serverless application",
		`  # attach or update a secret on an application
  runware serverless apps secret set my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsSecretListCmd() *cobra.Command {
	return stubLeaf(
		"list <name>",
		"List secrets for a serverless application",
		`  # list secrets attached to an application
  runware serverless apps secret list my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsSecretRemoveCmd() *cobra.Command {
	return stubLeaf(
		"remove <name>",
		"Remove a secret from a serverless application",
		`  # detach a secret from an application
  runware serverless apps secret remove my-app`,
		cobra.ExactArgs(1),
	)
}
