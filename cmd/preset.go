package cmd

import (
	"fmt"
	"sort"

	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

var presetSaveFlags struct {
	model     string
	width     int
	height    int
	steps     int
	cfgScale  float64
	scheduler string
}

var presetCmd = &cobra.Command{
	Use:   "preset",
	Short: "Manage named presets",
}

var presetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all saved presets",
	Example: `  # list all saved presets
  runware preset list`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Get()
		format := output.ParseFormat(getFormat())

		if format != output.FormatTable {
			return output.Print(format, cfg.Presets, nil, nil)
		}

		names := make([]string, 0, len(cfg.Presets))
		for name := range cfg.Presets {
			names = append(names, name)
		}
		sort.Strings(names)

		headers := []any{"Name", "Model", "Width", "Height", "Steps", "CFG", "Scheduler"}
		var rows [][]any
		for _, name := range names {
			p := cfg.Presets[name]
			rows = append(rows, []any{
				name, p.Model, p.Width, p.Height, p.Steps, p.CFGScale, p.Scheduler,
			})
		}

		if len(rows) == 0 {
			output.Info("No presets configured. Use 'runware preset save <name>' to create one.")
			return nil
		}

		return output.Print(format, cfg.Presets, headers, rows)
	},
}

var presetShowCmd = &cobra.Command{
	Use:   "show [name]",
	Short: "Show preset details",
	Example: `  # show details of a preset
  runware preset show portrait`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completePresetNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		preset := config.GetPreset(args[0])
		if preset == nil {
			return fmt.Errorf("preset '%s' not found", args[0])
		}

		format := output.ParseFormat(getFormat())
		if format != output.FormatTable {
			return output.Print(format, preset, nil, nil)
		}

		return output.Print(format, preset,
			[]any{"Setting", "Value"},
			[][]any{
				{"Model", preset.Model},
				{"Width", preset.Width},
				{"Height", preset.Height},
				{"Steps", preset.Steps},
				{"CFG Scale", preset.CFGScale},
				{"Scheduler", preset.Scheduler},
			},
		)
	},
}

var presetSaveCmd = &cobra.Command{
	Use:   "save [name]",
	Short: "Save a named preset",
	Example: `  # save a preset with model and dimensions
  runware preset save portrait --model runware:100@1 --width 512 --height 768

  # save a preset with steps and cfg
  runware preset save fast --model runware:100@1 --steps 20 --cfg 7`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		preset := config.Preset{}
		if presetSaveFlags.model != "" {
			preset.Model = presetSaveFlags.model
		}
		if presetSaveFlags.width > 0 {
			preset.Width = presetSaveFlags.width
		}
		if presetSaveFlags.height > 0 {
			preset.Height = presetSaveFlags.height
		}
		if presetSaveFlags.steps > 0 {
			preset.Steps = presetSaveFlags.steps
		}
		if presetSaveFlags.cfgScale > 0 {
			preset.CFGScale = presetSaveFlags.cfgScale
		}
		if presetSaveFlags.scheduler != "" {
			preset.Scheduler = presetSaveFlags.scheduler
		}

		if err := config.SavePreset(name, preset); err != nil {
			return fmt.Errorf("failed to save preset: %w", err)
		}

		output.Success(fmt.Sprintf("Preset '%s' saved", name))
		return nil
	},
}

var presetDeleteCmd = &cobra.Command{
	Use:   "delete [name]",
	Short: "Delete a preset",
	Example: `  # delete a preset
  runware preset delete portrait`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completePresetNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if config.GetPreset(name) == nil {
			return fmt.Errorf("preset '%s' not found", name)
		}

		if err := config.DeletePreset(name); err != nil {
			return fmt.Errorf("failed to delete preset: %w", err)
		}

		output.Success(fmt.Sprintf("Preset '%s' deleted", name))
		return nil
	},
}

func init() {
	f := presetSaveCmd.Flags()
	f.StringVarP(&presetSaveFlags.model, "model", "m", "", "Model identifier")
	f.IntVarP(&presetSaveFlags.width, "width", "W", 0, "Image width")
	f.IntVarP(&presetSaveFlags.height, "height", "H", 0, "Image height")
	f.IntVarP(&presetSaveFlags.steps, "steps", "s", 0, "Inference steps")
	f.Float64VarP(&presetSaveFlags.cfgScale, "cfg", "c", 0, "CFG scale")
	f.StringVarP(&presetSaveFlags.scheduler, "scheduler", "S", "", "Scheduler")

	presetCmd.AddCommand(presetListCmd)
	presetCmd.AddCommand(presetShowCmd)
	presetCmd.AddCommand(presetSaveCmd)
	presetCmd.AddCommand(presetDeleteCmd)
}
