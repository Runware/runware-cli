package model

import (
	"encoding/json"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// modelExamples renders a model's example requests as a table. The full request
// and response payloads are available via --format json.
type modelExamples struct {
	examples []api.ModelExample
}

func (r modelExamples) Headers() []string {
	return []string{"Title", "Capability", "ID"}
}

func (r modelExamples) Rows() [][]any {
	rows := make([][]any, 0, len(r.examples))
	for _, ex := range r.examples {
		rows = append(rows, []any{orDash(ex.Title), ex.Capability, ex.ID})
	}
	return rows
}

// MarshalJSON delegates to the raw examples payload, which carries the full
// request and response for each example.
func (r modelExamples) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.examples)
}

// MarshalYAML delegates to the raw examples payload.
func (r modelExamples) MarshalYAML() (any, error) {
	return r.examples, nil
}

func newExamplesCmd(_ *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "examples <air>",
		Short: "Show example requests for a model by AIR identifier",
		Example: `  # Examples for a model
  runware model examples google:gemini@3.1-pro

  # Full request and response payloads
  runware model examples google:gemini@3.1-pro --format json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			air := args[0]

			spin := cmdutil.NewSpinner("Fetching examples...")
			spin.Start()

			examples, err := api.FetchModelExamples(cmd.Context(), air)
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), modelExamples{examples: examples})
		},
	}
}
