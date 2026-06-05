package run

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Field name constants for API payload keys used throughout the run package.
// Centralising them here avoids repeated string literals across source and test files.
const (
	fieldTaskType       = "taskType"
	fieldTaskUUID       = "taskUUID"
	fieldDeliveryMethod = "deliveryMethod"
	fieldModel          = "model"
	fieldImageURL       = "imageURL"
	fieldVideoURL       = "videoURL"
	fieldAudioURL       = "audioURL"
	fieldPositivePrompt = "positivePrompt"
	fieldWidth          = "width"
	fieldHeight         = "height"
	fieldText           = "text"

	// schemaType* constants for JSON Schema "type" values.
	schemaTypeString  = "string"
	schemaTypeInteger = "integer"
	schemaTypeArray   = "array"
	schemaTypeObject  = "object"

	// taskType* constants for the known inference task type values.
	taskTypeImage = "imageInference"
	taskTypeVideo = "videoInference"
	taskTypeAudio = "audioInference"
	taskTypeText  = "textInference"
	taskType3D    = "3dInference"

	// fieldOutputs is the top-level result field used by 3D inference responses.
	// Its value is an object with a "files" array: {"files":[{"url":"...","uuid":"..."}]}.
	fieldOutputs     = "outputs"
	fieldOutputFiles = "files"
	fieldOutputURL   = "url"

	// deliveryMethodAsync and deliveryMethodSync are the recognised deliveryMethod values.
	deliveryMethodAsync = "async"
	deliveryMethodSync  = "sync"
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
	// Items holds the schema for elements of an array-typed property.
	// Used to coerce values when dot-notation paths descend into arrays.
	Items *schemaNode `json:"items"`
	// AllOf, OneOf, and DependentRequired support structural constraints used by
	// model schemas to express mutually-exclusive option sets (e.g. dimension
	// combinations) and co-dependent fields.
	AllOf             []schemaNode        `json:"allOf"`
	OneOf             []schemaNode        `json:"oneOf"`
	DependentRequired map[string][]string `json:"dependentRequired"`
}

// autoFields are parameters that the run command injects automatically and
// therefore must be excluded from both required-field checks and completions.
var autoFields = map[string]struct{}{
	fieldTaskType:       {},
	fieldTaskUUID:       {},
	fieldDeliveryMethod: {},
	fieldModel:          {},
}

// protectedFields are system-managed keys that the user must not supply as
// key=value CLI arguments. Each entry carries a short hint shown in the error
// message directing the user to the correct mechanism.
// deliveryMethod is intentionally absent: it may be overridden via either the
// --delivery-method flag or a key=value argument.
var protectedFields = map[string]string{
	fieldModel:    "pass the model as the first positional argument",
	fieldTaskType: "use the --task-type flag instead",
	fieldTaskUUID: "this field is system-generated and cannot be set manually",
}

