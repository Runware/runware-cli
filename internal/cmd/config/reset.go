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
		Short: "Reset configuration to defaults",
		Example: `  # reset all config to defaults
  runware config reset`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := &config.Config{
				Defaults: config.Defaults{
					Model:        config.DefaultModel,
					Width:        config.DefaultWidth,
					Height:       config.DefaultHeight,
					Steps:        config.DefaultSteps,
					CFGScale:     config.DefaultCFGScale,
					Scheduler:    config.DefaultScheduler,
					OutputDir:    config.DefaultOutputDir,
					OutputFormat: config.DefaultOutputFmt,
					Format:       config.DefaultFormat,
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
