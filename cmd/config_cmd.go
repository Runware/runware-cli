package cmd

import (
	"fmt"
	"strconv"

	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print current configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Get()
		format := output.ParseFormat(getFormat())

		// Mask the API key for display
		display := *cfg
		if display.APIKey != "" {
			display.APIKey = config.MaskKey(display.APIKey)
		}
		if display.StagingKey != "" {
			display.StagingKey = config.MaskKey(display.StagingKey)
		}

		if format != output.FormatTable {
			return output.Print(format, display, nil, nil)
		}

		return output.Print(format, display,
			[]interface{}{"Setting", "Value"},
			[][]interface{}{
				{"Environment", display.Environment},
				{"API Key", display.APIKey},
				{"Mode", display.Mode},
				{"Default Model", display.Defaults.Model},
				{"Default Width", display.Defaults.Width},
				{"Default Height", display.Defaults.Height},
				{"Default Steps", display.Defaults.Steps},
				{"Default CFG Scale", display.Defaults.CFGScale},
				{"Default Scheduler", display.Defaults.Scheduler},
				{"Default Output Dir", display.Defaults.OutputDir},
				{"Default Output Format", display.Defaults.OutputFormat},
				{"Default Format", display.Defaults.Format},
			},
		)
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set [key] [value]",
	Short: "Set a config value",
	Args:  cobra.ExactArgs(2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return []string{
				"environment",
				"mode",
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
		typed, err := parseConfigValue(key, args[1])
		if err != nil {
			return err
		}

		viper.Set(key, typed)

		cfg := config.Get()
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		output.Success(fmt.Sprintf("Set %s = %v", key, typed))
		return nil
	},
}

var configResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset configuration to defaults",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := &config.Config{
			Environment: config.DefaultEnv,
			Mode:        config.DefaultMode,
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

		output.Success("Configuration reset to defaults")
		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print config file path",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(config.ConfigPath())
	},
}

// defaultsKeys are config keys that live under the "defaults" namespace.
var defaultsKeys = map[string]bool{
	"model": true, "width": true, "height": true, "steps": true,
	"cfg_scale": true, "scheduler": true, "output_dir": true,
	"output_format": true, "format": true,
}

// normalizeConfigKey maps shorthand keys like "steps" to "defaults.steps".
func normalizeConfigKey(key string) string {
	if defaultsKeys[key] {
		return "defaults." + key
	}
	return key
}

// configKeyTypes maps the normalized key path to the expected scalar type
// so `runware config set` can store typed values instead of raw strings.
var configKeyTypes = map[string]string{
	"defaults.width":     "int",
	"defaults.height":    "int",
	"defaults.steps":     "int",
	"defaults.cfg_scale": "float",
}

// parseConfigValue converts the raw CLI argument into the type viper expects
// for the given key. Unknown keys default to string.
func parseConfigValue(key, raw string) (interface{}, error) {
	switch configKeyTypes[key] {
	case "int":
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid integer for %s: %q", key, raw)
		}
		return n, nil
	case "float":
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number for %s: %q", key, raw)
		}
		return f, nil
	default:
		return raw, nil
	}
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configResetCmd)
	configCmd.AddCommand(configPathCmd)
}
