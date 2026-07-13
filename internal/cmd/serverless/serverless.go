// Package serverless implements the "serverless" command group for deploying
// and managing Runware serverless applications.
package serverless

import (
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// NewCmd returns the "serverless" command group. The logger is accepted for
// uniform registration with the other command groups and will be threaded to
// leaf commands as they are added.
func NewCmd(_ *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serverless",
		Short: "Manage Runware serverless deployments",
		Long:  "Deploy, monitor, and manage Runware serverless deployments on the platform",
	}
	cmd.AddCommand(
		newRegistryCmd(),
		newAppsCmd(),
	)
	return cmd
}
