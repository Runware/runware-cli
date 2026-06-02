package account

import (
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// NewCmd returns the account command with credits and other account subcommands.
func NewCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Account information and credits",
		Example: `  # show current credit balance
  runware account credits`,
	}
	cmd.AddCommand(newCreditsCmd(logger))
	return cmd
}
