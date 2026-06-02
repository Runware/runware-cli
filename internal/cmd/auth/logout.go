package auth

import (
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

func newLogoutCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear stored credentials",
		Example: `  # clear stored credentials
  runware auth logout`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.RemoveAPIKey(); err != nil {
				return fmt.Errorf("failed to remove API key: %w", err)
			}
			logger.Info("✓ Logged out")
			return nil
		},
	}
}
