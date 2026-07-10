package media

import (
	"fmt"
	"log/slog"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// deleteResult wraps the mediaStorage delete response for display. Delete returns
// only the mediaUUID that was removed (no URL).
type deleteResult struct {
	MediaUUID string `json:"mediaUUID" yaml:"mediaUUID"`
	TaskUUID  string `json:"taskUUID"  yaml:"taskUUID"`
}

func (r deleteResult) Headers() []string {
	return []string{"Field", "Value"}
}

func (r deleteResult) Rows() [][]any {
	return [][]any{
		{"Media UUID", r.MediaUUID},
		{"Task UUID", r.TaskUUID},
	}
}

// newDeleteCmd returns the "media delete" command.
func newDeleteCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <mediaUUID>",
		Short: "Permanently delete stored media by its UUID",
		Long: `Permanently remove media previously uploaded to your Runware account,
identified by its mediaUUID. This cannot be undone.`,
		Example: `  # delete a stored asset by its UUID
  runware media delete 5f1d2c3b-8a4e-4c2a-9f1a-2b3c4d5e6f70`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := uuid.Parse(args[0]); err != nil {
				return fmt.Errorf("invalid mediaUUID %q: %w", args[0], err)
			}

			spin := cmdutil.NewSpinner("Deleting...")
			spin.Start()

			t, err := cmdutil.NewTransport(cmd, slog.New(logger))
			if err != nil {
				spin.Stop()
				return err
			}
			defer t.Close() //nolint:errcheck

			client := api.NewClient(t, slog.New(logger))
			result, err := client.MediaStorage(cmd.Context(), api.MediaOperationDelete, args[0])
			if err != nil {
				spin.Stop()
				return err
			}

			spin.Stop()
			return output.Print(cmdutil.FormatFor(cmd), deleteResult{
				MediaUUID: result.MediaUUID.String(),
				TaskUUID:  result.TaskUUID.String(),
			})
		},
	}
}
