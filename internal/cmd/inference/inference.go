package inference

import "github.com/spf13/cobra"

func New() *cobra.Command {
	return &cobra.Command{
		Use:   "inference",
		Short: "Run inference tasks",
	}
}
