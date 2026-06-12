package run

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

// NewCmd returns the "run" command — a unified inference gateway that accepts
// any model AIR identifier and a set of key=value parameter pairs, validates
// them against the model's runtime JSON Schema, and submits the request to the
// Runware inference API.
func NewCmd(logger *log.Logger) *cobra.Command {
	var flags struct {
		preset         string
		taskType       string
		outputDir      string
		noDownload     bool
		deliveryMethod string
		pollInterval   time.Duration
		validate       bool
	}

	cmd := &cobra.Command{
		Use:   "run <model> [key=value ...]",
		Short: "Run an inference request against any Runware model",
		Long: `Run an inference request against any Runware model.

The model is identified by its AIR (AI Resource) identifier. Parameters are
passed as key=value pairs. The model's JSON Schema is fetched automatically to
coerce parameter types and determine the task type.

The API validates the request, so parameters are not checked against the schema
by default. Pass --validate to additionally enforce the schema's required and
conditional constraints client-side before submitting.

If the schema cannot determine the task type (e.g. for community or custom
fine-tuned models), specify it explicitly with --task-type.

When --preset is provided, the preset's model and parameters are used as
defaults. Any key=value arguments on the command line override the preset.
The model positional argument may be omitted when --preset supplies one.`,
		Example: `  # Image generation
  runware run runware:101@1 positivePrompt="A serene mountain landscape" width=1024 height=1024

  # Text inference (LLM)
  runware run minimax:m3@0 messages.0.role=user messages.0.content="Explain quantum computing"

  # Text inference — multi-turn conversation
  runware run minimax:m3@0 messages.0.role=user messages.0.content="What is Go?" messages.1.role=assistant messages.1.content="A compiled language." messages.2.role=user messages.2.content="How do I install it?"

  # Video generation
  runware run klingai:5@3 positivePrompt="Ocean waves at sunset" width=1920 height=1080 duration=10

  # 3D inference — text to 3D
  runware run tencent:hunyuan-3d@3.1-pro positivePrompt="A red vintage sports car"

  # 3D inference — image to 3D
  runware run tencent:hunyuan-3d@3.1-pro inputs.images.0="https://example.com/product.jpg"

  # Audio inference
  runware run elevenlabs:1@1 positivePrompt="Upbeat electronic dance music with driving bass and synth leads" duration=30
  runware run minimax:speech@2.8 speech.text="Hello, this is a text-to-speech example." speech.voice=English_expressive_narrator

  # Community model — task type must be specified explicitly
  runware run civitai:305149@392545 --task-type imageInference positivePrompt="A portrait" width=1024 height=1024

  # Upscale
  runware run runware:35@2 settings.confidence=0.45 settings.maxDetections=3 settings.maskPadding=14 settings.maskBlur=4 inputs.image="https://assets.runware.ai/assets/inputs/38837c23-1b72-4322-8465-ec950f83e2ad.jpg"

  # Remove background (inspect returned fields/URLs)
  runware run runware:110@1 --task-type removeBackground --format json --no-download inputs.image="https://assets.runware.ai/assets/inputs/8e540ecf-ef5e-4a70-b07a-dc73ffd827a2.jpg"

  # Caption (inspect returned fields/URLs)
  runware run memories:1@1 --task-type caption --format json --no-download inputs.video="https://assets.runware.ai/assets/inputs/42b64dcb-3c21-4b50-83ab-779d338dde47.mp4"

  # Load a saved preset, overriding individual params
  runware run --preset portrait positivePrompt="Sunset over the ocean"

  # Save output to a specific directory
  runware run runware:101@1 positivePrompt="Abstract art" --output-dir ./my-images width=1024 height=1024

  # Output as JSON without downloading
  runware run runware:101@1 positivePrompt="Abstract art" --format json --no-download width=1024 height=1024`,
		// Model positional arg is required unless --preset supplies one.
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				if p, _ := cmd.Flags().GetString("preset"); p == "" {
					return fmt.Errorf("requires at least 1 arg(s), only received 0")
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			model, kvArgs := cmdutil.SplitModelArgs(args)

			if flags.preset != "" {
				p := config.GetPreset(flags.preset)
				if p == nil {
					return fmt.Errorf("preset %q not found", flags.preset)
				}
				// Preset supplies the model if it was not given as a positional arg.
				if model == "" {
					model = p.Model
				}
				// Merge params: preset provides defaults, CLI key=value args override.
				merged, err := mergePresetParams(p.Params, kvArgs)
				if err != nil {
					return err
				}
				// Rebuild kvArgs as a sorted slice for deterministic behaviour.
				kvArgs = make([]string, 0, len(merged))
				for _, k := range slices.Sorted(maps.Keys(merged)) {
					kvArgs = append(kvArgs, k+"="+merged[k])
				}
			}

			if model == "" {
				return fmt.Errorf("model is required: provide as first argument or via --preset")
			}

			spin := cmdutil.NewSpinner("Running task...")
			spin.Start()

			t, err := cmdutil.NewTransport(cmd, slog.New(logger))
			if err != nil {
				spin.Stop()
				return err
			}
			defer t.Close() //nolint:errcheck

			client := api.NewClient(t, slog.New(logger))

			results, err := client.Run(cmd.Context(), model, kvArgs, api.RunOptions{
				TaskType:       flags.taskType,
				DeliveryMethod: flags.deliveryMethod,
				PollInterval:   flags.pollInterval,
				OnProgress:     runProgress(spin),
				Validate:       flags.validate,
			})
			if err != nil {
				spin.Stop()
				return err
			}

			spin.Stop()

			if len(results) == 0 {
				return fmt.Errorf("no results returned from API")
			}

			return handleResults(cmd, logger, results, flags.outputDir, flags.noDownload, spin)
		},
		// Dynamic completion: when the model arg is already typed, suggest
		// key= completions from the schema's properties.
		ValidArgsFunction: schemaArgCompleter,
	}

	f := cmd.Flags()
	f.StringVar(&flags.preset, "preset", "", "Load parameters from a saved preset (model and params used as defaults)")
	f.StringVar(&flags.taskType, "task-type", "", "Override the detected task type (e.g. imageInference, videoInference, textInference, audioInference, 3dInference)")
	f.StringVar(&flags.outputDir, "output-dir", config.Get().Defaults.OutputDir, "Directory to save downloaded output files")
	f.BoolVar(&flags.noDownload, "no-download", false, "Skip auto-downloading media files (imageURL, videoURL, audioURL, outputs.files[].url)")
	f.StringVar(&flags.deliveryMethod, "delivery-method", string(api.DeliveryMethodAsync), "Delivery method (sync or async)")
	f.DurationVar(&flags.pollInterval, "poll-interval", 2*time.Second, "Polling interval when delivery method is async")
	f.BoolVar(&flags.validate, "validate", false, "Validate parameters against the model schema before submitting (off by default; the API validates the request)")

	//nolint:errcheck,gosec
	cmd.RegisterFlagCompletionFunc("preset", func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		config.Init() //nolint:errcheck,gosec
		return config.ListPresets(), cobra.ShellCompDirectiveNoFileComp
	})

	//nolint:errcheck,gosec
	cmd.RegisterFlagCompletionFunc("task-type", func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return []cobra.Completion{
			taskTypeImage,
			taskTypeVideo,
			taskTypeAudio,
			taskTypeText,
			taskType3D,
		}, cobra.ShellCompDirectiveNoFileComp
	})

	//nolint:errcheck,gosec
	cmd.RegisterFlagCompletionFunc("delivery-method", func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return []cobra.Completion{
			string(api.DeliveryMethodSync),
			string(api.DeliveryMethodAsync),
		}, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

// mergePresetParams merges preset params (as defaults) with CLI key=value args
// (which take priority). Returns an error for any CLI arg that is not in
// key=value form or has an empty key, matching the validation in schema.ParseKV
// so that --preset runs fail consistently with non-preset runs.
func mergePresetParams(presetParams map[string]string, kvArgs []string) (map[string]string, error) {
	merged := make(map[string]string, len(presetParams)+len(kvArgs))
	for k, v := range presetParams {
		merged[k] = v
	}
	for _, kv := range kvArgs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("invalid argument %q: must be in key=value form", kv)
		}
		if k == "" {
			return nil, fmt.Errorf("invalid argument %q: key must not be empty", kv)
		}
		merged[k] = v
	}
	return merged, nil
}

func runProgress(spin *cmdutil.Spinner) func(p int) {
	const baseMsg = "Waiting for result..."

	return func(p int) {
		if p > 0 {
			spin.SetMessage(fmt.Sprintf("%s %d%%", baseMsg, p))
			return
		}

		spin.SetMessage(baseMsg)
	}
}
