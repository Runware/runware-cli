package run

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

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
		taskType   string
		outputDir  string
		noDownload bool
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
  runware run openai:o1@0 messages='[{"role":"user","content":"Explain quantum computing"}]'

  # Video generation
  runware run klingai:5@3 positivePrompt="Ocean waves at sunset" duration=10

  # Community model — task type must be specified explicitly
  runware run civitai:305149@392545 --task-type imageInference positivePrompt="A portrait" width=1024 height=1024

  # Save output to a specific directory
  runware run runware:101@1 positivePrompt="Abstract art" --output-dir ./my-images

  # Output as JSON without downloading
  runware run runware:101@1 positivePrompt="Abstract art" --format json --no-download`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			model := args[0]
			kvArgs := args[1:]

			// --- 1. Fetch the model schema ---
			schema, schemaErr := api.FetchModelSchema(cmd.Context(), model)
			if schemaErr != nil {
				// Without a schema we cannot auto-detect the task type.
				// If the caller provided --task-type we can still proceed without validation.
				if flags.taskType == "" {
					return fmt.Errorf("could not fetch schema for %q: %w; use --task-type to specify the task type and skip validation", model, schemaErr)
				}
				logger.Warn("schema unavailable; skipping validation", "model", model, "err", schemaErr)
			}

			// --- 2. Parse the request schema ---
			var reqSchema schemaNode
			if schema != nil {
				if err := json.Unmarshal(schema.RequestSchema, &reqSchema); err != nil {
					return fmt.Errorf("failed to parse request schema: %w", err)
				}
			}

			// --- 3. Determine task type ---
			taskType := flags.taskType
			if taskType == "" {
				detected, ok := extractTaskType(reqSchema)
				if !ok {
					return fmt.Errorf("could not detect task type for model %q; use --task-type to specify it (run 'runware model schema %s' to inspect the schema)", model, model)
				}
				taskType = detected
			}

			// --- 4. Parse key=value arguments into payload ---
			payload := map[string]any{
				"model": model,
			}
			for _, kv := range kvArgs {
				k, v, err := parseKV(kv, reqSchema)
				if err != nil {
					return fmt.Errorf("invalid argument %q: %w", kv, err)
				}
				payload[k] = v
			}

			// --- 5. Validate required fields against schema ---
			if schema != nil {
				if err := validateRequired(reqSchema, payload); err != nil {
					return err
				}
			}

			// --- 6. Inject system fields ---
			payload["taskType"] = taskType
			payload["taskUUID"] = uuid.New()

			// --- 7. Connect, send, receive ---
			t, err := cmdutil.NewTransport(cmd, slog.New(logger))
			if err != nil {
				return err
			}
			defer t.Close() //nolint:errcheck

			client := api.NewClient(t, slog.New(logger))
			results, err := client.RunDynamic(cmd.Context(), payload)
			if err != nil {
				return err
			}

			if len(results) == 0 {
				return fmt.Errorf("no results returned from API")
			}

			// --- 8. Output and optionally download media ---
			return handleResults(cmd, logger, results, flags.outputDir, flags.noDownload)
		},
		// Dynamic completion: when the model arg is already typed, suggest
		// key= completions from the schema's properties.
		ValidArgsFunction: schemaArgCompleter,
	}

	f := cmd.Flags()
	f.StringVar(&flags.taskType, "task-type", "", "Override the detected task type (e.g. imageInference, videoInference, textInference, audioInference)")
	f.StringVar(&flags.outputDir, "output-dir", "./outputs", "Directory to save downloaded output files")
	f.BoolVar(&flags.noDownload, "no-download", false, "Skip auto-downloading media files (imageURL, videoURL, audioURL)")

	//nolint:errcheck,gosec
	cmd.RegisterFlagCompletionFunc("task-type", func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return []cobra.Completion{
			"imageInference",
			"videoInference",
			"audioInference",
			"textInference",
		}, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

// schemaArgCompleter provides shell autocompletion for key=value positional arguments.
// When the first arg (the model AIR) is already present, it fetches the model's schema
// and returns "paramName=" completions for parameters not yet provided in args.
// Completion is best-effort: failures are silently ignored.
func schemaArgCompleter(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	// First argument is the model — let the shell handle free-form text for it.
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	model := args[0]
	schema, err := api.FetchModelSchema(cmd.Context(), model)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var node schemaNode
	if err := json.Unmarshal(schema.RequestSchema, &node); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Collect which keys the user has already provided.
	provided := make(map[string]bool, len(args))
	for _, a := range args[1:] {
		k, _, _ := strings.Cut(a, "=")
		if k != "" {
			provided[k] = true
		}
	}

	// If the user has started typing, filter to matching keys.
	prefix, _, _ := strings.Cut(toComplete, "=")

	var completions []cobra.Completion
	for name := range node.Properties {
		prop := node.Properties[name]
		if autoFields[name] || provided[name] {
			continue
		}
		if toComplete != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		desc := prop.Description
		if desc == "" {
			desc = prop.Type
		}
		completions = append(completions, cobra.CompletionWithDesc(name+"=", desc))
	}

	// NoSpace so the shell doesn't add a space after the '=', letting the user
	// immediately type the value.
	return completions, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}
