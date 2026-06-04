package account

import (
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// NewCmd returns the account command with subcommands for account management.
func NewCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Account information, team, and API keys",
		Example: `  # show full account details
  runware account details`,
	}
	cmd.AddCommand(newDetailsCmd(logger))
	return cmd
}
