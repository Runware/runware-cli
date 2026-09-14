package serverless

import "github.com/spf13/cobra"

// stubGroup returns a parent command that prints its own help when invoked
// with no subcommand, so it appears under Available Commands and in docs.
func stubGroup(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
}
