package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/briandowns/spinner"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

var videoInferenceCmd = &cobra.Command{
	Use:   "videoInference [prompt]",
	Short: "Generate videos from text or image input",
	Long: `Generate videos using text-to-video or image-to-video.

Examples:
  runware videoInference "a timelapse of a sunset over mountains" --model klingai:5@3
  runware videoInference "a cat playing piano" --model google:3@2 --duration 5
  runware videoInference "animate this scene" --model klingai:5@3 --source ./photo.png`,
	Args: cobra.ExactArgs(1),
	RunE: runVideoInference,
}

func init() {
	f := videoInferenceCmd.Flags()
	f.String("model", "", "Model identifier (e.g. klingai:5@3, google:3@2)")
	f.Int("width", 0, "Video width in pixels")
	f.Int("height", 0, "Video height in pixels")
	f.Float64("duration", 0, "Video duration in seconds")
	f.Int("steps", 0, "Number of inference steps")
	f.Float64("cfg", 0, "CFG scale")
	f.Int64("seed", 0, "Seed for reproducibility")
	f.String("negative", "", "Negative prompt")
	f.Int("count", 1, "Number of videos to generate")
	f.String("output", "", "Output directory")
	f.String("output-format", "", "Video format: mp4, webm")
	f.Bool("no-download", false, "Print video URLs instead of downloading")
	f.String("source", "", "Source image path for image-to-video")
	f.String("source-last", "", "Last frame image path")
	f.Bool("include-cost", false, "Include cost info in response")
	f.String("preset", "", "Named preset to apply")
	f.Bool("dry-run", false, "Print the API request without executing")
	f.Int("poll-interval", 5, "Polling interval in seconds for async results")
	f.Int("timeout", 600, "Maximum wait time in seconds for video generation")

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
	key := config.GetAPIKey()
	if key == "" {
		output.Error("No API key configured. Run 'runware auth login' to authenticate.")
		return api.ErrNoAPIKey
	}

	cfg := config.Get()
	prompt := args[0]

	// Start with config defaults for shared fields
	model := cfg.Defaults.Model
	outputDir := cfg.Defaults.OutputDir

	// Apply preset if specified (reuses image preset structure for shared fields)
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

	width, _ := cmd.Flags().GetInt("width")
	height, _ := cmd.Flags().GetInt("height")
	duration, _ := cmd.Flags().GetFloat64("duration")
	steps, _ := cmd.Flags().GetInt("steps")
	cfgScale, _ := cmd.Flags().GetFloat64("cfg")
	seed, _ := cmd.Flags().GetInt64("seed")
	negative, _ := cmd.Flags().GetString("negative")
	count, _ := cmd.Flags().GetInt("count")
	outputFormat, _ := cmd.Flags().GetString("output-format")
	noDownload, _ := cmd.Flags().GetBool("no-download")
	includeCost, _ := cmd.Flags().GetBool("include-cost")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	sourcePath, _ := cmd.Flags().GetString("source")
	sourceLastPath, _ := cmd.Flags().GetString("source-last")
	pollInterval, _ := cmd.Flags().GetInt("poll-interval")
	timeout, _ := cmd.Flags().GetInt("timeout")

	// Build request
	req := &api.VideoInferenceRequest{
		TaskUUID:       api.NewUUID(),
		Model:          model,
		PositivePrompt: prompt,
		DeliveryMethod: api.DeliveryMethodAsync,
		IncludeCost:    includeCost,
	}

	if negative != "" {
		req.NegativePrompt = negative
	}
	if width > 0 {
		req.Width = width
	}
	if height > 0 {
		req.Height = height
	}
	if duration > 0 {
		req.Duration = duration
	}
	if steps > 0 {
		req.Steps = steps
	}
	if cfgScale > 0 {
		req.CFGScale = cfgScale
	}
	if cmd.Flags().Changed("seed") {
		req.Seed = seed
	}
	if count > 1 {
		req.NumberResults = count
	}
	if outputFormat != "" {
		req.OutputFormat = api.OutputFormat(outputFormat)
	}

	// Handle source images (image-to-video)
	if sourcePath != "" {
		encoded, err := encodeImageFile(sourcePath)
		if err != nil {
			return fmt.Errorf("failed to read source image: %w", err)
		}
		req.FrameImages = append(req.FrameImages, api.FrameImage{
			InputImage: encoded,
			Frame:      "first",
		})
	}
	if sourceLastPath != "" {
		encoded, err := encodeImageFile(sourceLastPath)
		if err != nil {
			return fmt.Errorf("failed to read last frame image: %w", err)
		}
		req.FrameImages = append(req.FrameImages, api.FrameImage{
			InputImage: encoded,
			Frame:      "last",
		})
	}

	// Dry run — print request and exit
	if dryRun {
		data, _ := json.MarshalIndent([]any{req}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Submit the video generation task
	var s *spinner.Spinner
	if output.IsTTY() {
		s = spinner.New(spinner.CharSets[14], 100*time.Millisecond, spinner.WithWriter(os.Stderr))
		s.Suffix = " Submitting video generation task..."
		s.Start()
	}

	client := api.NewClient(key, config.GetBaseURL(), flagVerbose)

	ctx, cancel := contextWithTimeout(cmd)
	defer cancel()

	submitResults, err := client.VideoInference(ctx, req)

	if err != nil {
		if s != nil {
			s.Stop()
		}
		if api.IsAuthError(err) {
			output.Error("Authentication failed. Run 'runware auth login' to set your API key.")
			return err
		}
		return err
	}

	// Poll for completion
	taskUUID := req.TaskUUID
	if len(submitResults) > 0 && submitResults[0].TaskUUID != "" {
		taskUUID = submitResults[0].TaskUUID
	}

	if s != nil {
		s.Suffix = " Generating video (this may take a few minutes)..."
	}

	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	interval := time.Duration(pollInterval) * time.Second
	var results []api.VideoInferenceResult

	for time.Now().Before(deadline) {
		time.Sleep(interval)

		rawData, err := client.GetResponse(ctx, taskUUID)
		if err != nil {
			// getResponse may return an error if results aren't ready yet — keep polling
			if flagVerbose {
				fmt.Fprintf(os.Stderr, "Poll: %s\n", err) //nolint:errcheck,gosec
			}
			continue
		}

		if len(rawData) == 0 {
			continue
		}

		// Parse results
		for _, raw := range rawData {
			var r api.VideoInferenceResult
			if err := json.Unmarshal(raw, &r); err != nil {
				continue
			}
			// Check if we have a video URL (indicates completion)
			if r.VideoURL != "" || r.MediaURL != "" {
				results = append(results, r)
			}
		}

		if len(results) > 0 {
			break
		}
	}

	if s != nil {
		s.Stop()
	}

	if len(results) == 0 {
		output.Error("Video generation timed out or returned no results")
		return fmt.Errorf("no video results after %ds", timeout)
	}

	// JSON/YAML output
	format := output.ParseFormat(getFormat())
	if format != output.FormatTable {
		return output.Print(format, results, nil, nil)
	}

	// Table output — download or print URLs
	if noDownload {
		headers := []any{tableHeaderNum, tableHeaderURL, tableHeaderSeed}
		var rows [][]any
		for i := range results {
			url := results[i].VideoURL
			if url == "" {
				url = results[i].MediaURL
			}
			row := []any{i + 1, url, results[i].Seed}
			if includeCost {
				row = append(row, fmt.Sprintf("%.4f", results[i].Cost))
			}
			rows = append(rows, row)
		}
		if includeCost {
			headers = append(headers, tableHeaderCost)
		}
		return output.Print(format, results, headers, rows)
	}

	// Download videos
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	headers := []any{tableHeaderNum, tableHeaderFile, tableHeaderSeed}
	if includeCost {
		headers = append(headers, tableHeaderCost)
	}

	var rows [][]any
	var downloadFailures int

	for i := range results {
		ext := outputFormat
		if ext == "" {
			ext = "mp4"
		}
		filename := fmt.Sprintf("runware_%s_%d.%s", time.Now().Format("20060102_150405"), i+1, ext)
		destPath := filepath.Join(outputDir, filename)

		url := results[i].VideoURL
		if url == "" {
			url = results[i].MediaURL
		}

		if err := downloadFile(ctx, url, destPath); err != nil {
			output.Error(fmt.Sprintf("Failed to download video %d: %s", i+1, err))
			downloadFailures++
			continue
		}

		row := []any{i + 1, destPath, results[i].Seed}
		if includeCost {
			row = append(row, fmt.Sprintf("%.4f", results[i].Cost))
		}
		rows = append(rows, row)
	}

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

// downloadFile downloads a URL to a local file (works for any file type).
func downloadFile(ctx context.Context, url, destPath string) error {
	return downloadImage(ctx, url, destPath)
}
