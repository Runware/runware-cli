// Package schema provides JSON Schema parsing, validation, and value coercion
// for Runware model request schemas. It is transport-agnostic and contains no
// CLI or HTTP dependencies.
package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// JSON Schema "type" field values.
const (
	TypeString  = "string"
	TypeInteger = "integer"
	TypeArray   = "array"
	TypeObject  = "object"
)

// Recognised deliveryMethod values.
const (
	DeliveryMethodAsync = "async"
	DeliveryMethodSync  = "sync"
)

// fieldDeliveryMethod is the payload key for the delivery method field.
// Kept unexported because callers reference the run-package field constants.
const fieldDeliveryMethod = "deliveryMethod"

// Node is a minimal representation of a JSON Schema node used for task-type
// extraction, input validation, and value coercion. It covers all fields needed
// by the run command; it intentionally omits fields only used by model schema display.
type Node struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Type        string          `json:"type"`
	Default     json.RawMessage `json:"default"`
	Properties  map[string]Node `json:"properties"`
	Required    []string        `json:"required"`
	// Enum and Const are used to extract constant values such as taskType.
	Enum  []json.RawMessage `json:"enum"`
	Const json.RawMessage   `json:"const"`
	// Items holds the schema for elements of an array-typed property.
	// Used to coerce values when dot-notation paths descend into arrays.
	Items *Node `json:"items"`
	// AllOf, OneOf, and DependentRequired support structural constraints used by
	// model schemas to express mutually-exclusive option sets (e.g. dimension
	// combinations) and co-dependent fields.
	AllOf             []Node              `json:"allOf"`
	OneOf             []Node              `json:"oneOf"`
	DependentRequired map[string][]string `json:"dependentRequired"`
}

// ManagedField describes a system-managed payload key that is injected
// automatically by the run command.
type ManagedField struct {
	// Protected indicates that the user must not supply this field via a
	// key=value CLI argument. When true, Hint is shown in the error message
	// directing the user to the correct mechanism.
	Protected bool
	// Hint is a short human-readable explanation shown when a protected field
	// is rejected. Empty for non-protected managed fields.
	Hint string
}

// ManagedFields is the set of payload keys managed by the run command.
// Every entry is excluded from required-field checks and shell completions.
// Entries with Protected: true additionally reject user-supplied key=value args.
// deliveryMethod is not protected: it may be overridden via --delivery-method
// or a key=value argument.
var ManagedFields = map[string]ManagedField{
	"model": {
		Protected: true,
		Hint:      "pass the model as the first positional argument",
	},
	"taskType": {
		Protected: true,
		Hint:      "use the --task-type flag instead",
	},
	"taskUUID": {
		Protected: true,
		Hint:      "this field is system-generated and cannot be set manually",
	},
	"deliveryMethod": {
		Protected: false,
	},
}

// IsAuto reports whether field is managed automatically by the run command
// and should be excluded from required-field validation and shell completions.
func IsAuto(field string) bool {
	_, ok := ManagedFields[field]
	return ok
}

// IsProtected reports whether field is a system-managed key that must not be
// supplied as a user key=value argument. When blocked, hint describes the
// correct mechanism to use instead.
func IsProtected(field string) (hint string, ok bool) {
	f, ok := ManagedFields[field]
	if !ok || !f.Protected {
		return "", false
	}
	return f.Hint, true
}

// ExtractTaskType inspects node.Properties["taskType"] for a constant value
// using const → enum[0] → default precedence.
// Returns the task type string and true on success, or ("", false) when the
// schema does not encode a constant taskType.
func ExtractTaskType(node Node) (string, bool) {
	prop, ok := node.Properties["taskType"]
	if !ok {
		return "", false
	}

	if len(prop.Const) > 0 {
		var s string
		if err := json.Unmarshal(prop.Const, &s); err == nil && s != "" {
			return s, true
		}
	}

	if len(prop.Enum) > 0 {
		var s string
		if err := json.Unmarshal(prop.Enum[0], &s); err == nil && s != "" {
			return s, true
		}
	}

	if len(prop.Default) > 0 {
		var s string
		if err := json.Unmarshal(prop.Default, &s); err == nil && s != "" {
			return s, true
		}
	}

	return "", false
}

