package config

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

func newSetCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Example: `  # set the default output directory
  runware config set output_dir ~/my-images

  # set the default output format (table, json, yaml)
  runware config set format json

  # set the default transport (ws, http)
  runware config set transport http`,
		Args: cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return []cobra.Completion{
					keyOutputDir,
					keyFormat,
					keyTransport,
				}, cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			validKeys := config.ValidDefaultsKeys()
			if _, ok := validKeys[key]; !ok {
				keys := make([]string, 0, len(validKeys))
				for k := range validKeys {
					keys = append(keys, k)
				}
				return fmt.Errorf("unknown config key %q: valid keys are: %s", key, strings.Join(keys, ", "))
			}

			if err := validateConfigValue(key, value); err != nil {
				return err
			}

			cfg := config.Get()
			if err := applyConfigValue(cfg, key, value); err != nil {
				return err
			}
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
	case keyFormat:
		if !output.ValidFormat(value) {
			return fmt.Errorf("invalid format %q: must be one of: %s", value, strings.Join(output.ValidFormats(), ", "))
		}
	case keyOutputDir:
		if value == "" {
			return fmt.Errorf("output_dir cannot be empty")
		}
	case keyTransport:
		if !transport.ValidTransport(value) {
			return fmt.Errorf("invalid transport %q: must be one of: %s", value, strings.Join(transport.ValidTransports(), ", "))
		}
	}
	return nil
}
