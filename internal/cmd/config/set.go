package config

import (
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newSetCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Example: `  # set the default output directory
  runware config set output_dir ~/my-images

  # set the default output format (table, json, yaml)
  runware config set format json`,
		Args: cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return []cobra.Completion{
					"output_dir",
					"format",
				}, cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			key := normalizeConfigKey(args[0])
			value := args[1]

			viper.Set(key, value)

			cfg := config.Get()
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			logger.Info("✓ Config updated", "key", key, "value", value)
			return nil
		},
	}
}
