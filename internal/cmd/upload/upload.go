// Package upload implements the "upload" command, which uploads asset files to
// the Runware platform and returns their reusable UUID.
package upload

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// uploadResult wraps the imageUpload API response for display. JSON and YAML
// output the struct directly (keys match the API for easy chaining); the table
// renderer flattens it into a two-column key/value layout.
type uploadResult struct {
	ImageUUID string `json:"imageUUID" yaml:"imageUUID"`
	TaskUUID  string `json:"taskUUID"  yaml:"taskUUID"`
}

func (r uploadResult) Headers() []string {
	return []string{"Field", "Value"}
}

func (r uploadResult) Rows() [][]any {
	return [][]any{
		{"Image UUID", r.ImageUUID},
		{"Task UUID", r.TaskUUID},
	}
}

// imageExtMIME maps the file extensions accepted by the imageUpload API to their
// MIME types, used to build a data URI for local files.
var imageExtMIME = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".gif":  "image/gif",
}

// NewCmd returns the "upload" command.
func NewCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "upload <file|url>",
		Short: "Upload an asset and return its UUID",
		Long: `Upload an image to the Runware platform for use as input in other tasks
(e.g. image-to-image, upscaling, background removal).

The argument may be a local file path, a publicly accessible URL, or a data URI.
Local files are read and uploaded; URLs and data URIs are forwarded as-is. The
command prints the uploaded imageUUID (and taskUUID), which can be passed to
image parameters such as inputs.seedImage on the run command.

Supported file types: JPEG, JPG, PNG, WEBP, BMP, GIF. Video and audio upload is
not yet supported by the API.`,
		Example: `  # upload a local image and print its UUID
  runware upload ./photo.jpg

  # upload a remote image by URL
  runware upload https://example.com/photo.jpg

  # upload and use the UUID directly in a run command
  runware run runware:100@1 positivePrompt="Same scene at night" \
    inputs.seedImage=$(runware upload ./photo.jpg -F json | jq -r '.imageUUID')`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			image, err := buildImageInput(args[0])
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
			result, err := client.UploadImage(cmd.Context(), image)
			if err != nil {
				spin.Stop()
				return err
			}

			spin.Stop()
			return output.Print(cmdutil.FormatFor(cmd), uploadResult{
				ImageUUID: result.ImageUUID.String(),
				TaskUUID:  result.TaskUUID.String(),
			})
		},
	}
}

// buildImageInput converts a CLI argument into the value accepted by the
// imageUpload "image" field. Remote URLs and data URIs are returned unchanged;
// local file paths are read, validated by extension, and encoded as a data URI.
func buildImageInput(arg string) (string, error) {
	if isRemoteOrDataURI(arg) {
		return arg, nil
	}

	info, err := os.Stat(arg)
	if err != nil {
		return "", fmt.Errorf("cannot read file %q: %w", arg, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%q is a directory, not a file", arg)
	}

	ext := strings.ToLower(filepath.Ext(arg))
	mime, ok := imageExtMIME[ext]
	if !ok {
		return "", fmt.Errorf("unsupported file type %q: supported types are JPEG, JPG, PNG, WEBP, BMP, GIF", ext)
	}

	data, err := os.ReadFile(arg) //nolint:gosec // user-supplied path is expected for a CLI upload
	if err != nil {
		return "", fmt.Errorf("cannot read file %q: %w", arg, err)
	}

	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data)), nil
}

// isRemoteOrDataURI reports whether arg should be forwarded to the API verbatim
// rather than read from disk.
func isRemoteOrDataURI(arg string) bool {
	lower := strings.ToLower(arg)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:")
}
