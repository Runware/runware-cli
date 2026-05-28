package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	rhttp "github.com/runware/runware-cli/pkg/http"
	"github.com/spf13/cobra"
)

var audioInferenceCmd = &cobra.Command{
	Use:   "audioInference [prompt]",
	Short: "Generate audio from text descriptions",
	Long: `Generate audio using text-to-audio, music generation, or sound effects.

Examples:
  runware audioInference "a jazz piano solo with soft drums" --model elevenlabs:1@1 --duration 30
  runware audioInference "ocean waves crashing on rocks" --model elevenlabs:1@1 --duration 60
  runware audioInference "upbeat electronic music" --model elevenlabs:1@1 --duration 120 --sample-rate 48000`,
	Args: cobra.ExactArgs(1),
	RunE: runAudioInference,
}

func init() {
	f := audioInferenceCmd.Flags()
	f.String("model", "", "Model identifier (e.g. elevenlabs:1@1)")
	f.Float64("duration", 0, "Audio duration in seconds (10-300)")
	f.Int("count", 1, "Number of audio files to generate (max 3)")
	f.String("output", "", "Output directory")
	f.String("output-format", "", "Audio format: mp3")
	f.Bool("no-download", false, "Print audio URLs instead of downloading")
	f.Bool("include-cost", false, "Include cost info in response")
	f.Int("sample-rate", 0, "Sample rate in Hz (8000-48000)")
	f.Int("bitrate", 0, "Bitrate in kbps (32-320, compressed formats only)")
	f.String("preset", "", "Named preset to apply")
	f.Bool("dry-run", false, "Print the API request without executing")
	f.Duration("poll-interval", defaultPollInterval, "Polling interval for async results")
	f.Duration("timeout", defaultAudioGenerationTimeout, "Maximum wait time for audio generation")
	f.Duration("download-timeout", defaultAudioDownloadTimeout, "timeout to use when downloading audio inference results")

	audioInferenceCmd.RegisterFlagCompletionFunc("output-format", func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{string(api.OutputFormatMP3)}, cobra.ShellCompDirectiveNoFileComp
	})

	audioInferenceCmd.RegisterFlagCompletionFunc("preset", completePresetNames) //nolint:errcheck,gosec
}

func runAudioInference(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	key := config.GetAPIKey()
	if key == "" {
		output.Error("No API key configured. Run 'runware auth login' to authenticate.")
		return api.ErrNoAPIKey
	}

	cfg := config.Get()
	prompt := args[0]

	model := cfg.Defaults.Model
	outputDir := cfg.Defaults.OutputDir

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
	if cmd.Flags().Changed("output") {
		outputDir, _ = cmd.Flags().GetString("output")
	}

	duration, _ := cmd.Flags().GetFloat64("duration")
	count, _ := cmd.Flags().GetInt("count")
	outputFormat, _ := cmd.Flags().GetString("output-format")
	noDownload, _ := cmd.Flags().GetBool("no-download")
	includeCost, _ := cmd.Flags().GetBool("include-cost")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	sampleRate, _ := cmd.Flags().GetInt("sample-rate")
	bitrate, _ := cmd.Flags().GetInt("bitrate")
	pollInterval, _ := cmd.Flags().GetDuration("poll-interval")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	downloadTimeout, _ := cmd.Flags().GetDuration("download-timeout")

	// Validation
	if duration <= 0 {
		return fmt.Errorf("--duration is required (10-300 seconds)")
	}

	// Build request
	req := &api.AudioInferenceRequest{
		TaskUUID:       api.NewUUID(),
		Model:          model,
		PositivePrompt: prompt,
		Duration:       duration,
		DeliveryMethod: api.DeliveryMethodAsync,
		IncludeCost:    includeCost,
	}

	req.NumberResults = count
	if outputFormat != "" {
		req.OutputFormat = api.OutputFormat(outputFormat)
	}

	// Audio settings
	if sampleRate > 0 || bitrate > 0 {
		req.AudioSettings = &api.AudioSettings{}
		if sampleRate > 0 {
			req.AudioSettings.SampleRate = sampleRate
		}
		if bitrate > 0 {
			req.AudioSettings.Bitrate = bitrate
		}
	}

	// Dry run
	if dryRun {
		data, _ := json.MarshalIndent([]any{req}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Submit
	s := output.NewSpinner(" Submitting audio generation task...")
	s.Start()

	client := api.NewClient(key, config.GetBaseURL(), flagVerbose)
	_, err := client.AudioInference(context.Background(), req)
	if err != nil {
		s.Stop()

		if api.IsAuthError(err) {
			output.Error("Authentication failed. Run 'runware auth login' to set your API key.")
			return err
		}
		return err
	}

	s.Suffix(" Generating audio (this may take a few minutes)...")

	taskUUID := req.TaskUUID

	pollCtx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()

	results, err := api.PollResults(
		pollCtx,
		client,
		taskUUID,
		pollInterval,
		flagVerbose,
		func(raw json.RawMessage) (api.AudioInferenceResult, bool) {
			var r api.AudioInferenceResult
			if err := json.Unmarshal(raw, &r); err != nil {
				return r, false
			}
			return r, r.AudioURL != ""
		},
	)
	if err != nil {
		s.Stop()
		output.Error("Audio generation failed")
		return err
	}

	if len(results) == 0 {
		s.Stop()
		output.Error("Audio generation timed out or returned no results")
		return fmt.Errorf("no audio results after %v", timeout)
	}

	// JSON/YAML output
	format := output.ParseFormat(getFormat())
	if format != output.FormatTable {
		s.Stop()
		return output.Print(format, results, nil, nil)
	}

	// Table output
	if noDownload {
		headers := []any{tableHeaderNum, tableHeaderURL}
		if includeCost {
			headers = append(headers, tableHeaderCost)
		}
		var rows [][]any
		for i, r := range results {
			row := []any{i + 1, r.AudioURL}
			if includeCost {
				row = append(row, fmt.Sprintf("%.4f", r.Cost))
			}
			rows = append(rows, row)
		}

		s.Stop()
		return output.Print(format, results, headers, rows)
	}

	s.Suffix(" Downloading audio results...")

	// Download audio files
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	headers := []any{tableHeaderNum, tableHeaderFile}
	if includeCost {
		headers = append(headers, tableHeaderCost)
	}
	var rows [][]any

	for i, r := range results {
		ext := outputFormat
		if ext == "" {
			ext = string(api.OutputFormatMP3)
		}
		filename := fmt.Sprintf("runware_%s_%d.%s", time.Now().Format("20060102_150405"), i+1, ext)
		destPath := filepath.Join(outputDir, filename)

		if err := rhttp.Download(ctx, r.AudioURL, destPath, downloadTimeout); err != nil {
			output.Error(fmt.Sprintf("Failed to download audio %d: %s", i+1, err))
			continue
		}

		row := []any{i + 1, destPath}
		if includeCost {
			row = append(row, fmt.Sprintf("%.4f", r.Cost))
		}
		rows = append(rows, row)
	}

	s.Stop()
	return output.Print(format, results, headers, rows)
}
