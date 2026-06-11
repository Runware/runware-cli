package run

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

// NewResultCmd returns the "result" command — resume waiting for an async task
// by its taskUUID and display the result when it completes.
func NewResultCmd(logger *log.Logger) *cobra.Command {
	var flags struct {
		outputDir    string
		noDownload   bool
		pollInterval time.Duration
	}

	cmd := &cobra.Command{
		Use:   "result <taskUUID>",
		Short: "Wait for and display the result of an async task",
		Long: `Resume waiting for the result of an async task by its taskUUID.

This is useful when a long-running task (e.g. training) was submitted via
"runware run" and the CLI was interrupted before the task completed. The
taskUUID is printed when the task is first submitted.`,
		Example: `  # Wait for a training task to complete
  runware result 7fbf4fc9-5b61-461c-84a4-1e496da4debb

  # Output as JSON without downloading
  runware result 7fbf4fc9-5b61-461c-84a4-1e496da4debb -F json --no-download`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskUUID, err := uuid.Parse(args[0])
			if err != nil {
				return fmt.Errorf("invalid taskUUID %q: %w", args[0], err)
			}

			spin := cmdutil.NewSpinner("Waiting for result...")
			spin.Start()

			t, err := cmdutil.NewTransport(cmd, slog.New(logger))
			if err != nil {
				spin.Stop()
				return err
			}
			defer t.Close() //nolint:errcheck

			client := api.NewClient(t, slog.New(logger))

			results, err := client.Poll(cmd.Context(), taskUUID, flags.pollInterval, func(p int) {
				if p > 0 {
					spin.SetMessage(fmt.Sprintf("Waiting for result... %d%%", p))
					return
				}
				spin.SetMessage("Waiting for result...")
			})
			if err != nil {
				spin.Stop()
				return err
			}

			spin.Stop()

			if len(results) == 0 {
				return fmt.Errorf("no results returned for task %s", taskUUID)
			}

			return handleResults(cmd, logger, results, flags.outputDir, flags.noDownload, spin)
		},
	}

	f := cmd.Flags()
	f.StringVar(&flags.outputDir, "output-dir", config.Get().Defaults.OutputDir, "Directory to save downloaded output files")
	f.BoolVar(&flags.noDownload, "no-download", false, "Skip auto-downloading media files")
	f.DurationVar(&flags.pollInterval, "poll-interval", 2*time.Second, "Polling interval")

	return cmd
}
