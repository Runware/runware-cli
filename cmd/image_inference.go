package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

var imageInferenceCmd = &cobra.Command{
	Use:   "imageInference [prompt]",
	Short: "Generate images from text or image input",
	Long: `Generate images using text-to-image, image-to-image, or inpainting.

Examples:
  runware imageInference "a cat riding a rocket"
  runware imageInference "make it cinematic" --source ./input.png --strength 0.7
  runware imageInference "replace with a dog" --source ./photo.png --mask ./mask.png`,
	Args: cobra.ExactArgs(1),
	RunE: runImageInference,
}

func init() {
	f := imageInferenceCmd.Flags()
	f.String("model", "", "Model identifier")
	f.Int("width", 0, "Image width")
	f.Int("height", 0, "Image height")
	f.Int("steps", 0, "Number of inference steps")
	f.Float64("cfg", 0, "CFG scale")
	f.String("scheduler", "", "Scheduler (e.g. euler, dpm++)")
	f.Int64("seed", 0, "Seed for reproducibility")
	f.String("negative", "", "Negative prompt")
	f.Int("count", 1, "Number of images to generate")
	f.String("output", "", "Output directory")
	f.String("output-format", "", "Image format: png, jpg, webp")
	f.Bool("no-download", false, "Print image URLs instead of downloading")
	f.String("source", "", "Source image path for img2img")
	f.Float64("strength", 0.7, "img2img strength (0.0-1.0)")
	f.String("mask", "", "Mask image path for inpainting")
	f.String("preset", "", "Named preset to apply")
	f.Bool("dry-run", false, "Print the API request without executing")

	imageInferenceCmd.RegisterFlagCompletionFunc("scheduler", func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{
			"euler\tEuler",
			"euler_a\tEuler Ancestral",
			"dpm++_2m\tDPM++ 2M",
			"dpm++_2m_karras\tDPM++ 2M Karras",
			"dpm++_sde\tDPM++ SDE",
			"dpm++_sde_karras\tDPM++ SDE Karras",
			"ddim\tDDIM",
			"lms\tLMS",
			"lms_karras\tLMS Karras",
			"heun\tHeun",
			"dpm_2\tDPM 2",
			"dpm_2_karras\tDPM 2 Karras",
			"dpm_2_a\tDPM 2 Ancestral",
			"dpm_2_a_karras\tDPM 2 Ancestral Karras",
			"uni_pc\tUniPC",
		}, cobra.ShellCompDirectiveNoFileComp
	})

	imageInferenceCmd.RegisterFlagCompletionFunc("output-format", func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{string(api.OutputFormatPNG), string(api.OutputFormatJPG), string(api.OutputFormatWebP)}, cobra.ShellCompDirectiveNoFileComp
	})

	imageInferenceCmd.RegisterFlagCompletionFunc("preset", completePresetNames) //nolint:errcheck,gosec

	imageInferenceCmd.RegisterFlagCompletionFunc("source", func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{string(api.OutputFormatPNG), string(api.OutputFormatJPG), string(api.OutputFormatJPEG), string(api.OutputFormatWebP)}, cobra.ShellCompDirectiveFilterFileExt
	})

	imageInferenceCmd.RegisterFlagCompletionFunc("mask", func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{string(api.OutputFormatPNG), string(api.OutputFormatJPG), string(api.OutputFormatJPEG), string(api.OutputFormatWebP)}, cobra.ShellCompDirectiveFilterFileExt
	})
}

// completePresetNames provides dynamic completion for preset names from config.
func completePresetNames(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	// Ensure config is loaded for completion context
	config.Init() //nolint:errcheck,gosec
	return config.ListPresets(), cobra.ShellCompDirectiveNoFileComp
}

