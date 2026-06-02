package cmdutil

import (
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// FormatFor resolves the output format for a command: --format flag takes
// precedence, falling back to the configured default.
func FormatFor(cmd *cobra.Command) output.Format {
	if f, _ := cmd.Root().PersistentFlags().GetString("format"); f != "" {
		return output.ParseFormat(f)
	}
	return output.ParseFormat(config.Get().Defaults.Format)
}
