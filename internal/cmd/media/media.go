// Package media implements the "media" command group for managing media in a
// Runware account: uploading assets for reuse and deleting them by UUID.
package media

import (
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// NewCmd returns the "media" command with upload and delete subcommands.
func NewCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "media",
		Short: "Store and delete media in your Runware account",
	}
	cmd.AddCommand(newUploadCmd(logger))
	cmd.AddCommand(newDeleteCmd(logger))
	return cmd
}
