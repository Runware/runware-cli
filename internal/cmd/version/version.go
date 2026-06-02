package version

import (
	"fmt"
	"runtime"

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
			format := getFormat(cmd)

			data := map[string]string{
				"version": versionStr,
				"commit":  commit,
				"date":    date,
				"go":      runtime.Version(),
			}

			if format == output.FormatTable {
				fmt.Printf("runware %s\n", versionStr)
				fmt.Printf("  commit: %s\n", commit)
				fmt.Printf("  built:  %s\n", date)
				fmt.Printf("  go:     %s\n", runtime.Version())
				return nil
			}

			return output.Print(format, data, nil, nil)
		},
	}
}

func getFormat(cmd *cobra.Command) output.Format {
	if f, _ := cmd.Root().PersistentFlags().GetString("format"); f != "" {
		return output.ParseFormat(f)
	}
	return output.ParseFormat("")
}