func runImageInference(cmd *cobra.Command, args []string) error {
	key := config.GetAPIKey()
	if key == "" {
		output.Error("No API key configured. Run 'runware auth login' to authenticate.")
		return api.ErrNoAPIKey
	}

	cfg := config.Get()
	prompt := args[0]

	// Start with config defaults for universal fields
	model := cfg.Defaults.Model
	width := cfg.Defaults.Width
	height := cfg.Defaults.Height
	outputDir := cfg.Defaults.OutputDir
	outputFormat := cfg.Defaults.OutputFormat

	// Model-specific fields — only sent when explicitly set via flag or preset
	var steps int
	var cfgScale float64
	var scheduler string

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
		if preset.Width > 0 {
			width = preset.Width
		}
		if preset.Height > 0 {
			height = preset.Height
		}
		if preset.Steps > 0 {
			steps = preset.Steps
		}
		if preset.CFGScale > 0 {
			cfgScale = preset.CFGScale
		}
		if preset.Scheduler != "" {
			scheduler = preset.Scheduler
		}
	}

	// Override with explicit CLI flags
	if cmd.Flags().Changed("model") {
		model, _ = cmd.Flags().GetString("model")
	}
	if cmd.Flags().Changed("width") {
		width, _ = cmd.Flags().GetInt("width")
	}
	if cmd.Flags().Changed("height") {
		height, _ = cmd.Flags().GetInt("height")
	}
	if cmd.Flags().Changed("steps") {
		steps, _ = cmd.Flags().GetInt("steps")
	}
	if cmd.Flags().Changed("cfg") {
		cfgScale, _ = cmd.Flags().GetFloat64("cfg")
	}
	if cmd.Flags().Changed("scheduler") {
		scheduler, _ = cmd.Flags().GetString("scheduler")
	}
	if cmd.Flags().Changed("output") {
		outputDir, _ = cmd.Flags().GetString("output")
	}
	if cmd.Flags().Changed("output-format") {
		outputFormat, _ = cmd.Flags().GetString("output-format")
	}

	count, _ := cmd.Flags().GetInt("count")
	seed, _ := cmd.Flags().GetInt64("seed")
	negative, _ := cmd.Flags().GetString("negative")
	noDownload, _ := cmd.Flags().GetBool("no-download")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	sourcePath, _ := cmd.Flags().GetString("source")
	strength, _ := cmd.Flags().GetFloat64("strength")
	maskPath, _ := cmd.Flags().GetString("mask")

	// Validation
	if maskPath != "" && sourcePath == "" {
		return fmt.Errorf("--mask requires --source to be set")
	}

	// Build request — universal fields always included
	req := &api.ImageInferenceRequest{
		TaskUUID:       api.NewUUID(),
		PositivePrompt: prompt,
		Model:          model,
		Width:          width,
		Height:         height,
		NumberResults:  count,
		OutputFormat:   api.OutputFormat(outputFormat),
	}

	// Model-specific fields — only included when explicitly set
	if steps > 0 {
		req.Steps = steps
	}
	if cfgScale > 0 {
		req.CFGScale = cfgScale
	}
	if scheduler != "" {
		req.Scheduler = scheduler
	}
	if negative != "" {
		req.NegativePrompt = negative
	}
	if cmd.Flags().Changed("seed") {
		req.Seed = seed
	}

	// Handle source image (img2img)
	if sourcePath != "" {
		encoded, err := encodeImageFile(sourcePath)
		if err != nil {
			return fmt.Errorf("failed to read source image: %w", err)
		}
		req.InputImage = encoded
		req.Strength = strength
	}

	// Handle mask image (inpainting)
	if maskPath != "" {
		encoded, err := encodeImageFile(maskPath)
		if err != nil {
			return fmt.Errorf("failed to read mask image: %w", err)
		}
		req.MaskImage = encoded
	}

	// Dry run — print request and exit
	if dryRun {
		data, _ := json.MarshalIndent([]any{req}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Start spinner
	var s *spinner.Spinner
	if output.IsTTY() {
		s = spinner.New(spinner.CharSets[14], 100*time.Millisecond, spinner.WithWriter(os.Stderr))
		s.Suffix = " Generating image..."
		s.Start()
	}

	client := api.NewClient(key, config.GetBaseURL(), flagVerbose)
	results, err := client.ImageInference(context.Background(), req)

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
		output.Error("No images returned")
		return fmt.Errorf("empty result")
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
		for i, r := range results {
			rows = append(rows, []any{i + 1, r.ImageURL, r.Seed})
		}
		return output.Print(format, results, headers, rows)
	}

	// Download images
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	headers := []any{tableHeaderNum, tableHeaderFile, tableHeaderSeed}
	var rows [][]any

	for i, r := range results {
		ext := outputFormat
		if ext == "" {
			ext = string(api.OutputFormatJPG)
		}
		filename := fmt.Sprintf("runware_%s_%d.%s", time.Now().Format("20060102_150405"), i+1, ext)
		destPath := filepath.Join(outputDir, filename)

		if err := downloadImage(r.ImageURL, destPath); err != nil {
			output.Error(fmt.Sprintf("Failed to download image %d: %s", i+1, err))
			continue
		}

		rows = append(rows, []any{i + 1, destPath, r.Seed})
	}

	return output.Print(format, results, headers, rows)
}

// encodeImageFile reads a file and returns a base64 data URI.
func encodeImageFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	ext := strings.ToLower(filepath.Ext(path))
	var mime string
	switch ext {
	case ".png":
		mime = "image/png"
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".webp":
		mime = "image/webp"
	default:
		mime = "image/png"
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", mime, encoded), nil
}

// downloadClient is used for fetching generated images. It enforces an overall
// timeout so a hung server cannot stall the CLI indefinitely.
var downloadClient = &http.Client{Timeout: 5 * time.Minute}

// downloadImage downloads a URL to a local file.
func downloadImage(url, destPath string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck,gosec

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close() //nolint:errcheck,gosec

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
