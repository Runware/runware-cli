package config

import (
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

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

func (r configShowResult) Headers() []string {
	return []string{"Setting", "Value"}
}

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
