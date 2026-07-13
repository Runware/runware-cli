package serverless

import (
	"github.com/spf13/cobra"
)

// newRegistryCmd returns the "serverless registry" command group.
func newRegistryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "registry",
		Short: "Manage container registries for serverless deployments",
	}
}
