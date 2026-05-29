package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	rhttp "github.com/runware/runware-cli/pkg/http"
	"github.com/spf13/cobra"
)

var videoFlags struct {
	model           string
	width           int
	height          int
	duration        float64
	steps           int
	cfgScale        float64
	seed            int64
	negative        string
	count           int
	outputDir       string
	outputFormat    string
	noDownload      bool
	sourcePath      string
	sourceLastPath  string
	includeCost     bool
	preset          string
	dryRun          bool
	pollInterval    time.Duration
	timeout         time.Duration
	downloadTimeout time.Duration
}

var videoInferenceCmd = &cobra.Command{
	Use:   "video [prompt]",
	Short: "Generate videos from text or image input",
	Long: `Generate videos using text-to-video or image-to-video.

Examples:
  runware inference video "a timelapse of a sunset over mountains" --model klingai:5@3
  runware inference video "a cat playing piano" --model google:3@2 --duration 4
  runware inference video "animate this scene" --model klingai:5@3 --source ./photo.png`,
	Args: cobra.ExactArgs(1),
	RunE: runVideoInference,
}

func init() {
	f := videoInferenceCmd.Flags()
	f.StringVarP(&videoFlags.model, "model", "m", "", "Model identifier (e.g. klingai:5@3, google:3@2)")
	f.IntVarP(&videoFlags.width, "width", "W", 0, "Video width in pixels")
	f.IntVarP(&videoFlags.height, "height", "H", 0, "Video height in pixels")
	f.Float64VarP(&videoFlags.duration, "duration", "d", 0, "Video duration in seconds")
	f.IntVarP(&videoFlags.steps, "steps", "s", 0, "Number of inference steps")
	f.Float64VarP(&videoFlags.cfgScale, "cfg", "c", 0, "CFG scale")
	f.Int64VarP(&videoFlags.seed, "seed", "e", 0, "Seed for reproducibility")
	f.StringVarP(&videoFlags.negative, "negative", "N", "", "Negative prompt")
	f.IntVarP(&videoFlags.count, "count", "n", 1, "Number of videos to generate")
	f.StringVarP(&videoFlags.outputDir, "output", "o", "", "Output directory")
	f.StringVarP(&videoFlags.outputFormat, "output-format", "f", "", "Format of generated videos: mp4, webm")
	f.BoolVarP(&videoFlags.noDownload, "no-download", "D", false, "Print video URLs instead of downloading")
	f.StringVarP(&videoFlags.sourcePath, "source", "i", "", "Source image path for image-to-video")
	f.StringVarP(&videoFlags.sourceLastPath, "source-last", "L", "", "Last frame image path")
	f.BoolVarP(&videoFlags.includeCost, "include-cost", "C", false, "Include cost info in response")
	f.StringVarP(&videoFlags.preset, "preset", "p", "", "Named preset to apply")
	f.BoolVarP(&videoFlags.dryRun, "dry-run", "X", false, "Print the API request without executing")
	f.DurationVarP(&videoFlags.pollInterval, "poll-interval", "I", defaultPollInterval, "Polling interval for async results")
	f.DurationVarP(&videoFlags.timeout, "timeout", "t", defaultVideoGenerationTimeout, "Maximum wait time for video generation")
	f.DurationVarP(&videoFlags.downloadTimeout, "download-timeout", "T", defaultVideoDownloadTimeout, "Timeout for downloading video results")

	videoInferenceCmd.RegisterFlagCompletionFunc("output-format", func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{"mp4", "webm"}, cobra.ShellCompDirectiveNoFileComp
	})

	videoInferenceCmd.RegisterFlagCompletionFunc("preset", completePresetNames) //nolint:errcheck,gosec

	videoInferenceCmd.RegisterFlagCompletionFunc("source", func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{string(api.OutputFormatMP4)}, cobra.ShellCompDirectiveFilterFileExt
	})

	videoInferenceCmd.RegisterFlagCompletionFunc("source-last", func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{string(api.OutputFormatMP4)}, cobra.ShellCompDirectiveFilterFileExt
	})
}

