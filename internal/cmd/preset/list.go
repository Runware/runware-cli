package preset

import (
	"sort"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

type presetListResult []presetRow

type presetRow struct {
	Name      string  `json:"name"`
	Model     string  `json:"model"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Steps     int     `json:"steps"`
	CFGScale  float64 `json:"cfg_scale"`
	Scheduler string  `json:"scheduler"`
}

func (r presetListResult) Headers() []string {
	return []string{"Name", "Model", "Width", "Height", "Steps", "CFG", "Scheduler"}
}

func (r presetListResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i, p := range r {
		rows[i] = []any{p.Name, p.Model, p.Width, p.Height, p.Steps, p.CFGScale, p.Scheduler}
	}
	return rows
}

func buildPresetList(presets map[string]config.Preset) presetListResult {
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make(presetListResult, len(names))
	for i, name := range names {
		p := presets[name]
		result[i] = presetRow{
			Name:      name,
			Model:     p.Model,
			Width:     p.Width,
			Height:    p.Height,
			Steps:     p.Steps,
			CFGScale:  p.CFGScale,
			Scheduler: p.Scheduler,
		}
	}
	return result
}

func newListCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all saved presets",
		Example: `  # list all saved presets
  runware preset list`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Get()

			if len(cfg.Presets) == 0 {
				logger.Info("No presets configured. Use 'runware preset save <name>' to create one.")
				return nil
			}

			return output.Print(cmdutil.FormatFor(cmd), buildPresetList(cfg.Presets))
		},
	}
}
