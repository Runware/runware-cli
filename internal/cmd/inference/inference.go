package inference

import "github.com/spf13/cobra"

// NewCmd returns the inference command with image, audio, and other inference subcommands.
func NewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inference",
		Short: "Run inference tasks",
	}
}
