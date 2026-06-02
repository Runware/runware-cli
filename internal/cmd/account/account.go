package account

import (
	"github.com/spf13/cobra"
)

// New returns the account command with credits and other account subcommands.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Account information and credits",
		Example: `  # show current credit balance
  runware account credits`,
	}
	cmd.AddCommand(newCreditsCmd())
	return cmd
}
