package config

import (
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

type configShowResult struct {
	APIKey    string `json:"api_key"`
	OutputDir string `json:"output_dir"`
	Format    string `json:"format"`
	Transport string `json:"transport"`
}

func (r configShowResult) Headers() []string {
	return []string{"Setting", "Value"}
}

func (r configShowResult) Rows() [][]any {
	return [][]any{
		{keyAPIKey, r.APIKey},
		{keyOutputDir, r.OutputDir},
		{keyFormat, r.Format},
		{keyTransport, r.Transport},
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

			apiKey := cfg.APIKey
			if apiKey == "" {
				apiKey = "not configured"
			} else {
				apiKey = config.MaskKey(apiKey)
			}

			return output.Print(cmdutil.FormatFor(cmd), configShowResult{
				APIKey:    apiKey,
				OutputDir: cfg.Defaults.OutputDir,
				Format:    cfg.Defaults.Format,
				Transport: cfg.Defaults.Transport,
			})
		},
	}
}
