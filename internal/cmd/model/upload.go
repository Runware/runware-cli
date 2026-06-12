package model

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// modelUploadDetail renders the final upload result as a vertical key-value table.
type modelUploadDetail struct {
	r *api.ModelUploadResult
}

func (d modelUploadDetail) Headers() []string {
	return []string{"Field", "Value"}
}

func (d modelUploadDetail) Rows() [][]any {
	return [][]any{
		{"Status", d.r.Status},
		{colAIR, orDash(d.r.AIR)},
		{"Message", orDash(d.r.Message)},
	}
}

// MarshalJSON delegates to the underlying result so JSON output contains the raw data.
func (d modelUploadDetail) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.r)
}

// MarshalYAML delegates to the underlying result so YAML output contains the raw data.
func (d modelUploadDetail) MarshalYAML() (any, error) {
	return d.r, nil
}

func newUploadCmd(logger *log.Logger) *cobra.Command {
	var flags struct {
		category             string
		name                 string
		version              string
		downloadURL          string
		architecture         string
		format               string
		modelType            string
		air                  string
		uniqueIdentifier     string
		heroImageURL         string
		tags                 []string
		shortDescription     string
		comment              string
		private              bool
		defaultCFG           float64
		defaultSteps         int
		defaultScheduler     string
		defaultStrength      float64
		defaultWeight        float64
		positiveTriggerWords string
	}

	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload a custom model to the Runware platform",
		Long: `Upload a custom model to the Runware platform.

The model file must be hosted at a publicly reachable URL (--download-url).
After submission the upload progresses through a processing pipeline
(validated, downloaded, optimized, stored) before the model becomes ready;
the command waits and reports each phase. On success the model's AIR
identifier is printed and the model can be used with 'runware run'.`,
		Example: `  # Upload a FLUX checkpoint from a hosted safetensors file
  runware model upload --category checkpoint --architecture flux1d \
    --name "My Custom Model" --version 1.0 \
    --download-url https://example.com/model.safetensors --private

  # Upload a LoRA with metadata and defaults
  runware model upload --category lora --architecture sdxl \
    --name "Style LoRA" --version 1.0 \
    --download-url https://example.com/lora.safetensors \
    --tags style,portrait --default-weight 0.8 \
    --positive-trigger-words "myStyle" --short-description "Portrait style LoRA"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			spin := cmdutil.NewSpinner("Uploading model...")
			spin.Start()

			t, err := cmdutil.NewTransport(cmd, slog.New(logger))
			if err != nil {
				spin.Stop()
				return err
			}
			defer t.Close() //nolint:errcheck

			client := api.NewClient(t, slog.New(logger))

			req := api.ModelUploadRequest{
				Category:             flags.category,
				Name:                 flags.name,
				Version:              flags.version,
				DownloadURL:          flags.downloadURL,
				Architecture:         flags.architecture,
				Format:               flags.format,
				Type:                 flags.modelType,
				AIR:                  flags.air,
				UniqueIdentifier:     flags.uniqueIdentifier,
				HeroImageURL:         flags.heroImageURL,
				Tags:                 flags.tags,
				ShortDescription:     flags.shortDescription,
				Comment:              flags.comment,
				DefaultScheduler:     flags.defaultScheduler,
				PositiveTriggerWords: flags.positiveTriggerWords,
			}
			// Optional non-string fields are only sent when the flag was set,
			// so the API applies its own defaults otherwise.
			if cmd.Flags().Changed("private") {
				req.Private = &flags.private
			}
			if cmd.Flags().Changed("default-cfg") {
				req.DefaultCFG = &flags.defaultCFG
			}
			if cmd.Flags().Changed("default-steps") {
				req.DefaultSteps = &flags.defaultSteps
			}
			if cmd.Flags().Changed("default-strength") {
				req.DefaultStrength = &flags.defaultStrength
			}
			if cmd.Flags().Changed("default-weight") {
				req.DefaultWeight = &flags.defaultWeight
			}

			result, err := client.ModelUpload(cmd.Context(), req, api.ModelUploadOptions{
				OnStatus: func(status, _ string) {
					spin.SetMessage(fmt.Sprintf("Uploading model... (%s)", status))
				},
			})
			if err != nil {
				spin.Stop()
				return err
			}

			spin.Stop()
			return output.Print(cmdutil.FormatFor(cmd), modelUploadDetail{r: result})
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.category, "category", "c", "", "Model category: checkpoint, lora, lycoris, vae, embeddings")
	f.StringVar(&flags.name, "name", "", "Display name of the model")
	f.StringVar(&flags.version, "version", "", "Version identifier (e.g. 1.0)")
	f.StringVar(&flags.downloadURL, "download-url", "", "Publicly reachable URL hosting the model file")
	f.StringVarP(&flags.architecture, "architecture", "a", "", "Model architecture (e.g. flux1d, sdxl, sd1x)")
	f.StringVar(&flags.format, "format", "safetensors", "Model file format")
	f.StringVarP(&flags.modelType, "type", "t", "", "Model type (checkpoint: base, inpainting; lora/embeddings: positive, negative)")
	f.StringVar(&flags.air, "air", "", "Custom AIR identifier (provider:model@version)")
	f.StringVar(&flags.uniqueIdentifier, "unique-identifier", "", "Custom unique identifier for the model")
	f.StringVar(&flags.heroImageURL, "hero-image-url", "", "URL of a display image for the model")
	f.StringSliceVar(&flags.tags, "tags", nil, "Comma-separated categorical labels")
	f.StringVar(&flags.shortDescription, "short-description", "", "Brief model summary")
	f.StringVar(&flags.comment, "comment", "", "Internal notes")
	f.BoolVar(&flags.private, "private", false, "Restrict access to your organization only")
	f.Float64Var(&flags.defaultCFG, "default-cfg", 0, "Default CFG scale used when running the model")
	f.IntVar(&flags.defaultSteps, "default-steps", 0, "Default step count used when running the model")
	f.StringVar(&flags.defaultScheduler, "default-scheduler", "", "Default scheduler used when running the model")
	f.Float64Var(&flags.defaultStrength, "default-strength", 0, "Default strength for image-to-image (0-1)")
	f.Float64Var(&flags.defaultWeight, "default-weight", 0, "Default weight (lora, lycoris)")
	f.StringVar(&flags.positiveTriggerWords, "positive-trigger-words", "", "Activation keywords (lora, lycoris)")

	for _, name := range []string{"category", "name", "version", "download-url", "architecture"} {
		if err := cmd.MarkFlagRequired(name); err != nil {
			panic(err)
		}
	}

	cmd.RegisterFlagCompletionFunc("category", func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{"checkpoint", "lora", "lycoris", "vae", "embeddings"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.RegisterFlagCompletionFunc("format", func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{"safetensors"}, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}
