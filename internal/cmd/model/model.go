package model

import "github.com/spf13/cobra"

// NewCmd returns the model command with search and other model management subcommands.
func NewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "model",
		Short: "Manage and search models",
	}
}