// ValidateRequired returns an error if any required schema field (excluding
// auto-injected fields) is absent from payload.
func ValidateRequired(node Node, payload map[string]any) error {
	var missing []string
	for _, req := range node.Required {
		if IsAuto(req) {
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

// ParseKV splits a "key=value" argument on the first '=' and coerces the value
// to the Go type implied by the model schema. The key may use dot-notation to
// address nested fields (e.g. "speech.text=Hello" or "messages.0.role=user").
// A non-numeric segment inside an array-typed parent is treated as index 0
// (sugar for the common single-item case: "messages.role=user").
// Returns a path slice (length ≥ 1) and the coerced value.
func ParseKV(arg string, node Node) (path []string, value any, err error) {
	k, v, found := strings.Cut(arg, "=")
	if !found {
		return nil, nil, fmt.Errorf("must be in key=value form (got %q)", arg)
	}
	if k == "" {
		return nil, nil, fmt.Errorf("key must not be empty (got %q)", arg)
	}

	segments := strings.Split(k, ".")
	for _, seg := range segments {
		if seg == "" {
			return nil, nil, fmt.Errorf("key contains empty segment (got %q)", arg)
		}
	}

	segments = normalisePathSegments(segments, node)

	leaf := schemaForPath(node, segments)

	coerced, coerceErr := coerceValue(v, leaf)
	if coerceErr != nil {
		return nil, nil, fmt.Errorf("invalid value for %q: %w", k, coerceErr)
	}
	return segments, coerced, nil
}

// normalisePathSegments walks the schema alongside the raw path segments and
// inserts an implicit "0" index wherever a non-numeric segment is encountered
// inside an array-typed schema node.
func normalisePathSegments(segments []string, node Node) []string {
	out := make([]string, 0, len(segments)+1)
	cur := node
	for i, seg := range segments {
		prop, hasProp := cur.Properties[seg]
		if hasProp && prop.Type == TypeArray && i < len(segments)-1 {
			next := segments[i+1]
			if !IsNumeric(next) {
				out = append(out, seg, "0")
				if prop.Items != nil {
					cur = *prop.Items
				} else {
					cur = Node{}
				}
				continue
			}
		}
		out = append(out, seg)
		if IsNumeric(seg) {
			if cur.Items != nil {
				cur = *cur.Items
			} else {
				cur = Node{}
			}
		} else if p, ok := cur.Properties[seg]; ok {
			cur = p
		} else {
			cur = Node{}
		}
	}
	return out
}

// schemaForPath traverses a Node along the given path and returns the leaf
// node. Numeric segments step into Items; string segments step into Properties.
// Returns an empty Node if the path cannot be resolved.
func schemaForPath(node Node, path []string) Node {
	cur := node
	for _, seg := range path {
		if IsNumeric(seg) {
			if cur.Items != nil {
				cur = *cur.Items
			} else {
				return Node{}
			}
		} else {
			prop, ok := cur.Properties[seg]
			if !ok {
				return Node{}
			}
			cur = prop
		}
	}
	return cur
}

// DeepSet writes value into payload at the location described by path, creating
// intermediate maps and slices as needed. Existing maps are merged rather than
// replaced.
func DeepSet(payload map[string]any, path []string, value any) {
	if len(path) == 0 {
		return
	}
	if len(path) == 1 {
		payload[path[0]] = value
		return
	}

	head, rest := path[0], path[1:]

	if IsNumeric(head) {
		return
	}

	nextIsIndex := IsNumeric(rest[0])

	if nextIsIndex {
		idx := MustAtoi(rest[0])
		existing := payload[head]
		sl := toSlice(existing)
		for len(sl) <= idx {
			sl = append(sl, nil)
		}
		if len(rest) == 1 {
			sl[idx] = value
		} else {
			child := toMap(sl[idx])
			DeepSet(child, rest[1:], value) //nolint:gosec
			sl[idx] = child
		}
		payload[head] = sl
	} else {
		existing := payload[head]
		child := toMap(existing)
		DeepSet(child, rest, value)
		payload[head] = child
	}
}

// NormalizeProvidedKey returns the canonical dot-notation key for a "key=value"
// completion argument by running it through ParseKV to apply auto-index sugar.
// Falls back to the verbatim key portion when ParseKV fails.
func NormalizeProvidedKey(arg string, node Node) string {
	path, _, err := ParseKV(arg, node)
	if err == nil {
		return strings.Join(path, ".")
	}
	k, _, _ := strings.Cut(arg, "=")
	return k
}

// AllLeafsProvided reports whether every non-AutoField leaf path of node,
// rooted at prefix, is present in provided.
func AllLeafsProvided(prefix string, node Node, provided map[string]struct{}) bool {
	for name := range node.Properties {
		prop := node.Properties[name]
		if IsAuto(name) {
			continue
		}
		full := prefix + "." + name

		switch prop.Type {
		case TypeObject:
			if len(prop.Properties) > 0 {
				if !AllLeafsProvided(full, prop, provided) {
					return false
				}
				continue
			}
		case TypeArray:
			if prop.Items != nil && prop.Items.Type == TypeObject && len(prop.Items.Properties) > 0 {
				if !AllLeafsProvided(full+".0", *prop.Items, provided) {
					return false
				}
				continue
			}
			if _, ok := provided[full+".0"]; !ok {
				return false
			}
			continue
		}
		if _, ok := provided[full]; !ok {
			return false
		}
	}
	return true
}

// IsNumeric reports whether s is a non-negative integer string.
func IsNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// MustAtoi converts a numeric string to int. Callers must ensure IsNumeric(s).
func MustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func toSlice(v any) []any {
	if sl, ok := v.([]any); ok {
		return sl
	}
	return []any{}
}

func toMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// coerceValue converts the raw string v to the Go type indicated by the JSON
// Schema node prop.
func coerceValue(v string, prop Node) (any, error) {
	switch prop.Type {
	case TypeInteger:
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

	case TypeArray, TypeObject:
		var out any
		if err := json.Unmarshal([]byte(v), &out); err != nil {
			return nil, fmt.Errorf("expected JSON %s, got %q: %w", prop.Type, v, err)
		}
		return out, nil

	default:
		return bestEffortDecode(v), nil
	}
}

// bestEffortDecode attempts JSON decoding of v; if that fails it returns v unchanged.
func bestEffortDecode(v string) any {
	var out any
	if err := json.Unmarshal([]byte(v), &out); err == nil {
		return out
	}
	return v
}

// ValidateAllOf checks structural constraints expressed in the schema's allOf
// array: dependentRequired fields and oneOf const-property combinations.
func ValidateAllOf(node Node, payload map[string]any) error {
	for i := range node.AllOf {
		if err := checkDependentRequired(node.AllOf[i].DependentRequired, payload); err != nil {
			return err
		}
		if len(node.AllOf[i].OneOf) > 0 {
			if err := checkOneOfCombinations(node.AllOf[i].OneOf, payload); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkDependentRequired(depReq map[string][]string, payload map[string]any) error {
	triggers := make([]string, 0, len(depReq))
	for k := range depReq {
		triggers = append(triggers, k)
	}
	sort.Strings(triggers)

	for _, trigger := range triggers {
		if _, ok := payload[trigger]; !ok {
			continue
		}
		deps := depReq[trigger]
		var missingDeps []string
		for _, dep := range deps {
			if _, ok := payload[dep]; !ok {
				missingDeps = append(missingDeps, dep)
			}
		}
		if len(missingDeps) > 0 {
			sort.Strings(missingDeps)
			return fmt.Errorf(
				"field %q requires %s to also be set",
				trigger, strings.Join(missingDeps, ", "),
			)
		}
	}
	return nil
}

// combination represents one branch of a oneOf constraint in which every
// property carries a const value.
type combination struct {
	title  string
	consts map[string]any
}

func checkOneOfCombinations(branches []Node, payload map[string]any) error {
	combos := make([]combination, 0, len(branches))
	for i := range branches {
		consts := make(map[string]any, len(branches[i].Properties))
		for field := range branches[i].Properties {
			prop := branches[i].Properties[field]
			if len(prop.Const) == 0 {
				continue
			}
			var val any
			if err := json.Unmarshal(prop.Const, &val); err == nil {
				consts[field] = val
			}
		}
		if len(consts) > 0 {
			combos = append(combos, combination{title: branches[i].Title, consts: consts})
		}
	}
	if len(combos) == 0 {
		return nil
	}

	fieldInAll := func(field string) bool {
		for _, c := range combos {
			if _, ok := c.consts[field]; !ok {
				return false
			}
		}
		return true
	}
	var constrained []string
	for field := range combos[0].consts {
		if fieldInAll(field) {
			constrained = append(constrained, field)
		}
	}
	sort.Strings(constrained)

	if len(constrained) == 0 {
		return nil
	}

	comboList := buildComboList(combos, constrained)

	var missing []string
	for _, field := range constrained {
		if _, ok := payload[field]; !ok {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"missing required parameter(s): %s\n  must be one of:\n%s\nRun 'runware model schema <model>' to see all parameters",
			strings.Join(missing, ", "), comboList,
		)
	}

	for _, combo := range combos {
		if matchesCombo(payload, combo.consts) {
			return nil
		}
	}
	return fmt.Errorf(
		"invalid combination for %s\n  must be one of:\n%s",
		strings.Join(constrained, "/"), comboList,
	)
}

func matchesCombo(payload map[string]any, consts map[string]any) bool {
	for field, want := range consts {
		got, ok := payload[field]
		if !ok {
			return false
		}
		if !jsonEqual(got, want) {
			return false
		}
	}
	return true
}

func jsonEqual(a, b any) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(aj, bj)
}

func buildComboList(combos []combination, constrained []string) string {
	var sb strings.Builder
	for _, combo := range combos {
		sb.WriteString("    ")
		if combo.title != "" {
			sb.WriteString(combo.title)
			sb.WriteString(": ")
		}
		for i, field := range constrained {
			if i > 0 {
				sb.WriteString(" ")
			}
			fmt.Fprintf(&sb, "%s=%v", field, combo.consts[field])
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ExtractDeliveryMethod returns the allowed delivery method values and the
// schema-specified default for the model's deliveryMethod property.
func ExtractDeliveryMethod(node Node) (options []string, defaultVal string) {
	prop, ok := node.Properties[fieldDeliveryMethod]
	if !ok {
		return nil, ""
	}

	for i := range prop.OneOf {
		if len(prop.OneOf[i].Const) > 0 {
			var s string
			if err := json.Unmarshal(prop.OneOf[i].Const, &s); err == nil && s != "" {
				options = append(options, s)
			}
		}
	}
	if len(options) == 0 {
		for _, raw := range prop.Enum {
			var s string
			if err := json.Unmarshal(raw, &s); err == nil && s != "" {
				options = append(options, s)
			}
		}
	}
	if len(options) == 0 && len(prop.Const) > 0 {
		var s string
		if err := json.Unmarshal(prop.Const, &s); err == nil && s != "" {
			options = append(options, s)
		}
	}

	if len(prop.Default) > 0 {
		var s string
		if err := json.Unmarshal(prop.Default, &s); err == nil {
			defaultVal = s
		}
	}

	return options, defaultVal
}

// ResolveDeliveryMethod determines the delivery method to use for a request.
// Priority: payload value > flagVal > schema default > first schema option.
func ResolveDeliveryMethod(flagVal string, payload map[string]any, node Node) string {
	if v, ok := payload[fieldDeliveryMethod]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	if flagVal != "" {
		return flagVal
	}
	options, defaultVal := ExtractDeliveryMethod(node)
	if defaultVal != "" {
		return defaultVal
	}
	if len(options) > 0 {
		return options[0]
	}
	return ""
}
