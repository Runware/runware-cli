// Package serverless implements the "serverless" command group for deploying
// and managing Runware serverless applications.
package serverless

import (
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// NewCmd returns the "serverless" command group.
func NewCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serverless",
		Short: "Manage Runware serverless applications",
		Long:  "Deploy, monitor, and manage Runware serverless applications on the platform",
	}
	cmd.AddCommand(
		newInitCmd(),
		newDevCmd(),
		newDeployCmd(logger),
		newUsageCmd(),
		newGPUsCmd(logger),
		newOpenCmd(),
		newUseCmd(),
		newRegistryCmd(),
		newAppsCmd(logger),
	)
	return cmd
}
