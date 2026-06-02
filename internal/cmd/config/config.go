package config

import (
	"fmt"

	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration",
	}
	cmd.AddCommand(newShowCmd())
	cmd.AddCommand(newSetCmd())
	cmd.AddCommand(newResetCmd())
	cmd.AddCommand(newPathCmd())
	return cmd
}

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print current configuration",
		Example: `  # print current configuration
  runware config show`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Get()

			maskedKey := cfg.APIKey
			if maskedKey != "" {
				maskedKey = config.MaskKey(maskedKey)
			}

			return output.Print(cmdutil.FormatFor(cmd), configShowResult{
				APIKey:       maskedKey,
				Model:        cfg.Defaults.Model,
				Width:        cfg.Defaults.Width,
				Height:       cfg.Defaults.Height,
				Steps:        cfg.Defaults.Steps,
				CFGScale:     cfg.Defaults.CFGScale,
				Scheduler:    cfg.Defaults.Scheduler,
				OutputDir:    cfg.Defaults.OutputDir,
				OutputFormat: cfg.Defaults.OutputFormat,
				Format:       cfg.Defaults.Format,
			})
		},
	}
}

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

func newResetCmd() *cobra.Command {
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

			output.Success("Configuration reset to defaults")
			return nil
		},
	}
}

func newPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print config file path",
		Example: `  # print config file location
  runware config path`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return output.Print(cmdutil.FormatFor(cmd), configPathResult{
				Path: config.ConfigPath(),
			})
		},
	}
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

// configShowResult holds configuration values for display.
type configShowResult struct {
	APIKey       string  `json:"api_key"`
	Model        string  `json:"model"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	Steps        int     `json:"steps"`
	CFGScale     float64 `json:"cfg_scale"`
	Scheduler    string  `json:"scheduler"`
	OutputDir    string  `json:"output_dir"`
	OutputFormat string  `json:"output_format"`
	Format       string  `json:"format"`
}

func (r configShowResult) Headers() []string { return []string{"Setting", "Value"} }
func (r configShowResult) Rows() [][]any {
	return [][]any{
		{"API Key", r.APIKey},
		{"Default Model", r.Model},
		{"Default Width", r.Width},
		{"Default Height", r.Height},
		{"Default Steps", r.Steps},
		{"Default CFG Scale", r.CFGScale},
		{"Default Scheduler", r.Scheduler},
		{"Default Output Dir", r.OutputDir},
		{"Default Output Format", r.OutputFormat},
		{"Default Format", r.Format},
	}
}

// configPathResult holds the config file path for display.
type configPathResult struct {
	Path string `json:"path"`
}

func (r configPathResult) Headers() []string { return []string{"Config Path"} }
func (r configPathResult) Rows() [][]any     { return [][]any{{r.Path}} }
