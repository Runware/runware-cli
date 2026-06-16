package model

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/runware/runware-cli/internal/schema"
	"github.com/spf13/cobra"
)

// colType and colAIR are shared column/field labels used across model table views.
const (
	colType           = "Type"
	colAIR            = "AIR"
	defaultEmptyValue = "—"
)

// table rendering. Only the fields needed for display are captured.
type schemaNode struct {
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Type        schema.SchemaType     `json:"type"`
	Default     json.RawMessage       `json:"default"`
	Properties  map[string]schemaNode `json:"properties"`
	Required    []string              `json:"required"`
}

// schemaRow is a single rendered row in the parameter table.
type schemaRow struct {
	name     string // indented parameter name
	typeStr  string
	required string // "yes" / "no"
	def      string // formatted default value, or "—"
	desc     string
}

// modelSchemaTable implements output.Tabular for schema parameter display.
type modelSchemaTable struct {
	rows []schemaRow
}

func (t modelSchemaTable) Headers() []string {
	return []string{"Parameter", colType, "Required", "Default", "Description"}
}

func (t modelSchemaTable) Rows() [][]any {
	rows := make([][]any, len(t.rows))
	for i := range t.rows {
		r := &t.rows[i]
		rows[i] = []any{r.name, r.typeStr, r.required, r.def, r.desc}
	}
	return rows
}

// buildRows recursively flattens a schemaNode's properties into schemaRows.
// Required fields are emitted first (sorted), then optional fields (sorted).
// Depth is capped at maxDepth to guard against pathological schemas.
func buildRows(node schemaNode, requiredSet map[string]struct{}, depth, maxDepth int) []schemaRow {
	if depth > maxDepth || len(node.Properties) == 0 {
		return nil
	}

	indent := strings.Repeat("  ", depth)

	// Partition into required and optional, both sorted alphabetically.
	var req, opt []string
	for name := range node.Properties {
		if _, ok := requiredSet[name]; ok {
			req = append(req, name)
		} else {
			opt = append(opt, name)
		}
	}
	sort.Strings(req)
	sort.Strings(opt)
	names := append(req, opt...) //nolint:gocritic

	var rows []schemaRow
	for _, name := range names {
		child := node.Properties[name]

		required := "no"
		if _, ok := requiredSet[name]; ok {
			required = "yes"
		}

		rows = append(rows, schemaRow{
			name:     indent + name,
			typeStr:  formatSchemaType(child),
			required: required,
			def:      formatSchemaDefault(child.Default),
			desc:     child.Description,
		})

		// Recurse into nested objects.
		if len(child.Properties) > 0 {
			childRequired := stringSet(child.Required)
			rows = append(rows, buildRows(child, childRequired, depth+1, maxDepth)...)
		}
	}

	return rows
}

// formatSchemaType returns a display string for a schema node's type.
func formatSchemaType(node schemaNode) string {
	if node.Type != "" {
		return string(node.Type)
	}
	return defaultEmptyValue
}

// formatSchemaDefault renders a json.RawMessage default value as a plain string.
// Strings are unquoted; numbers and booleans are left as-is; null/absent → "—".
func formatSchemaDefault(raw json.RawMessage) string {
	if len(raw) == 0 {
		return defaultEmptyValue
	}

	// Try to unquote a JSON string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// For numbers, booleans, etc., use the raw JSON token directly.
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "null" {
		return defaultEmptyValue
	}
	return trimmed
}

// stringSet converts a slice of strings into a presence map.
func stringSet(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}

func newSchemaCmd(_ *log.Logger) *cobra.Command {
	var flags struct {
		response bool
	}

	cmd := &cobra.Command{
		Use:   "schema <air>",
		Short: "Show the request/response schema for a model",
		Example: `  # Show request parameters for a model
  runware model schema google:3@2

  # Show response parameters instead
  runware model schema google:3@2 --response

  # Output the full schema envelope as JSON
  runware model schema google:3@2 --format json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			air := args[0]

			spin := cmdutil.NewSpinner("Fetching model schema...")
			spin.Start()

			schema, err := api.FetchModelSchema(cmd.Context(), air)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			format := cmdutil.FormatFor(cmd)

			// For JSON/YAML emit the full envelope unchanged.
			if format != output.FormatTable {
				return output.Print(format, schema)
			}

			// For table, parse the selected schema and build indented rows.
			selected := schema.RequestSchema
			if flags.response {
				selected = schema.ResponseSchema
			}

			var node schemaNode
			if err := json.Unmarshal(selected, &node); err != nil {
				return fmt.Errorf("failed to parse schema: %w", err)
			}

			rows := buildRows(node, stringSet(node.Required), 0, 4)
			if err := output.Print(format, modelSchemaTable{rows: rows}); err != nil {
				return err
			}

			if schema.Documentation != "" {
				fmt.Fprintf(os.Stderr, "Docs: %s\n", schema.Documentation)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&flags.response, "response", "r", false, "Show response schema instead of request schema")

	return cmd
}
