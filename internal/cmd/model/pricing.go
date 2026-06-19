package model

import (
	"encoding/json"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// modelPricing renders a model's pricing as a configuration/price table.
type modelPricing struct {
	p *api.ModelPricing
}

func (r modelPricing) Headers() []string {
	return []string{"Configuration", "Price"}
}

func (r modelPricing) Rows() [][]any {
	rows := make([][]any, 0, len(r.p.PricingExamples))
	for _, ex := range r.p.PricingExamples {
		rows = append(rows, []any{ex.Configuration, ex.Price})
	}
	return rows
}

// MarshalJSON delegates to the raw pricing payload.
func (r modelPricing) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.p)
}

// MarshalYAML delegates to the raw pricing payload.
func (r modelPricing) MarshalYAML() (any, error) {
	return r.p, nil
}

func newPricingCmd(_ *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "pricing <air>",
		Short: "Show pricing for a model by AIR identifier",
		Example: `  # Pricing for a model
  runware model pricing google:gemini@3.1-pro

  # Output as JSON
  runware model pricing google:gemini@3.1-pro --format json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			air := args[0]

			spin := cmdutil.NewSpinner("Fetching pricing...")
			spin.Start()

			pricing, err := api.FetchModelPricing(cmd.Context(), air)
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), modelPricing{p: pricing})
		},
	}
}
