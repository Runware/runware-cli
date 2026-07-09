package media

import (
	"log/slog"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// uploadResult wraps the mediaStorage upload response for display. JSON and YAML
// output the struct directly (keys match the API for easy chaining); the table
// renderer flattens it into a two-column key/value layout.
type uploadResult struct {
	MediaUUID string `json:"mediaUUID" yaml:"mediaUUID"`
	MediaURL  string `json:"mediaURL"  yaml:"mediaURL"`
	TaskUUID  string `json:"taskUUID"  yaml:"taskUUID"`
}

func (r uploadResult) Headers() []string {
	return []string{"Field", "Value"}
}

func (r uploadResult) Rows() [][]any {
	return [][]any{
		{"Media UUID", r.MediaUUID},
		{"Media URL", r.MediaURL},
		{"Task UUID", r.TaskUUID},
	}
}

// newUploadCmd returns the "media upload" command.
func newUploadCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "upload <file|url>",
		Short: "Upload media and return its UUID",
		Long: `Upload media to the Runware platform for use as input in other tasks
(e.g. image-to-image, upscaling, background removal).

The argument may be a local file path, a publicly accessible URL, or a data URI.
Local files are read and uploaded; URLs and data URIs are forwarded as-is. The
command prints the stored mediaUUID and mediaURL, which can be passed to media
parameters such as inputs.seedImage on the run command.

Supported file types: JPEG, JPG, PNG, WEBP, BMP, GIF.`,
		Example: `  # upload a local image and print its UUID
  runware media upload ./photo.jpg

  # upload a remote image by URL
  runware media upload https://example.com/photo.jpg

  # upload and use the UUID directly in a run command
  runware run runware:100@1 positivePrompt="Same scene at night" width=1024 height=1024 \
    inputs.seedImage=$(runware media upload ./photo.jpg -F json | jq -r '.mediaUUID')`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			media, err := cmdutil.BuildImageInput(args[0])
			if err != nil {
				return err
			}

			spin := cmdutil.NewSpinner("Uploading...")
			spin.Start()

			t, err := cmdutil.NewTransport(cmd, slog.New(logger))
			if err != nil {
				spin.Stop()
				return err
			}
			defer t.Close() //nolint:errcheck

			client := api.NewClient(t, slog.New(logger))
			result, err := client.MediaStorage(cmd.Context(), api.MediaOperationUpload, media)
			if err != nil {
				spin.Stop()
				return err
			}

			spin.Stop()
			return output.Print(cmdutil.FormatFor(cmd), uploadResult{
				MediaUUID: result.MediaUUID.String(),
				MediaURL:  result.MediaURL,
				TaskUUID:  result.TaskUUID.String(),
			})
		},
	}
}
