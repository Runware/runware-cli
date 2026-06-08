package preset

import (
	"fmt"
	"maps"
	"slices"

	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

type presetShowResult struct {
	Name   string            `json:"name"`
	Model  string            `json:"model"`
	Params map[string]string `json:"params,omitempty"`
}

func (r presetShowResult) Headers() []string {
	return []string{"Setting", "Value"}
}

func (r presetShowResult) Rows() [][]any {
	rows := [][]any{
		{"name", r.Name},
		{"model", r.Model},
	}
	for _, k := range slices.Sorted(maps.Keys(r.Params)) {
		rows = append(rows, []any{k, r.Params[k]})
	}
	return rows
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
				Name:   args[0],
				Model:  p.Model,
				Params: p.Params,
			})
		},
	}
}
