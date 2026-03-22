package cmd

import (
	"fmt"
	"runtime"

	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	RunE: func(cmd *cobra.Command, args []string) error {
		format := output.ParseFormat(getFormat())

		data := map[string]string{
			"version": version,
			"commit":  commit,
			"date":    date,
			"go":      runtime.Version(),
		}

		if format == output.FormatTable {
			fmt.Printf("runware %s\n", version)
			fmt.Printf("  commit: %s\n", commit)
			fmt.Printf("  built:  %s\n", date)
			fmt.Printf("  go:     %s\n", runtime.Version())
			return nil
		}

		return output.Print(format, data, nil, nil)
	},
}
