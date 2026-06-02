package version

import (
	"runtime"

	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	versionStr = "dev"
	commit     = "none"
	date       = "unknown"
)

func SetVersionInfo(v, c, d string) {
	versionStr = v
	commit = c
	date = d
}

func New() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Example: `  # print version information
  runware version`,
		RunE: func(cmd *cobra.Command, args []string) error {
			data := map[string]string{
				"version": versionStr,
				"commit":  commit,
				"date":    date,
				"go":      runtime.Version(),
			}

			return output.Print(cmdutil.FormatFor(cmd), data, &output.Table{
				Headers: []string{"Field", "Value"},
				Rows: [][]any{
					{"version", versionStr},
					{"commit", commit},
					{"built", date},
					{"go", runtime.Version()},
				},
			})
		},
	}
}
