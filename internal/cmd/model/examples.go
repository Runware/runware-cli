package model

import (
	"encoding/json"
	"strings"

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
	return []string{"Title", "Capability", "Prompt"}
}

func (r modelExamples) Rows() [][]any {
	rows := make([][]any, 0, len(r.examples))
	for _, ex := range r.examples {
		rows = append(rows, []any{orDash(ex.Title), ex.Capability, orDash(promptPreview(ex.Request, 60))})
	}
	return rows
}

// promptPreview pulls a human-readable prompt out of an example request so the
// table conveys what the example does. It tries positivePrompt, then the last
// user message, collapses whitespace, and truncates to limit runes.
func promptPreview(req map[string]any, limit int) string {
	text := ""
	if p, ok := req["positivePrompt"].(string); ok {
		text = p
	} else if msgs, ok := req["messages"].([]any); ok {
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok || msg["role"] != "user" {
				continue
			}
			switch c := msg["content"].(type) {
			case string:
				text = c
			case []any:
				for _, part := range c {
					if pm, ok := part.(map[string]any); ok {
						if t, ok := pm["text"].(string); ok && t != "" {
							text = t
						}
					}
				}
			}
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > limit {
		return string(r[:limit-1]) + "…"
	}
	return text
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
		Use:   "examples <model>",
		Short: "Show example requests for a model",
		Example: `  # Examples for a model
  runware model examples google:gemini@3.1-pro

  # Full request and response payloads
  runware model examples google:gemini@3.1-pro --format json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			spin := cmdutil.NewSpinner("Fetching examples...")
			spin.Start()

			examples, err := api.FetchModelExamples(cmd.Context(), id)
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), modelExamples{examples: examples})
		},
	}
}
