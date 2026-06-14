package config

import (
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

func newResetCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Reset configuration defaults",
		Example: `  # reset all config defaults (API key and presets are preserved)
  runware config reset`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Preserve the API key and presets.
			existing, err := config.FileConfig()
			if err != nil {
				return fmt.Errorf("failed to read config: %w", err)
			}
			cfg := &config.Config{
				APIKey:  existing.APIKey,
				Presets: existing.Presets,
				Defaults: config.Defaults{
					OutputDir: config.DefaultOutputDir,
					Format:    config.DefaultFormat,
					Transport: config.DefaultTransport,
				},
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			logger.Info("✓ Configuration reset to defaults")
			return nil
		},
	}
}
