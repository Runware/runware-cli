package auth

import (
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// New returns the auth command with login, logout, and status subcommands.
func New(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
		Long:  "Login, logout, and check authentication status.",
	}
	cmd.AddCommand(newLoginCmd(logger))
	cmd.AddCommand(newLogoutCmd(logger))
	cmd.AddCommand(newStatusCmd(logger))
	return cmd
}
