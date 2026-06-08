package cmdutil

import (
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// FormatFor returns the output format for a command from the --format flag.
// The flag's default is set to the user's configured default at startup
// (see NewRootCmd), so no separate config fallback is required here.
func FormatFor(cmd *cobra.Command) output.Format {
	f, _ := cmd.Root().PersistentFlags().GetString("format")
	return output.ParseFormat(f)
}
