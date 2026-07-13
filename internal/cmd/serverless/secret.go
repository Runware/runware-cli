package serverless

import (
	"github.com/spf13/cobra"
)

// newSecretCmd returns the "serverless apps secret" command group.
func newSecretCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "secret",
		Short: "Manage secrets for a serverless application",
	}
}
