package model

import "github.com/spf13/cobra"

// New returns the model command with search and other model management subcommands.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "model",
		Short: "Manage and search models",
	}
}
