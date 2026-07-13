package serverless

import (
	"github.com/spf13/cobra"
)

// newAppsCmd returns the "serverless apps" command group.
func newAppsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "Manage deployed serverless applications",
	}
	cmd.AddCommand(newSecretCmd())
	return cmd
}
