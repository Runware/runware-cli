package serverless

import "github.com/spf13/cobra"

// newRegistryCmd returns the "serverless registry" command group for managing
// container registry credentials (API: secrets with type=registry).
func newRegistryCmd() *cobra.Command {
	cmd := stubGroup("registry", "Manage container registries for serverless applications")
	cmd.Long = "Manage container registry credentials used to pull private images for serverless applications."
	cmd.AddCommand(
		newRegistryAddCmd(),
		newRegistryListCmd(),
		newRegistryRemoveCmd(),
	)
	return cmd
}

func newRegistryAddCmd() *cobra.Command {
	return stubLeaf(
		"add <name>",
		"Add a container registry",
		`  # add a container registry credential
  runware serverless registry add ghcr`,
		cobra.ExactArgs(1),
	)
}

func newRegistryListCmd() *cobra.Command {
	return stubLeaf(
		"list",
		"List configured container registries",
		`  # list registry credentials
  runware serverless registry list`,
		cobra.NoArgs,
	)
}

func newRegistryRemoveCmd() *cobra.Command {
	return stubLeaf(
		"remove <name>",
		"Remove a container registry",
		`  # remove a registry credential
  runware serverless registry remove ghcr`,
		cobra.ExactArgs(1),
	)
}
