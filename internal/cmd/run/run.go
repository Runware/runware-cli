package run

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

// NewCmd returns the "run" command — a unified inference gateway that accepts
// any model AIR identifier and a set of key=value parameter pairs, validates
// them against the model's runtime JSON Schema, and submits the request to the
// Runware inference API.
func NewCmd(logger *log.Logger) *cobra.Command {
	var flags struct {
		taskType       string
		outputDir      string
		noDownload     bool
		deliveryMethod string
		pollInterval   time.Duration
	}

	cmd := &cobra.Command{
		Use:   "run <model> [key=value ...]",
		Short: "Run an inference request against any Runware model",
		Long: `Run an inference request against any Runware model.

The model is identified by its AIR (AI Resource) identifier. Parameters are
passed as key=value pairs. The model's JSON Schema is fetched automatically to
validate inputs and determine the task type.

If the schema cannot determine the task type (e.g. for community or custom
fine-tuned models), specify it explicitly with --task-type.`,
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

  # Save output to a specific directory
  runware run runware:101@1 positivePrompt="Abstract art" --output-dir ./my-images width=1024 height=1024

  # Output as JSON without downloading
  runware run runware:101@1 positivePrompt="Abstract art" --format json --no-download width=1024 height=1024`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			model := args[0]
			kvArgs := args[1:]

			spin := spinner.New(spinner.CharSets[14], 100*time.Millisecond,
				spinner.WithWriter(os.Stderr))

			// --- 1. Fetch the model schema ---
			spin.Suffix = " Fetching schema..."
			spin.Start()

			schema, schemaErr := api.FetchModelSchema(cmd.Context(), model)
			if schemaErr != nil {
				spin.Stop()
				// Without a schema we cannot auto-detect the task type.
				// If the caller provided --task-type we can still proceed without validation.
				if flags.taskType == "" {
					return fmt.Errorf("could not fetch schema for %q: %w; use --task-type to specify the task type and skip validation", model, schemaErr)
				}
				logger.Warn("schema unavailable; skipping validation", "model", model, "err", schemaErr)
				spin.Start()
			}

			// --- 2. Validate the request ---
			spin.Suffix = " Validating request..."

			var reqSchema schemaNode
			if schema != nil {
				if err := json.Unmarshal(schema.RequestSchema, &reqSchema); err != nil {
					spin.Stop()
					return fmt.Errorf("failed to parse request schema: %w", err)
				}
			}

			// --- 3. Determine task type ---
			taskType := flags.taskType
			if taskType == "" {
				detected, ok := extractTaskType(reqSchema)
				if !ok {
					spin.Stop()
					return fmt.Errorf("could not detect task type for model %q; use --task-type to specify it (run 'runware model schema %s' to inspect the schema)", model, model)
				}
				taskType = detected
			}

			// --- 4. Parse key=value arguments into payload ---
			payload := map[string]any{
				fieldModel: model,
			}
			for _, kv := range kvArgs {
				path, v, err := parseKV(kv, reqSchema)
				if err != nil {
					spin.Stop()
					return fmt.Errorf("invalid argument %q: %w", kv, err)
				}
				if hint, blocked := protectedFields[path[0]]; blocked {
					spin.Stop()
					return fmt.Errorf("argument %q: key %q is reserved — %s", kv, path[0], hint)
				}
				deepSet(payload, path, v)
			}

			// --- 5. Validate required fields against schema ---
			if schema != nil {
				if err := validateRequired(reqSchema, payload); err != nil {
					spin.Stop()
					return err
				}
				if err := validateAllOf(reqSchema, payload); err != nil {
					spin.Stop()
					return err
				}
			}

			// --- 5a. Resolve and inject delivery method ---
			deliveryMethod := resolveDeliveryMethod(flags.deliveryMethod, payload, reqSchema)
			if deliveryMethod != "" {
				payload[fieldDeliveryMethod] = deliveryMethod
			}

			// --- 6. Inject system fields ---
			taskUUID := uuid.New()
			payload[fieldTaskType] = taskType
			payload[fieldTaskUUID] = taskUUID

			// --- 7. Connect and submit ---
			spin.Suffix = " Submitting request..."

			t, err := cmdutil.NewTransport(cmd, slog.New(logger))
			if err != nil {
				spin.Stop()
				return err
			}
			defer t.Close() //nolint:errcheck

			client := api.NewClient(t, slog.New(logger))

			// Submit the task. For sync delivery the response already contains
			// the completed results. For async it is an acknowledgment only.
			initialResults, err := client.Run(cmd.Context(), payload)
			if err != nil {
				spin.Stop()
				return err
			}

			// --- 7a. Collect results: poll for async, use initial response for sync ---
			var results []json.RawMessage

			if strings.EqualFold(deliveryMethod, deliveryMethodAsync) {
				spin.Suffix = " Waiting for result..."

				results, err = client.Poll(cmd.Context(), taskUUID, flags.pollInterval, func(p int) {
					spin.Suffix = fmt.Sprintf(" Waiting for result... %d%%", p)
				})

				if err != nil {
					spin.Stop()
					return err
				}
			} else {
				results = initialResults
			}

			spin.Stop()

			if len(results) == 0 {
				return fmt.Errorf("no results returned from API")
			}

			// --- 8. Output and optionally download media ---
			return handleResults(cmd, logger, results, flags.outputDir, flags.noDownload, spin)
		},
		// Dynamic completion: when the model arg is already typed, suggest
		// key= completions from the schema's properties.
		ValidArgsFunction: schemaArgCompleter,
	}

	f := cmd.Flags()
	f.StringVar(&flags.taskType, "task-type", "", "Override the detected task type (e.g. imageInference, videoInference, textInference, audioInference, 3dInference)")
	f.StringVar(&flags.outputDir, "output-dir", "./outputs", "Directory to save downloaded output files")
	f.BoolVar(&flags.noDownload, "no-download", false, "Skip auto-downloading media files (imageURL, videoURL, audioURL, outputs.files[].url)")
	f.StringVar(&flags.deliveryMethod, "delivery-method", "", "Override delivery method (sync or async); default taken from model schema")
	f.DurationVar(&flags.pollInterval, "poll-interval", 2*time.Second, "Polling interval when delivery method is async")

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
			deliveryMethodSync,
			deliveryMethodAsync,
		}, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}
