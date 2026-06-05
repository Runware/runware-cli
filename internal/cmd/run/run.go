package run

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
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
				if _, alreadySet := payload[fieldDeliveryMethod]; !alreadySet {
					payload[fieldDeliveryMethod] = deliveryMethod
				}
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
			initialResults, err := client.RunDynamic(cmd.Context(), payload)
			if err != nil {
				spin.Stop()
				return err
			}

			// --- 7a. Collect results: poll for async, use initial response for sync ---
			var results []json.RawMessage

			if strings.EqualFold(deliveryMethod, deliveryMethodAsync) {
				spin.Suffix = " Waiting for result..."

				results, err = client.PollDynamic(cmd.Context(), taskUUID, flags.pollInterval, func(p int) {
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
	f.StringVar(&flags.taskType, "task-type", "", "Override the detected task type (e.g. imageInference, videoInference, textInference, audioInference)")
	f.StringVar(&flags.outputDir, "output-dir", "./outputs", "Directory to save downloaded output files")
	f.BoolVar(&flags.noDownload, "no-download", false, "Skip auto-downloading media files (imageURL, videoURL, audioURL)")
	f.StringVar(&flags.deliveryMethod, "delivery-method", "", "Override delivery method (sync or async); default taken from model schema")
	f.DurationVar(&flags.pollInterval, "poll-interval", 2*time.Second, "Polling interval when delivery method is async")

	//nolint:errcheck,gosec
	cmd.RegisterFlagCompletionFunc("task-type", func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return []cobra.Completion{
			"imageInference",
			"videoInference",
			"audioInference",
			"textInference",
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

// schemaArgCompleter provides shell autocompletion for key=value positional arguments.
// When the first arg (the model AIR) is already present, it fetches the model's schema
// and returns dot-notation leaf completions (e.g. "speech.text=", "messages.0.role=")
// for parameters not yet provided in args. Completion is best-effort: failures are
// silently ignored.
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

	// Collect the full dot-notation key of every arg the user has already typed,
	// e.g. "messages.0.role", "width".
	provided := make(map[string]struct{}, len(args))
	for _, a := range args[1:] {
		k, _, _ := strings.Cut(a, "=")
		if k != "" {
			provided[k] = struct{}{}
		}
	}

	// nextArrayIdx returns the next unused index for an array field identified by
	// its dot-notation prefix (e.g. "messages"). It scans already-provided args
	// for the pattern "prefix.N.*" and returns max(N)+1, or 0 if none found.
	nextArrayIdx := func(prefix string) int {
		highest := -1
		needle := prefix + "."
		for k := range provided {
			if !strings.HasPrefix(k, needle) {
				continue
			}
			rest := k[len(needle):]
			seg, _, _ := strings.Cut(rest, ".")
			if isNumeric(seg) {
				if n := mustAtoi(seg); n > highest {
					highest = n
				}
			}
		}
		return highest + 1
	}

	// If the user has started typing, only return completions that share the prefix.
	prefix, _, hasEq := strings.Cut(toComplete, "=")
	if hasEq {
		// Value side already typed — nothing more to complete.
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	completions := collectCompletions("", node, provided, prefix, nextArrayIdx)

	// NoSpace so the shell doesn't add a space after the '=', letting the user
	// immediately type the value.
	return completions, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}

// collectCompletions recursively walks a schema node and emits a dot-notation
// "key=" completion for every leaf property. prefix is the dot-path built so far
// (empty at the top level). Object properties are recursed into; array fields
// use nextArrayIdx to determine the next index to suggest.
func collectCompletions(
	prefix string,
	node schemaNode,
	provided map[string]struct{},
	toCompletePrefix string,
	nextArrayIdx func(string) int,
) []cobra.Completion {
	var out []cobra.Completion

	for name := range node.Properties {
		prop := node.Properties[name]
		if _, skip := autoFields[name]; skip {
			continue
		}

		full := name
		if prefix != "" {
			full = prefix + "." + name
		}

		switch prop.Type {
		case schemaTypeObject:
			if len(prop.Properties) > 0 {
				// Recurse — emit completions for the object's own leaves.
				out = append(out, collectCompletions(full, prop, provided, toCompletePrefix, nextArrayIdx)...)
				continue
			}
			// Object with no known sub-properties — fall through to leaf.

		case schemaTypeArray:
			idx := nextArrayIdx(full)
			idxStr := strconv.Itoa(idx)
			indexedPrefix := full + "." + idxStr

			if prop.Items != nil && prop.Items.Type == schemaTypeObject && len(prop.Items.Properties) > 0 {
				// Recurse into the item schema.
				out = append(out, collectCompletions(indexedPrefix, *prop.Items, provided, toCompletePrefix, nextArrayIdx)...)
				continue
			}
			// Scalar array or unknown items — suggest "field.N=".
			candidate := indexedPrefix + "="
			if _, done := provided[indexedPrefix]; !done && strings.HasPrefix(indexedPrefix, toCompletePrefix) {
				desc := prop.Description
				if desc == "" {
					desc = prop.Type
				}
				out = append(out, cobra.CompletionWithDesc(candidate, desc))
			}
			continue
		}

		// Leaf field (string / integer / number / boolean / object with no sub-props).
		candidate := full + "="
		if _, done := provided[full]; !done && strings.HasPrefix(full, toCompletePrefix) {
			desc := prop.Description
			if desc == "" {
				desc = prop.Type
			}
			out = append(out, cobra.CompletionWithDesc(candidate, desc))
		}
	}

	return out
}
