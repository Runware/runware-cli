package config

import (
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

type configPathResult struct {
	Path string `json:"path"`
}

func (r configPathResult) Headers() []string {
	return []string{"Config Path"}
}

func (r configPathResult) Rows() [][]any {
	return [][]any{{r.Path}}
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
