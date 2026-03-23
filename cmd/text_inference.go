package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

var textInferenceCmd = &cobra.Command{
	Use:   "textInference [message]",
	Short: "Generate text using a language model",
	Long: `Send a message to a language model and get a text response.

Examples:
  runware textInference "What is the capital of France?"
  runware textInference "Explain quantum computing" --model runware:qwen3-thinking@1 --max-tokens 500
  runware textInference "Write a haiku about coding" --system "You are a poet" --temperature 0.8
  runware textInference "List 3 facts about Mars" --output-format json`,
	Args: cobra.ExactArgs(1),
	RunE: runTextInference,
}

func init() {
	f := textInferenceCmd.Flags()
	f.String("model", "", "Model identifier (e.g. runware:qwen3-thinking@1)")
	f.String("system", "", "System prompt")
	f.Int("max-tokens", 0, "Maximum tokens in response (1-128000)")
	f.Float64("temperature", 0, "Sampling temperature (0-2)")
	f.Float64("top-p", 0, "Nucleus sampling parameter (0-1)")
	f.Int("top-k", 0, "Top-k sampling parameter (1-100)")
	f.Int64("seed", 0, "Random seed for reproducibility")
	f.StringSlice("stop", nil, "Stop sequences (max 5)")
	f.Int("count", 1, "Number of results to generate (1-4)")
	f.String("output-format", "", "LLM output format: text or json")
	f.Bool("include-cost", false, "Include cost info in response")
	f.String("preset", "", "Named preset to apply")
	f.Bool("dry-run", false, "Print the API request without executing")

	_ = textInferenceCmd.RegisterFlagCompletionFunc("output-format", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"text", "json"}, cobra.ShellCompDirectiveNoFileComp
	})

	_ = textInferenceCmd.RegisterFlagCompletionFunc("preset", completePresetNames)
}

func runTextInference(cmd *cobra.Command, args []string) error {
	key := config.GetAPIKey()
	if key == "" {
		output.Error("No API key configured. Run 'runware auth login' to authenticate.")
		return api.ErrNoAPIKey
	}

	cfg := config.Get()
	message := args[0]

	model := cfg.Defaults.Model

	// Apply preset if specified
	presetName, _ := cmd.Flags().GetString("preset")
	if presetName != "" {
		preset := config.GetPreset(presetName)
		if preset == nil {
			return fmt.Errorf("preset '%s' not found", presetName)
		}
		if preset.Model != "" {
			model = preset.Model
		}
	}

	// Override with explicit CLI flags
	if cmd.Flags().Changed("model") {
		model, _ = cmd.Flags().GetString("model")
	}

	systemPrompt, _ := cmd.Flags().GetString("system")
	maxTokens, _ := cmd.Flags().GetInt("max-tokens")
	temperature, _ := cmd.Flags().GetFloat64("temperature")
	topP, _ := cmd.Flags().GetFloat64("top-p")
	topK, _ := cmd.Flags().GetInt("top-k")
	seed, _ := cmd.Flags().GetInt64("seed")
	stopSequences, _ := cmd.Flags().GetStringSlice("stop")
	count, _ := cmd.Flags().GetInt("count")
	outputFmt, _ := cmd.Flags().GetString("output-format")
	includeCost, _ := cmd.Flags().GetBool("include-cost")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Build request
	req := &api.TextInferenceRequest{
		TaskType: "textInference",
		TaskUUID: api.NewUUID(),
		Model:    model,
		Messages: []api.Message{
			{Role: "user", Content: message},
		},
		IncludeCost: includeCost,
	}

	if systemPrompt != "" {
		req.SystemPrompt = systemPrompt
	}
	if maxTokens > 0 {
		req.MaxTokens = maxTokens
	}
	if cmd.Flags().Changed("temperature") {
		req.Temperature = temperature
	}
	if cmd.Flags().Changed("top-p") {
		req.TopP = topP
	}
	if topK > 0 {
		req.TopK = topK
	}
	if seed > 0 {
		req.Seed = seed
	}
	if len(stopSequences) > 0 {
		req.StopSequences = stopSequences
	}
	if count > 1 {
		req.NumberResults = count
	}
	if outputFmt != "" {
		req.OutputFormat = outputFmt
	}

	// Dry run
	if dryRun {
		data, _ := json.MarshalIndent([]interface{}{req}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Submit
	var s *spinner.Spinner
	if output.IsTTY() {
		s = spinner.New(spinner.CharSets[14], 100*time.Millisecond, spinner.WithWriter(os.Stderr))
		s.Suffix = " Generating text..."
		s.Start()
	}

	client := api.NewClient(key, config.GetBaseURL(), flagVerbose)
	results, err := client.TextInference(context.Background(), req)

	if s != nil {
		s.Stop()
	}

	if err != nil {
		if api.IsAuthError(err) {
			output.Error("Authentication failed. Run 'runware auth login' to set your API key.")
			return err
		}
		return err
	}

	if len(results) == 0 {
		output.Error("No results returned")
		return fmt.Errorf("empty response from text inference")
	}

	// JSON/YAML output
	format := output.ParseFormat(getFormat())
	if format != output.FormatTable {
		return output.Print(format, results, nil, nil)
	}

	// Table: single result without cost — print text directly (pipe-friendly)
	if len(results) == 1 && !includeCost {
		fmt.Println(results[0].Text)
		return nil
	}

	// Multiple results or cost requested — use table
	headers := []interface{}{"#", "Text"}
	if includeCost {
		headers = append(headers, "Cost")
	}
	var rows [][]interface{}
	for i, r := range results {
		text := r.Text
		if len(text) > 100 {
			text = text[:100] + "..."
		}
		row := []interface{}{i + 1, text}
		if includeCost {
			row = append(row, fmt.Sprintf("%.6f", r.Cost))
		}
		rows = append(rows, row)
	}
	return output.Print(format, results, headers, rows)
}
