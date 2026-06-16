package model

import (
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// NewCmd returns the model command with search and other model management subcommands.
func NewCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage and search models",
	}
	cmd.AddCommand(newSearchCmd(logger))
	cmd.AddCommand(newShowCmd(logger))
	cmd.AddCommand(newSchemaCmd(logger))
	cmd.AddCommand(newUploadCmd(logger))
	return cmd
}
