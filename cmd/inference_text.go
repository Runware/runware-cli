package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

var textFlags struct {
	model        string
	system       string
	maxTokens    int
	temperature  float64
	topP         float64
	topK         int
	seed         int64
	stop         []string
	count        int
	outputFormat string
	includeCost  bool
	preset       string
	dryRun       bool
}

var textInferenceCmd = &cobra.Command{
	Use:   "text [message]",
	Short: "Generate text using a language model",
	Long: `Send a message to a language model and get a text response.

Examples:
  runware inference text "What is the capital of France?" --model minimax:m2.7@highspeed
  runware inference text "Explain quantum computing" --model minimax:m2.7@highspeed --max-tokens 500
  runware inference text "Write a haiku about coding" --model minimax:m2.7@highspeed --system "You are a poet" --temperature 0.8
  runware inference text "List 3 facts about Mars" --model minimax:m2.7@highspeed --output-format json`,
	Args:    cobra.ExactArgs(1),
	PreRunE: preRunTextInference,
	RunE:    runTextInference,
}

var resolvedTextModel string

func init() {
	f := textInferenceCmd.Flags()
	f.StringVarP(&textFlags.model, "model", "m", "", "Model identifier (e.g. runware:qwen3-thinking@1)")
	f.StringVarP(&textFlags.system, "system", "y", "", "System prompt")
	f.IntVarP(&textFlags.maxTokens, "max-tokens", "M", 0, "Maximum tokens in response (1-128000)")
	f.Float64VarP(&textFlags.temperature, "temperature", "q", 0, "Sampling temperature (0-2)")
	f.Float64VarP(&textFlags.topP, "top-p", "P", 0, "Nucleus sampling parameter (0-1)")
	f.IntVarP(&textFlags.topK, "top-k", "K", 0, "Top-k sampling parameter (1-100)")
	f.Int64VarP(&textFlags.seed, "seed", "e", 0, "Random seed for reproducibility")
	f.StringSliceVarP(&textFlags.stop, "stop", "Z", nil, "Stop sequences (max 5)")
	f.IntVarP(&textFlags.count, "count", "n", 1, "Number of results to generate (1-4)")
	f.StringVarP(&textFlags.outputFormat, "output-format", "f", "", "Format of the model response: text, json")
	f.BoolVarP(&textFlags.includeCost, "include-cost", "C", false, "Include cost info in response")
	f.StringVarP(&textFlags.preset, "preset", "p", "", "Named preset to apply")
	f.BoolVarP(&textFlags.dryRun, "dry-run", "X", false, "Print the API request without executing")

	textInferenceCmd.RegisterFlagCompletionFunc("output-format", func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{string(api.OutputFormatText), string(api.OutputFormatJSON)}, cobra.ShellCompDirectiveNoFileComp
	})

	textInferenceCmd.RegisterFlagCompletionFunc("preset", completePresetNames) //nolint:errcheck,gosec
}

func preRunTextInference(cmd *cobra.Command, _ []string) error {
	// Resolve model from preset or flag for early validation
	resolvedTextModel = config.Get().Defaults.Model
	if textFlags.preset != "" {
		if preset := config.GetPreset(textFlags.preset); preset != nil && preset.Model != "" {
			resolvedTextModel = preset.Model
		}
	}
	if cmd.Flags().Changed("model") {
		resolvedTextModel = textFlags.model
	}
	if resolvedTextModel == "" {
		return fmt.Errorf("--model is required (e.g. minimax:m2.7@highspeed)")
	}
	if textFlags.count < 1 || textFlags.count > 4 {
		return fmt.Errorf("--count must be between 1 and 4")
	}
	return nil
}

func runTextInference(cmd *cobra.Command, args []string) error {
	key := config.GetAPIKey()
	if key == "" {
		output.Error("No API key configured. Run 'runware auth login' to authenticate.")
		return api.ErrNoAPIKey
	}

	message := args[0]

	// Build request
	req := &api.TextInferenceRequest{
		TaskUUID: api.NewUUID(),
		Model:    resolvedTextModel,
		Messages: []api.Message{
			{Role: "user", Content: message},
		},
		IncludeCost: textFlags.includeCost,
	}

	if textFlags.system != "" {
		req.SystemPrompt = textFlags.system
	}
	if textFlags.maxTokens > 0 {
		req.MaxTokens = textFlags.maxTokens
	}
	if cmd.Flags().Changed("temperature") {
		req.Temperature = textFlags.temperature
	}
	if cmd.Flags().Changed("top-p") {
		req.TopP = textFlags.topP
	}
	if textFlags.topK > 0 {
		req.TopK = textFlags.topK
	}
	if cmd.Flags().Changed("seed") {
		req.Seed = textFlags.seed
	}
	if len(textFlags.stop) > 0 {
		req.StopSequences = textFlags.stop
	}
	if textFlags.count > 1 {
		req.NumberResults = textFlags.count
	}
	if textFlags.outputFormat != "" {
		req.OutputFormat = api.OutputFormat(textFlags.outputFormat)
	}

	// Dry run
	if textFlags.dryRun {
		data, _ := json.MarshalIndent([]any{req}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Submit
	s := output.NewSpinner(" Generating text...")
	s.Start()

	client := api.NewClient(key, config.GetBaseURL(), flagVerbose)
	results, err := client.TextInference(context.Background(), req)
	if err != nil {
		s.Stop()
		if api.IsAuthError(err) {
			output.Error("Authentication failed. Run 'runware auth login' to set your API key.")
			return err
		}
		return err
	}

	s.Stop()

	if len(results) == 0 {
		output.Error("No results returned")
		return fmt.Errorf("empty response from text inference")
	}

	// JSON/YAML output
	format := output.ParseFormat(getFormat())
	if format != output.FormatTable {
		return output.Print(format, results, nil, nil)
	}

	// Single result without cost — print text directly (pipe-friendly)
	if len(results) == 1 && !textFlags.includeCost {
		fmt.Println(results[0].Text)
		return nil
	}

	// Multiple results or cost requested — use table
	headers := []any{"#", "Text"}
	if textFlags.includeCost {
		headers = append(headers, "Cost")
	}
	var rows [][]any
	for i, r := range results {
		text := r.Text
		if len(text) > 100 {
			text = text[:100] + "..."
		}
		row := []any{i + 1, text}
		if textFlags.includeCost {
			row = append(row, fmt.Sprintf("%.6f", r.Cost))
		}
		rows = append(rows, row)
	}
	return output.Print(format, results, headers, rows)
}