func runVideoInference(cmd *cobra.Command, args []string) error {
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
	if videoFlags.preset != "" {
		preset := config.GetPreset(videoFlags.preset)
		if preset == nil {
			return fmt.Errorf("preset '%s' not found", videoFlags.preset)
		}
		if preset.Model != "" {
			model = preset.Model
		}
	}

	// Override with explicit CLI flags
	if cmd.Flags().Changed("model") {
		model = videoFlags.model
	}
	if cmd.Flags().Changed("output") {
		outputDir = videoFlags.outputDir
	}

	// Build request
	req := &api.VideoInferenceRequest{
		TaskUUID:       uuid.New(),
		Model:          model,
		PositivePrompt: prompt,
		DeliveryMethod: api.DeliveryMethodAsync,
		IncludeCost:    videoFlags.includeCost,
	}

	if videoFlags.negative != "" {
		req.NegativePrompt = videoFlags.negative
	}
	if videoFlags.width > 0 {
		req.Width = videoFlags.width
	}
	if videoFlags.height > 0 {
		req.Height = videoFlags.height
	}
	if videoFlags.duration > 0 {
		req.Duration = videoFlags.duration
	}
	if videoFlags.steps > 0 {
		req.Steps = videoFlags.steps
	}
	if videoFlags.cfgScale > 0 {
		req.CFGScale = videoFlags.cfgScale
	}
	if cmd.Flags().Changed("seed") {
		req.Seed = videoFlags.seed
	}
	if videoFlags.count > 1 {
		req.NumberResults = videoFlags.count
	}
	if videoFlags.outputFormat != "" {
		req.OutputFormat = api.OutputFormat(videoFlags.outputFormat)
	}

	// Handle source images (image-to-video)
	if videoFlags.sourcePath != "" {
		encoded, err := encodeImageFile(videoFlags.sourcePath)
		if err != nil {
			return fmt.Errorf("failed to read source image: %w", err)
		}
		req.FrameImages = append(req.FrameImages, api.FrameImage{
			InputImage: encoded,
			Frame:      "first",
		})
	}
	if videoFlags.sourceLastPath != "" {
		encoded, err := encodeImageFile(videoFlags.sourceLastPath)
		if err != nil {
			return fmt.Errorf("failed to read last frame image: %w", err)
		}
		req.FrameImages = append(req.FrameImages, api.FrameImage{
			InputImage: encoded,
			Frame:      "last",
		})
	}

	// Dry run
	if videoFlags.dryRun {
		data, _ := json.MarshalIndent([]any{req}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Submit the video generation task
	s := output.NewSpinner(" Submitting video generation task...")
	s.Start()

	client := api.NewClient(key, config.GetBaseURL(), flagVerbose)

	submitResults, err := client.VideoInference(context.Background(), req)
	if err != nil {
		s.Stop()
		if api.IsAuthError(err) {
			output.Error("Authentication failed. Run 'runware auth login' to set your API key.")
			return err
		}
		return err
	}

	s.Suffix(" Generating video (this may take a few minutes)...")

	// Poll for completion
	taskID := req.TaskUUID
	if len(submitResults) > 0 && submitResults[0].TaskUUID != uuid.Nil {
		taskID = submitResults[0].TaskUUID
	}

	pollCtx, cancel := context.WithTimeout(cmd.Context(), videoFlags.timeout)
	defer cancel()

	results, err := api.PollResults(
		pollCtx,
		client,
		taskID,
		videoFlags.pollInterval,
		flagVerbose,
		func(raw json.RawMessage) (api.VideoInferenceResult, bool) {
			var r api.VideoInferenceResult
			if err := json.Unmarshal(raw, &r); err != nil {
				return r, false
			}
			return r, r.VideoURL != "" || r.MediaURL != ""
		},
	)
	if err != nil {
		s.Stop()
		output.Error("Video generation failed")
		return err
	}

	if len(results) == 0 {
		s.Stop()
		output.Error("Video generation timed out or returned no results")
		return fmt.Errorf("no video results after %v", videoFlags.timeout)
	}

	// JSON/YAML output
	format := output.ParseFormat(getFormat())
	if format != output.FormatTable {
		s.Stop()
		return output.Print(format, results, nil, nil)
	}

	// Table output — no-download: print URLs
	if videoFlags.noDownload {
		headers := []any{tableHeaderNum, tableHeaderURL, tableHeaderSeed}
		if videoFlags.includeCost {
			headers = append(headers, tableHeaderCost)
		}
		var rows [][]any
		for i := range results {
			url := results[i].VideoURL
			if url == "" {
				url = results[i].MediaURL
			}
			row := []any{i + 1, url, results[i].Seed}
			if videoFlags.includeCost {
				row = append(row, fmt.Sprintf("%.4f", results[i].Cost))
			}
			rows = append(rows, row)
		}
		s.Stop()
		return output.Print(format, results, headers, rows)
	}

	s.Suffix(" Downloading video results...")

	// Download videos
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	headers := []any{tableHeaderNum, tableHeaderFile, tableHeaderSeed}
	if videoFlags.includeCost {
		headers = append(headers, tableHeaderCost)
	}

	var rows [][]any
	var downloadFailures int

	for i := range results {
		ext := videoFlags.outputFormat
		if ext == "" {
			ext = "mp4"
		}
		filename := fmt.Sprintf("runware_%s_%d.%s", time.Now().Format("20060102_150405"), i+1, ext)
		destPath := filepath.Join(outputDir, filename)

		url := results[i].VideoURL
		if url == "" {
			url = results[i].MediaURL
		}

		if err := rhttp.Download(ctx, url, destPath, videoFlags.downloadTimeout); err != nil {
			output.Error(fmt.Sprintf("Failed to download video %d: %s", i+1, err))
			downloadFailures++
			continue
		}

		row := []any{i + 1, destPath, results[i].Seed}
		if videoFlags.includeCost {
			row = append(row, fmt.Sprintf("%.4f", results[i].Cost))
		}
		rows = append(rows, row)
	}

	s.Stop()

	if downloadFailures == len(results) {
		return fmt.Errorf("all %d video downloads failed", len(results))
	}

	if err := output.Print(format, results, headers, rows); err != nil {
		return err
	}
	if downloadFailures > 0 {
		return fmt.Errorf("%d of %d video downloads failed", downloadFailures, len(results))
	}
	return nil
}
