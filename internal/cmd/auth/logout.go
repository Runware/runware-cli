package auth

import (
	"fmt"

	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear stored credentials",
		Example: `  # clear stored credentials
  runware auth logout`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.RemoveAPIKey(); err != nil {
				return fmt.Errorf("failed to remove API key: %w", err)
			}
			output.Success("Logged out")
			return nil
		},
	}
}
