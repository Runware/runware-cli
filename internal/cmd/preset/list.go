package preset

import (
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

type presetListResult []presetRow

type presetRow struct {
	Name   string            `json:"name"`
	Model  string            `json:"model"`
	Params map[string]string `json:"params,omitempty"`
}

// paramssummary returns a compact human-readable summary of the params map,
// e.g. "height=768, width=512". Keys are sorted alphabetically.
func paramsummary(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := slices.Sorted(maps.Keys(params))
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, ", ")
}

func (r presetListResult) Headers() []string {
	return []string{"Name", "Model", "Params"}
}

func (r presetListResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i, p := range r {
		rows[i] = []any{p.Name, p.Model, paramsummary(p.Params)}
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
			Name:   name,
			Model:  p.Model,
			Params: p.Params,
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
			format := cmdutil.FormatFor(cmd)

			if len(cfg.Presets) == 0 {
				if format == output.FormatTable {
					logger.Info("No presets configured. Use 'runware preset save <name>' to create one.")
					return nil
				}
				return output.Print(format, presetListResult{})
			}

			return output.Print(format, buildPresetList(cfg.Presets))
		},
	}
}
