package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	rhttp "github.com/runware/runware-cli/pkg/http"
	"github.com/spf13/cobra"
)

var imageFlags struct {
	model           string
	width           int
	height          int
	steps           int
	cfgScale        float64
	scheduler       string
	seed            int64
	negative        string
	count           int
	outputDir       string
	outputFormat    string
	noDownload      bool
	sourcePath      string
	strength        float64
	maskPath        string
	preset          string
	dryRun          bool
	downloadTimeout time.Duration
}

var imageInferenceCmd = &cobra.Command{
	Use:   "image [prompt]",
	Short: "Generate images from text or image input",
	Long: `Generate images using text-to-image, image-to-image, or inpainting.

Examples:
  runware inference image "a cat riding a rocket"
  runware inference image "make it cinematic" --source ./input.png --strength 0.7
  runware inference image "replace with a dog" --source ./photo.png --mask ./mask.png`,
	Args:    cobra.ExactArgs(1),
	PreRunE: preRunImageInference,
	RunE:    runImageInference,
}

func init() {
	f := imageInferenceCmd.Flags()
	f.StringVarP(&imageFlags.model, "model", "m", "", "Model identifier")
	f.IntVarP(&imageFlags.width, "width", "W", 0, "Image width")
	f.IntVarP(&imageFlags.height, "height", "H", 0, "Image height")
	f.IntVarP(&imageFlags.steps, "steps", "s", 0, "Number of inference steps")
	f.Float64VarP(&imageFlags.cfgScale, "cfg", "c", 0, "CFG scale")
	f.StringVarP(&imageFlags.scheduler, "scheduler", "S", "", "Scheduler (e.g. euler, dpm++)")
	f.Int64VarP(&imageFlags.seed, "seed", "e", 0, "Seed for reproducibility")
	f.StringVarP(&imageFlags.negative, "negative", "N", "", "Negative prompt")
	f.IntVarP(&imageFlags.count, "count", "n", 1, "Number of images to generate")
	f.StringVarP(&imageFlags.outputDir, "output", "o", "", "Output directory")
	f.StringVarP(&imageFlags.outputFormat, "output-format", "f", "", "Format of generated images: png, jpg, webp")
	f.BoolVarP(&imageFlags.noDownload, "no-download", "D", false, "Print image URLs instead of downloading")
	f.StringVarP(&imageFlags.sourcePath, "source", "i", "", "Source image path for img2img")
	f.Float64VarP(&imageFlags.strength, "strength", "R", 0.7, "img2img strength (0.0-1.0)")
	f.StringVarP(&imageFlags.maskPath, "mask", "M", "", "Mask image path for inpainting")
	f.StringVarP(&imageFlags.preset, "preset", "p", "", "Named preset to apply")
	f.BoolVarP(&imageFlags.dryRun, "dry-run", "X", false, "Print the API request without executing")
	f.DurationVarP(&imageFlags.downloadTimeout, "download-timeout", "T", defaultImageDownloadTimeout, "Timeout for downloading image results")

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
	config.Init() //nolint:errcheck,gosec
	return config.ListPresets(), cobra.ShellCompDirectiveNoFileComp
}

func preRunImageInference(cmd *cobra.Command, _ []string) error {
	if imageFlags.maskPath != "" && imageFlags.sourcePath == "" {
		return fmt.Errorf("--mask requires --source to be set")
	}
	return nil
}

func runImageInference(cmd *cobra.Command, args []string) error {
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

	// Start with config defaults
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
	if imageFlags.preset != "" {
		preset := config.GetPreset(imageFlags.preset)
		if preset == nil {
			return fmt.Errorf("preset '%s' not found", imageFlags.preset)
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
		model = imageFlags.model
	}
	if cmd.Flags().Changed("width") {
		width = imageFlags.width
	}
	if cmd.Flags().Changed("height") {
		height = imageFlags.height
	}
	if cmd.Flags().Changed("steps") {
		steps = imageFlags.steps
	}
	if cmd.Flags().Changed("cfg") {
		cfgScale = imageFlags.cfgScale
	}
	if cmd.Flags().Changed("scheduler") {
		scheduler = imageFlags.scheduler
	}
	if cmd.Flags().Changed("output") {
		outputDir = imageFlags.outputDir
	}
	if cmd.Flags().Changed("output-format") {
		outputFormat = imageFlags.outputFormat
	}

	// Build request
	req := &api.ImageInferenceRequest{
		TaskUUID:       api.NewUUID(),
		PositivePrompt: prompt,
		Model:          model,
		Width:          width,
		Height:         height,
		NumberResults:  imageFlags.count,
		OutputFormat:   api.OutputFormat(outputFormat),
	}

	if steps > 0 {
		req.Steps = steps
	}
	if cfgScale > 0 {
		req.CFGScale = cfgScale
	}
	if scheduler != "" {
		req.Scheduler = scheduler
	}
	if imageFlags.negative != "" {
		req.NegativePrompt = imageFlags.negative
	}
	if cmd.Flags().Changed("seed") {
		req.Seed = imageFlags.seed
	}

	// Handle source image (img2img)
	if imageFlags.sourcePath != "" {
		encoded, err := encodeImageFile(imageFlags.sourcePath)
		if err != nil {
			return fmt.Errorf("failed to read source image: %w", err)
		}
		req.InputImage = encoded
		req.Strength = imageFlags.strength
	}

	// Handle mask image (inpainting)
	if imageFlags.maskPath != "" {
		encoded, err := encodeImageFile(imageFlags.maskPath)
		if err != nil {
			return fmt.Errorf("failed to read mask image: %w", err)
		}
		req.MaskImage = encoded
	}

	// Dry run
	if imageFlags.dryRun {
		data, _ := json.MarshalIndent([]any{req}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Start spinner
	s := output.NewSpinner(" Generating image...")
	s.Start()

	client := api.NewClient(key, config.GetBaseURL(), flagVerbose)
	results, err := client.ImageInference(ctx, req)
	if err != nil {
		s.Stop()
		if api.IsAuthError(err) {
			output.Error("Authentication failed. Run 'runware auth login' to set your API key.")
			return err
		}
		return err
	}

	if len(results) == 0 {
		s.Stop()
		output.Error("No images returned")
		return fmt.Errorf("empty result")
	}

	// JSON/YAML output
	format := output.ParseFormat(getFormat())
	if format != output.FormatTable {
		s.Stop()
		return output.Print(format, results, nil, nil)
	}

	// Table output — no-download: print URLs
	if imageFlags.noDownload {
		headers := []any{tableHeaderNum, tableHeaderURL, tableHeaderSeed}
		var rows [][]any
		for i, r := range results {
			rows = append(rows, []any{i + 1, r.ImageURL, r.Seed})
		}
		s.Stop()
		return output.Print(format, results, headers, rows)
	}

	s.Suffix(" Downloading image results...")

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

		if err := rhttp.Download(ctx, r.ImageURL, destPath, imageFlags.downloadTimeout); err != nil {
			output.Error(fmt.Sprintf("Failed to download image %d: %s", i+1, err))
			continue
		}

		rows = append(rows, []any{i + 1, destPath, r.Seed})
	}

	s.Stop()
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