// extractTaskType inspects requestSchema.properties.taskType for a constant
// value using const → enum[0] → default precedence.
// Returns the task type string and true on success, or ("", false) when the
// schema does not encode a constant taskType.
func extractTaskType(schema schemaNode) (string, bool) {
	prop, ok := schema.Properties[fieldTaskType]
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
		if _, ok := autoFields[req]; ok {
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
// to the Go type implied by the model schema. The key may use dot-notation to
// address nested fields (e.g. "speech.text=Hello" or "messages.0.role=user").
// A non-numeric segment inside an array-typed parent is treated as index 0
// (sugar for the common single-item case: "messages.role=user").
// Returns a path slice (length ≥ 1) and the coerced value.
func parseKV(arg string, schema schemaNode) (path []string, value any, err error) {
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

	// Normalise the path: for any non-numeric segment where the corresponding
	// schema node is an array, insert an implicit "0" index.
	segments = normalisePathSegments(segments, schema)

	// Find the leaf schema node to guide type coercion.
	leaf := schemaForPath(schema, segments)

	coerced, coerceErr := coerceValue(v, leaf)
	if coerceErr != nil {
		return nil, nil, fmt.Errorf("invalid value for %q: %w", k, coerceErr)
	}
	return segments, coerced, nil
}

// normalisePathSegments walks the schema alongside the raw path segments and
// inserts an implicit "0" index wherever a non-numeric segment is encountered
// inside an array-typed schema node. This implements the sugar that lets users
// write "messages.role=user" instead of "messages.0.role=user".
func normalisePathSegments(segments []string, schema schemaNode) []string {
	out := make([]string, 0, len(segments)+1)
	cur := schema
	for i, seg := range segments {
		prop, hasProp := cur.Properties[seg]
		if hasProp && prop.Type == schemaTypeArray && i < len(segments)-1 {
			// Next segment should be a numeric index. If it isn't, insert "0".
			next := segments[i+1]
			if !isNumeric(next) {
				out = append(out, seg, "0")
				// Advance cur to the items schema for subsequent segments.
				if prop.Items != nil {
					cur = *prop.Items
				} else {
					cur = schemaNode{}
				}
				continue
			}
		}
		out = append(out, seg)
		// Advance cur for the next iteration.
		if isNumeric(seg) {
			if cur.Items != nil {
				cur = *cur.Items
			} else {
				cur = schemaNode{}
			}
		} else if p, ok := cur.Properties[seg]; ok {
			cur = p
		} else {
			cur = schemaNode{}
		}
	}
	return out
}

// schemaForPath traverses a schema node along the given path and returns the
// leaf node. Numeric segments step into the array Items schema; string segments
// step into Properties. Returns an empty schemaNode if the path cannot be
// resolved (triggers best-effort coercion at the call site).
func schemaForPath(schema schemaNode, path []string) schemaNode {
	cur := schema
	for _, seg := range path {
		if isNumeric(seg) {
			if cur.Items != nil {
				cur = *cur.Items
			} else {
				return schemaNode{}
			}
		} else {
			prop, ok := cur.Properties[seg]
			if !ok {
				return schemaNode{}
			}
			cur = prop
		}
	}
	return cur
}

// deepSet writes value into payload at the location described by path, creating
// intermediate maps and slices as needed. Existing maps are merged rather than
// replaced, so two calls with paths ["speech","text"] and ["speech","voice"]
// both end up inside the same "speech" map.
func deepSet(payload map[string]any, path []string, value any) {
	if len(path) == 0 {
		return
	}
	if len(path) == 1 {
		payload[path[0]] = value
		return
	}

	head, rest := path[0], path[1:]

	if isNumeric(head) {
		// Numeric head means the caller should be operating on a slice, not a
		// map — this shouldn't happen at the top level.
		return
	}

	nextIsIndex := isNumeric(rest[0])

	if nextIsIndex {
		// head → slice; rest[0] is the index into that slice.
		idx := mustAtoi(rest[0])
		existing := payload[head]
		sl := toSlice(existing)
		for len(sl) <= idx {
			sl = append(sl, nil)
		}
		if len(rest) == 1 {
			sl[idx] = value
		} else {
			child := toMap(sl[idx])
			deepSet(child, rest[1:], value) //nolint:gosec // len(rest) > 1 guaranteed by the enclosing else branch
			sl[idx] = child
		}
		payload[head] = sl
	} else {
		// head → map; rest is a nested path.
		existing := payload[head]
		child := toMap(existing)
		deepSet(child, rest, value)
		payload[head] = child
	}
}

// normalizeProvidedKey returns the canonical dot-notation key for a "key=value"
// completion argument by running it through parseKV to apply auto-index sugar
// (e.g. "messages.role=user" → "messages.0.role"). Falls back to the verbatim
// key portion when parseKV fails (e.g. malformed arg or unknown schema).
func normalizeProvidedKey(arg string, node schemaNode) string {
	path, _, err := parseKV(arg, node)
	if err == nil {
		return strings.Join(path, ".")
	}
	k, _, _ := strings.Cut(arg, "=")
	return k
}

// allLeafsProvided reports whether every non-autoField leaf path of node,
// rooted at prefix, is present in provided. It mirrors the traversal logic
// of collectCompletions so the two stay in sync:
//   - object properties are recursed into
//   - array properties check index 0 of the item schema (best-effort for nested arrays)
//   - autoFields are skipped (they are never user-provided)
func allLeafsProvided(prefix string, node schemaNode, provided map[string]struct{}) bool {
	for name := range node.Properties {
		prop := node.Properties[name]
		if _, skip := autoFields[name]; skip {
			continue
		}
		full := prefix + "." + name

		switch prop.Type {
		case schemaTypeObject:
			if len(prop.Properties) > 0 {
				if !allLeafsProvided(full, prop, provided) {
					return false
				}
				continue
			}
		case schemaTypeArray:
			// For nested arrays, check index 0 only (best-effort).
			if prop.Items != nil && prop.Items.Type == schemaTypeObject && len(prop.Items.Properties) > 0 {
				if !allLeafsProvided(full+".0", *prop.Items, provided) {
					return false
				}
				continue
			}
			// Scalar array leaf — check index 0.
			if _, ok := provided[full+".0"]; !ok {
				return false
			}
			continue
		}
		// Leaf field.
		if _, ok := provided[full]; !ok {
			return false
		}
	}
	return true
}

// isNumeric reports whether s is a non-negative integer string.
func isNumeric(s string) bool {
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

// mustAtoi converts a numeric string to int. Callers must ensure isNumeric(s).
func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// toSlice coerces v to []any. If v is already []any it is returned as-is;
// otherwise an empty slice is returned.
func toSlice(v any) []any {
	if sl, ok := v.([]any); ok {
		return sl
	}
	return []any{}
}

// toMap coerces v to map[string]any. If v is already a map it is returned
// as-is; otherwise an empty map is returned.
func toMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// coerceValue converts the raw string v to the Go type indicated by the JSON
// Schema node. Supported types: integer, number, boolean, array, object, string.
// For unknown or missing types it falls back to bestEffortDecode.
func coerceValue(v string, prop schemaNode) (any, error) {
	switch prop.Type {
	case schemaTypeInteger:
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

	case schemaTypeArray, schemaTypeObject:
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

// validateAllOf checks structural constraints expressed in the schema's allOf
// array that are not captured by the flat required list. It handles two patterns
// common in Runware model schemas:
//
//  1. dependentRequired — if field A is present in the payload, every field
//     listed in dependentRequired[A] must also be present.
//
//  2. oneOf with const-property branches — the branches enumerate a fixed set
//     of valid field combinations (e.g. allowed width×height pairs). Fields
//     that appear with a const value in every branch are effectively required;
//     when present they must match exactly one branch.
func validateAllOf(schema schemaNode, payload map[string]any) error {
	for i := range schema.AllOf {
		if err := checkDependentRequired(schema.AllOf[i].DependentRequired, payload); err != nil {
			return err
		}
		if len(schema.AllOf[i].OneOf) > 0 {
			if err := checkOneOfCombinations(schema.AllOf[i].OneOf, payload); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkDependentRequired validates that whenever a "trigger" field is present in
// payload, all of its listed dependents are also present.
func checkDependentRequired(depReq map[string][]string, payload map[string]any) error {
	// Sort trigger keys so error messages are deterministic.
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
	consts map[string]any // field → const value
}

// checkOneOfCombinations validates a set of oneOf branches that each specify a
// fixed combination of field values (e.g. dimension pairs). It:
//   - discovers fields present with const values in every branch ("constrained fields")
//   - errors if any constrained field is absent from the payload
//   - errors if all are present but match no branch
func checkOneOfCombinations(branches []schemaNode, payload map[string]any) error {
	// Parse each branch into a combination. Skip branches with no const properties.
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

	// Find fields that carry a const in every combination.
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

	// Build a human-readable list of valid combinations for error messages.
	comboList := buildComboList(combos, constrained)

	// Check whether all constrained fields are present in the payload.
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

	// All constrained fields are present — verify they match one of the combos.
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

// matchesCombo returns true if every field in consts equals the corresponding
// payload value. Comparison is done via JSON round-trip to normalise numeric
// types (the payload may hold int64 while the schema const decoded as float64).
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

// jsonEqual compares two values for equality after normalising them through a
// JSON round-trip, so that int64(1920) and float64(1920) are considered equal.
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

// buildComboList formats the valid combinations as an indented bullet list,
// showing only the constrained fields (and the branch title when available).
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

// extractDeliveryMethod returns the allowed delivery method values and the
// schema-specified default for the model's deliveryMethod property.
// Options are collected from oneOf[i].Const first, then Enum, then a bare Const
// on the property node itself. Returns nil options and "" default when the
// property is absent (i.e. the model does not expose delivery method control).
func extractDeliveryMethod(schema schemaNode) (options []string, defaultVal string) {
	prop, ok := schema.Properties[fieldDeliveryMethod]
	if !ok {
		return nil, ""
	}

	// Collect option strings via oneOf → enum → const precedence.
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

	// Extract default value.
	if len(prop.Default) > 0 {
		var s string
		if err := json.Unmarshal(prop.Default, &s); err == nil {
			defaultVal = s
		}
	}

	return options, defaultVal
}

// resolveDeliveryMethod determines the delivery method to use for a request.
// Priority:
//  1. deliveryMethod already present in payload (user passed it as a key=value arg)
//  2. flagVal non-empty (user passed --delivery-method flag)
//  3. Schema default via extractDeliveryMethod; falls back to first option if no default
func resolveDeliveryMethod(flagVal string, payload map[string]any, schema schemaNode) string {
	if v, ok := payload[fieldDeliveryMethod]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	if flagVal != "" {
		return flagVal
	}
	options, defaultVal := extractDeliveryMethod(schema)
	if defaultVal != "" {
		return defaultVal
	}
	if len(options) > 0 {
		return options[0]
	}
	return ""
}
