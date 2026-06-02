package inference

import "github.com/spf13/cobra"

// New returns the inference command with image, audio, and other inference subcommands.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "inference",
		Short: "Run inference tasks",
	}
}
