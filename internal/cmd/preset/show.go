package preset

import (
	"fmt"

	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

type presetShowResult struct {
	Name      string  `json:"name"`
	Model     string  `json:"model"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Steps     int     `json:"steps"`
	CFGScale  float64 `json:"cfg_scale"`
	Scheduler string  `json:"scheduler"`
}

func (r presetShowResult) Headers() []string {
	return []string{"Setting", "Value"}
}

func (r presetShowResult) Rows() [][]any {
	return [][]any{
		{"Name", r.Name},
		{"Model", r.Model},
		{"Width", r.Width},
		{"Height", r.Height},
		{"Steps", r.Steps},
		{"CFG Scale", r.CFGScale},
		{"Scheduler", r.Scheduler},
	}
}

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [name]",
		Short: "Show preset details",
		Example: `  # show details of a preset
  runware preset show portrait`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completePresetNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := config.GetPreset(args[0])
			if p == nil {
				return fmt.Errorf("preset '%s' not found", args[0])
			}

			return output.Print(cmdutil.FormatFor(cmd), presetShowResult{
				Name:      args[0],
				Model:     p.Model,
				Width:     p.Width,
				Height:    p.Height,
				Steps:     p.Steps,
				CFGScale:  p.CFGScale,
				Scheduler: p.Scheduler,
			})
		},
	}
}
