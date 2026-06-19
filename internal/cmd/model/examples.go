package model

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// modelExamples carries example payloads for JSON/YAML output. The default
// (human) output is a runnable `runware run` command per example, printed by
// the command itself.
type modelExamples struct {
	examples []api.ModelExample
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

const fieldModel = "model"

var skipRequestKeys = map[string]bool{"taskType": true, "taskUUID": true, fieldModel: true}

// requestToCommand renders an example request as a copy-pasteable `runware run`
// command. Nested objects and arrays become the CLI's dot-notation key=value
// form, and the fields the CLI sets itself (taskType, taskUUID, model) are
// skipped.
func requestToCommand(air string, req map[string]any) string {
	var b strings.Builder
	b.WriteString("runware run ")
	b.WriteString(air)
	for _, pair := range flattenRequest(req) {
		b.WriteByte(' ')
		b.WriteString(pair)
	}
	return b.String()
}

func flattenRequest(req map[string]any) []string {
	var pairs []string
	for k, v := range req {
		if skipRequestKeys[k] {
			continue
		}
		flattenValue(k, v, &pairs)
	}
	sort.Strings(pairs)
	return pairs
}

func flattenValue(prefix string, v any, out *[]string) {
	switch val := v.(type) {
	case map[string]any:
		for k, sub := range val {
			flattenValue(prefix+"."+k, sub, out)
		}
	case []any:
		for i, item := range val {
			flattenValue(fmt.Sprintf("%s.%d", prefix, i), item, out)
		}
	default:
		*out = append(*out, prefix+"="+formatScalar(val))
	}
}

func formatScalar(v any) string {
	switch val := v.(type) {
	case string:
		return quoteArg(val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		if val == math.Trunc(val) && math.Abs(val) < 1e15 {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'g', -1, 64)
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%v", val)
	}
}

// quoteArg wraps a string in double quotes when it contains characters a shell
// or the key=value parser would mishandle.
func quoteArg(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		safe := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("._/:@-", r)
		if !safe {
			return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
		}
	}
	return s
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

			if format := cmdutil.FormatFor(cmd); format != output.FormatTable {
				return output.Print(format, modelExamples{examples: examples})
			}

			var b strings.Builder
			for i, ex := range examples {
				if i > 0 {
					b.WriteByte('\n')
				}
				header := orDash(ex.Title)
				if ex.Capability != "" {
					header += " · " + ex.Capability
				}
				air, _ := ex.Request[fieldModel].(string)
				if air == "" {
					air = id
				}
				b.WriteString(header)
				b.WriteByte('\n')
				b.WriteString("  ")
				b.WriteString(requestToCommand(air, ex.Request))
				b.WriteByte('\n')
			}

			_, err = fmt.Fprint(os.Stdout, b.String())
			return err
		},
	}
}
