package preset

import (
	"fmt"

	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

func newSaveCmd() *cobra.Command {
	var flags struct {
		model     string
		width     int
		height    int
		steps     int
		cfgScale  float64
		scheduler string
	}

	cmd := &cobra.Command{
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
			if flags.model != "" {
				preset.Model = flags.model
			}
			if flags.width > 0 {
				preset.Width = flags.width
			}
			if flags.height > 0 {
				preset.Height = flags.height
			}
			if flags.steps > 0 {
				preset.Steps = flags.steps
			}
			if flags.cfgScale > 0 {
				preset.CFGScale = flags.cfgScale
			}
			if flags.scheduler != "" {
				preset.Scheduler = flags.scheduler
			}

			if err := config.SavePreset(name, preset); err != nil {
				return fmt.Errorf("failed to save preset: %w", err)
			}

			output.Success(fmt.Sprintf("Preset '%s' saved", name))
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.model, "model", "m", "", "Model identifier")
	f.IntVarP(&flags.width, "width", "W", 0, "Image width")
	f.IntVarP(&flags.height, "height", "H", 0, "Image height")
	f.IntVarP(&flags.steps, "steps", "s", 0, "Inference steps")
	f.Float64VarP(&flags.cfgScale, "cfg", "c", 0, "CFG scale")
	f.StringVarP(&flags.scheduler, "scheduler", "S", "", "Scheduler")
	return cmd
}
