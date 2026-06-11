package config

import (
	"fmt"
	"strings"

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
			key := args[0]
			value := args[1]

			if _, ok := defaultsKeys[key]; !ok {
				return fmt.Errorf("unknown config key %q: valid keys are: output_dir, format", key)
			}

			if err := validateConfigValue(key, value); err != nil {
				return err
			}

			viperKey := normalizeConfigKey(key)
			viper.Set(viperKey, value)

			cfg := config.Get()
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			logger.Info("✓ Config updated", "key", key, "value", value)
			return nil
		},
	}
}

func validateConfigValue(key, value string) error {
	switch key {
	case "format":
		switch strings.ToLower(value) {
		case "table", "json", "yaml":
		default:
			return fmt.Errorf("invalid format %q: must be one of: table, json, yaml", value)
		}
	case "output_dir":
		if value == "" {
			return fmt.Errorf("output_dir cannot be empty")
		}
	}
	return nil
}
