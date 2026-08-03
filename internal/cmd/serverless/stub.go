package serverless

import (
	"fmt"

	"github.com/spf13/cobra"
)

// notImplemented prints command help and returns a clear stub error. Use for
// leaf commands that are registered but not yet wired to the API.
func notImplemented(cmd *cobra.Command) error {
	_ = cmd.Help()
	return fmt.Errorf("%s is not implemented yet", cmd.CommandPath())
}

// stubLeaf returns a runnable leaf command that shows help and reports that it
// is not implemented. Use for scaffolding the CLI surface ahead of API wiring.
func stubLeaf(use, short, example string, args cobra.PositionalArgs) *cobra.Command {
	return &cobra.Command{
		Use:     use,
		Short:   short,
		Example: example,
		Args:    args,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return notImplemented(cmd)
		},
	}
}

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
