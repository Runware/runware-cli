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

var audioFlags struct {
	model           string
	duration        float64
	count           int
	outputDir       string
	outputFormat    string
	noDownload      bool
	includeCost     bool
	sampleRate      int
	bitrate         int
	preset          string
	dryRun          bool
	pollInterval    time.Duration
	timeout         time.Duration
	downloadTimeout time.Duration
}

var audioInferenceCmd = &cobra.Command{
	Use:   "audio [prompt]",
	Short: "Generate audio from text descriptions",
	Long: `Generate audio using text-to-audio, music generation, or sound effects.

Examples:
  runware inference audio "a jazz piano solo with soft drums" --model elevenlabs:1@1 --duration 30
  runware inference audio "ocean waves crashing on rocks" --model elevenlabs:1@1 --duration 60
  runware inference audio "upbeat electronic music" --model elevenlabs:1@1 --duration 120 --sample-rate 48000`,
	Args:    cobra.ExactArgs(1),
	PreRunE: preRunAudioInference,
	RunE:    runAudioInference,
}

func init() {
	f := audioInferenceCmd.Flags()
	f.StringVarP(&audioFlags.model, "model", "m", "", "Model identifier (e.g. elevenlabs:1@1)")
	f.Float64VarP(&audioFlags.duration, "duration", "d", defaultMinAudioDuration, "Audio duration in seconds (10-300)")
	f.IntVarP(&audioFlags.count, "count", "n", 1, "Number of audio files to generate (max 3)")
	f.StringVarP(&audioFlags.outputDir, "output", "o", "", "Output directory")
	f.StringVarP(&audioFlags.outputFormat, "output-format", "f", "", "Format of generated audio: mp3")
	f.BoolVarP(&audioFlags.noDownload, "no-download", "D", false, "Print audio URLs instead of downloading")
	f.BoolVarP(&audioFlags.includeCost, "include-cost", "c", false, "Include cost info in response")
	f.IntVarP(&audioFlags.sampleRate, "sample-rate", "r", 0, "Sample rate in Hz (8000-48000)")
	f.IntVarP(&audioFlags.bitrate, "bitrate", "b", 0, "Bitrate in kbps (32-320, compressed formats only)")
	f.StringVarP(&audioFlags.preset, "preset", "p", "", "Named preset to apply")
	f.BoolVarP(&audioFlags.dryRun, "dry-run", "X", false, "Print the API request without executing")
	f.DurationVarP(&audioFlags.pollInterval, "poll-interval", "i", defaultPollInterval, "Polling interval for async results")
	f.DurationVarP(&audioFlags.timeout, "timeout", "t", defaultAudioGenerationTimeout, "Maximum wait time for audio generation")
	f.DurationVarP(&audioFlags.downloadTimeout, "download-timeout", "T", defaultAudioDownloadTimeout, "Timeout for downloading audio results")

	audioInferenceCmd.RegisterFlagCompletionFunc("output-format", func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{string(api.OutputFormatMP3)}, cobra.ShellCompDirectiveNoFileComp
	})

	audioInferenceCmd.RegisterFlagCompletionFunc("preset", completePresetNames) //nolint:errcheck,gosec
}

func preRunAudioInference(_ *cobra.Command, _ []string) error {
	if audioFlags.duration < defaultMinAudioDuration || audioFlags.duration > maxAudioDuration {
		return fmt.Errorf("--duration must be between %.0f and %.0f seconds", defaultMinAudioDuration, maxAudioDuration)
	}
	if audioFlags.count < 1 || audioFlags.count > 3 {
		return fmt.Errorf("--count must be between 1 and 3")
	}
	return nil
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
	if audioFlags.preset != "" {
		preset := config.GetPreset(audioFlags.preset)
		if preset == nil {
			return fmt.Errorf("preset '%s' not found", audioFlags.preset)
		}
		if preset.Model != "" {
			model = preset.Model
		}
	}

	// Override with explicit CLI flags
	if cmd.Flags().Changed("model") {
		model = audioFlags.model
	}
	if cmd.Flags().Changed("output") {
		outputDir = audioFlags.outputDir
	}

	// Build request
	req := &api.AudioInferenceRequest{
		TaskUUID:       api.NewUUID(),
		Model:          model,
		PositivePrompt: prompt,
		Duration:       audioFlags.duration,
		DeliveryMethod: api.DeliveryMethodAsync,
		IncludeCost:    audioFlags.includeCost,
		NumberResults:  audioFlags.count,
	}

	if audioFlags.outputFormat != "" {
		req.OutputFormat = api.OutputFormat(audioFlags.outputFormat)
	}

	if audioFlags.sampleRate > 0 || audioFlags.bitrate > 0 {
		req.AudioSettings = &api.AudioSettings{}
		if audioFlags.sampleRate > 0 {
			req.AudioSettings.SampleRate = audioFlags.sampleRate
		}
		if audioFlags.bitrate > 0 {
			req.AudioSettings.Bitrate = audioFlags.bitrate
		}
	}

	// Dry run
	if audioFlags.dryRun {
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

	pollCtx, cancel := context.WithTimeout(cmd.Context(), audioFlags.timeout)
	defer cancel()

	results, err := api.PollResults(
		pollCtx,
		client,
		req.TaskUUID,
		audioFlags.pollInterval,
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
		return fmt.Errorf("no audio results after %v", audioFlags.timeout)
	}

	// JSON/YAML output
	format := output.ParseFormat(getFormat())
	if format != output.FormatTable {
		s.Stop()
		return output.Print(format, results, nil, nil)
	}

	// Table output — no-download: just print URLs
	if audioFlags.noDownload {
		headers := []any{tableHeaderNum, tableHeaderURL}
		if audioFlags.includeCost {
			headers = append(headers, tableHeaderCost)
		}
		var rows [][]any
		for i, r := range results {
			row := []any{i + 1, r.AudioURL}
			if audioFlags.includeCost {
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
	if audioFlags.includeCost {
		headers = append(headers, tableHeaderCost)
	}
	var rows [][]any

	for i, r := range results {
		ext := audioFlags.outputFormat
		if ext == "" {
			ext = string(api.OutputFormatMP3)
		}
		filename := fmt.Sprintf("runware_%s_%d.%s", time.Now().Format("20060102_150405"), i+1, ext)
		destPath := filepath.Join(outputDir, filename)

		if err := rhttp.Download(ctx, r.AudioURL, destPath, audioFlags.downloadTimeout); err != nil {
			output.Error(fmt.Sprintf("Failed to download audio %d: %s", i+1, err))
			continue
		}

		row := []any{i + 1, destPath}
		if audioFlags.includeCost {
			row = append(row, fmt.Sprintf("%.4f", r.Cost))
		}
		rows = append(rows, row)
	}

	s.Stop()
	return output.Print(format, results, headers, rows)
}
