package run

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// schemaNode is a minimal representation of a JSON Schema node used for task-type
// extraction, input validation, and value coercion. It covers all fields needed
// by the run command; it intentionally omits fields only used by model schema display.
type schemaNode struct {
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Type        string                `json:"type"`
	Default     json.RawMessage       `json:"default"`
	Properties  map[string]schemaNode `json:"properties"`
	Required    []string              `json:"required"`
	// Enum and Const are used to extract constant values such as taskType.
	Enum  []json.RawMessage `json:"enum"`
	Const json.RawMessage   `json:"const"`
}

// autoFields are parameters that the run command injects automatically and
// therefore must be excluded from both required-field checks and completions.
var autoFields = map[string]bool{
	"taskType":       true,
	"taskUUID":       true,
	"deliveryMethod": true,
	"model":          true,
}

// extractTaskType inspects requestSchema.properties.taskType for a constant
// value using const → enum[0] → default precedence.
// Returns the task type string and true on success, or ("", false) when the
// schema does not encode a constant taskType.
func extractTaskType(schema schemaNode) (string, bool) {
	prop, ok := schema.Properties["taskType"]
	if !ok {
		return "", false
	}

	// const takes highest precedence — it is a single fixed value.
	if len(prop.Const) > 0 {
		var s string
		if err := json.Unmarshal(prop.Const, &s); err == nil && s != "" {
			return s, true
		}
	}

	// enum: use the first (and usually only) value.
	if len(prop.Enum) > 0 {
		var s string
		if err := json.Unmarshal(prop.Enum[0], &s); err == nil && s != "" {
			return s, true
		}
	}

	// default: some schemas express the task type this way.
	if len(prop.Default) > 0 {
		var s string
		if err := json.Unmarshal(prop.Default, &s); err == nil && s != "" {
			return s, true
		}
	}

	return "", false
}

// validateRequired returns an error if any required schema field (excluding
// auto-injected fields) is absent from payload.
func validateRequired(schema schemaNode, payload map[string]any) error {
	var missing []string
	for _, req := range schema.Required {
		if autoFields[req] {
			continue
		}
		if _, ok := payload[req]; !ok {
			missing = append(missing, req)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf(
			"missing required parameter(s): %s\nRun 'runware model schema <model>' to see all parameters",
			strings.Join(missing, ", "),
		)
	}
	return nil
}

// parseKV splits a "key=value" argument on the first '=' and coerces the value
// to the Go type implied by the model schema. When no schema is available for
// the key, best-effort JSON decoding is used, falling back to a plain string.
func parseKV(arg string, schema schemaNode) (key string, value any, err error) {
	k, v, found := strings.Cut(arg, "=")
	if !found {
		return "", nil, fmt.Errorf("must be in key=value form (got %q)", arg)
	}
	if k == "" {
		return "", nil, fmt.Errorf("key must not be empty (got %q)", arg)
	}

	prop, hasProp := schema.Properties[k]
	if hasProp {
		coerced, coerceErr := coerceValue(v, prop)
		if coerceErr != nil {
			return "", nil, fmt.Errorf("invalid value for %q: %w", k, coerceErr)
		}
		return k, coerced, nil
	}

	// No schema available for this key — best-effort decode.
	coerced := bestEffortDecode(v)
	return k, coerced, nil
}

// coerceValue converts the raw string v to the Go type indicated by the JSON
// Schema node. Supported types: integer, number, boolean, array, object, string.
// For unknown or missing types it falls back to bestEffortDecode.
func coerceValue(v string, prop schemaNode) (any, error) {
	switch prop.Type {
	case "integer":
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected integer, got %q", v)
		}
		return n, nil

	case "number":
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("expected number, got %q", v)
		}
		return n, nil

	case "boolean":
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("expected boolean (true/false), got %q", v)
		}
		return b, nil

	case "array", "object":
		var out any
		if err := json.Unmarshal([]byte(v), &out); err != nil {
			return nil, fmt.Errorf("expected JSON %s, got %q: %w", prop.Type, v, err)
		}
		return out, nil

	default:
		// "string" or unspecified — try JSON decode first so quoted literals,
		// embedded JSON numbers, etc. are handled, then fall back to plain string.
		return bestEffortDecode(v), nil
	}
}

// bestEffortDecode attempts JSON decoding of v; if that fails it returns v unchanged.
// This handles inputs like true, 42, [1,2,3] without a schema hint.
func bestEffortDecode(v string) any {
	var out any
	if err := json.Unmarshal([]byte(v), &out); err == nil {
		return out
	}
	return v
}
