package auth

import (
	"github.com/spf13/cobra"
)

// New returns the auth command with login, logout, and status subcommands.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
		Long:  "Login, logout, and check authentication status.",
	}
	cmd.AddCommand(newLoginCmd())
	cmd.AddCommand(newLogoutCmd())
	cmd.AddCommand(newStatusCmd())
	return cmd
}
