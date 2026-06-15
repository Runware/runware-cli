package model

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// modelDetail renders a single model as a vertical key-value table.
type modelDetail struct {
	m *api.ModelResult
}

func (d modelDetail) Headers() []string {
	return []string{"Field", "Value"}
}

func (d modelDetail) Rows() [][]any {
	m := d.m
	rows := [][]any{
		{"Name", m.Name},
		{colAIR, m.AIR},
		{"Version", m.Version},
		{"Category", m.Category},
		{"Architecture", m.Architecture},
		{colType, orDash(m.Type)},
		{"Base Model", orDash(m.BaseModel)},
		{"Private", m.Private},
	}

	if m.DefaultWidth != 0 || m.DefaultHeight != 0 {
		rows = append(rows, []any{"Default Size", formatDefaultSize(m.DefaultWidth, m.DefaultHeight)})
	}
	if m.DefaultSteps != 0 {
		rows = append(rows, []any{"Default Steps", m.DefaultSteps})
	}
	if m.DefaultScheduler != "" {
		rows = append(rows, []any{"Default Scheduler", m.DefaultScheduler})
	}
	if m.DefaultCFG != 0 {
		rows = append(rows, []any{"Default CFG", m.DefaultCFG})
	}
	if m.DefaultStrength != 0 {
		rows = append(rows, []any{"Default Strength", m.DefaultStrength})
	}
	if m.DefaultWeight != 0 {
		rows = append(rows, []any{"Default Weight", m.DefaultWeight})
	}
	if m.PositiveTriggerWords != "" {
		rows = append(rows, []any{"Positive Triggers", m.PositiveTriggerWords})
	}
	if m.NegativeTriggerWords != "" {
		rows = append(rows, []any{"Negative Triggers", m.NegativeTriggerWords})
	}
	if len(m.Tags) > 0 {
		rows = append(rows, []any{"Tags", strings.Join(m.Tags, ", ")})
	}
	if m.ImageURL != "" {
		rows = append(rows, []any{"Image URL", m.ImageURL})
	}

	return rows
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// MarshalJSON delegates to the underlying ModelResult so JSON output contains the raw model data.
func (d modelDetail) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.m)
}

// MarshalYAML delegates to the underlying ModelResult so YAML output contains the raw model data.
func (d modelDetail) MarshalYAML() (any, error) {
	return d.m, nil
}

func newShowCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "show <air>",
		Short: "Show full details for a model by AIR identifier",
		Example: `  # Show details for a specific model
  runware model show civitai:305149@392545

  # Output as JSON
  runware model show civitai:305149@392545 --format json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			air := args[0]

			spin := cmdutil.NewSpinner("Fetching model details...")
			spin.Start()

			t, err := cmdutil.NewTransport(cmd, slog.New(logger))
			if err != nil {
				spin.Stop()
				return err
			}
			defer t.Close() //nolint:errcheck

			client := api.NewClient(t, slog.New(logger))

			result, err := client.ModelSearch(cmd.Context(), api.ModelSearchRequest{
				Search: air,
				Limit:  100,
			})
			if err != nil {
				spin.Stop()
				return err
			}

			spin.Stop()
			var found *api.ModelResult
			for i := range result.Results {
				if result.Results[i].AIR == air {
					found = &result.Results[i]
					break
				}
			}
			if found == nil {
				return fmt.Errorf("model not found: %s", air)
			}

			return output.Print(cmdutil.FormatFor(cmd), modelDetail{m: found})
		},
	}
}
