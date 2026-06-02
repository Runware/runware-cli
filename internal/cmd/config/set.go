package config

import (
	"fmt"

	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set [key] [value]",
		Short: "Set a config value",
		Example: `  # set default model
  runware config set model "runware:100@1"

  # set default output format
  runware config set format json`,
		Args: cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return []string{
					"defaults.model",
					"defaults.width",
					"defaults.height",
					"defaults.steps",
					"defaults.cfg_scale",
					"defaults.scheduler",
					"defaults.output_dir",
					"defaults.output_format",
					"defaults.format",
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

			output.Success(fmt.Sprintf("Set %s = %s", key, value))
			return nil
		},
	}
}
