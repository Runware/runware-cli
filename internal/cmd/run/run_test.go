package run

import (
	"encoding/json"
	"testing"
)

// ---- extractTaskType tests ----

func TestExtractTaskType_Const(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			"taskType": {
				Const: json.RawMessage(`"imageInference"`),
			},
		},
	}
	got, ok := extractTaskType(schema)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "imageInference" {
		t.Errorf("expected imageInference, got %q", got)
	}
}

func TestExtractTaskType_Enum(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			"taskType": {
				Enum: []json.RawMessage{json.RawMessage(`"videoInference"`)},
			},
		},
	}
	got, ok := extractTaskType(schema)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "videoInference" {
		t.Errorf("expected videoInference, got %q", got)
	}
}

func TestExtractTaskType_Default(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			"taskType": {
				Default: json.RawMessage(`"textInference"`),
			},
		},
	}
	got, ok := extractTaskType(schema)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "textInference" {
		t.Errorf("expected textInference, got %q", got)
	}
}

func TestExtractTaskType_ConstTakesPrecedenceOverEnum(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			"taskType": {
				Const: json.RawMessage(`"audioInference"`),
				Enum:  []json.RawMessage{json.RawMessage(`"imageInference"`)},
			},
		},
	}
	got, ok := extractTaskType(schema)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "audioInference" {
		t.Errorf("const should take precedence; got %q", got)
	}
}

func TestExtractTaskType_MissingProperty(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			"model": {Type: "string"},
		},
	}
	_, ok := extractTaskType(schema)
	if ok {
		t.Error("expected ok=false when taskType property is absent")
	}
}

func TestExtractTaskType_EmptyProperties(t *testing.T) {
	_, ok := extractTaskType(schemaNode{})
	if ok {
		t.Error("expected ok=false for empty schema")
	}
}

// ---- validateRequired tests ----

func TestValidateRequired_AllPresent(t *testing.T) {
	schema := schemaNode{
		Required: []string{"model", "positivePrompt", "taskType"},
	}
	payload := map[string]any{
		"model":          "runware:101@1",
		"positivePrompt": "test",
	}
	if err := validateRequired(schema, payload); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateRequired_MissingField(t *testing.T) {
	schema := schemaNode{
		Required: []string{"model", "positivePrompt"},
	}
	payload := map[string]any{
		"model": "runware:101@1",
		// positivePrompt missing
	}
	err := validateRequired(schema, payload)
	if err == nil {
		t.Fatal("expected error for missing positivePrompt")
	}
	if !containsString(err.Error(), "positivePrompt") {
		t.Errorf("error should mention missing field; got: %v", err)
	}
}

func TestValidateRequired_AutoFieldsSkipped(t *testing.T) {
	// taskType, taskUUID, deliveryMethod are injected automatically and should
	// never be reported as missing even if schema lists them as required.
	schema := schemaNode{
		Required: []string{"taskType", "taskUUID", "deliveryMethod", "model"},
	}
	payload := map[string]any{
		"model": "runware:101@1",
	}
	if err := validateRequired(schema, payload); err != nil {
		t.Errorf("auto fields must not trigger missing-required error; got: %v", err)
	}
}

func TestValidateRequired_MultipleMissing(t *testing.T) {
	schema := schemaNode{
		Required: []string{"positivePrompt", "width", "height"},
	}
	payload := map[string]any{}
	err := validateRequired(schema, payload)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, field := range []string{"positivePrompt", "width", "height"} {
		if !containsString(err.Error(), field) {
			t.Errorf("error should mention %q; got: %v", field, err)
		}
	}
}

// ---- parseKV tests ----

func TestParseKV_StringDefault(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			"positivePrompt": {Type: "string"},
		},
	}
	k, v, err := parseKV("positivePrompt=A serene landscape", schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k != "positivePrompt" {
		t.Errorf("key: want positivePrompt, got %q", k)
	}
	if v != "A serene landscape" {
		t.Errorf("value: want 'A serene landscape', got %v", v)
	}
}

func TestParseKV_Integer(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			"width": {Type: "integer"},
		},
	}
	_, v, err := parseKV("width=1024", schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != int64(1024) {
		t.Errorf("want int64(1024), got %v (%T)", v, v)
	}
}

func TestParseKV_Number(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			"cfg": {Type: "number"},
		},
	}
	_, v, err := parseKV("cfg=3.5", schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != float64(3.5) {
		t.Errorf("want float64(3.5), got %v (%T)", v, v)
	}
}

func TestParseKV_Boolean(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			"includeCost": {Type: "boolean"},
		},
	}
	_, v, err := parseKV("includeCost=true", schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != true {
		t.Errorf("want true, got %v", v)
	}
}

func TestParseKV_Array(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			"messages": {Type: "array"},
		},
	}
	_, v, err := parseKV(`messages=[{"role":"user","content":"Hi"}]`, schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := v.([]any)
	if !ok || len(arr) != 1 {
		t.Errorf("want []any with 1 element, got %T %v", v, v)
	}
}

func TestParseKV_NoSchema_BestEffortNumber(t *testing.T) {
	// No property definition — best-effort should decode the number from JSON.
	_, v, err := parseKV("seed=42", schemaNode{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// json.Unmarshal decodes bare integers as float64.
	if v != float64(42) {
		t.Errorf("want float64(42) via best-effort, got %v (%T)", v, v)
	}
}

func TestParseKV_NoSchema_PlainString(t *testing.T) {
	_, v, err := parseKV("prompt=hello world", schemaNode{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "hello world" {
		t.Errorf("want 'hello world', got %v", v)
	}
}

func TestParseKV_MissingEquals(t *testing.T) {
	_, _, err := parseKV("noequals", schemaNode{})
	if err == nil {
		t.Error("expected error for missing '='")
	}
}

func TestParseKV_EmptyKey(t *testing.T) {
	_, _, err := parseKV("=value", schemaNode{})
	if err == nil {
		t.Error("expected error for empty key")
	}
}

func TestParseKV_InvalidInteger(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			"steps": {Type: "integer"},
		},
	}
	_, _, err := parseKV("steps=notanumber", schema)
	if err == nil {
		t.Error("expected error for invalid integer")
	}
}

// ---- buildRunResult tests ----

func TestBuildRunResult_PriorityFields(t *testing.T) {
	parsed := map[string]any{
		"taskType": "imageInference",
		"taskUUID": "abc-123",
		"imageURL": "https://example.com/img.png",
		"seed":     float64(42),
	}
	res := buildRunResult(parsed)

	// taskUUID should appear before imageURL; taskType should be suppressed.
	if len(res.fields) == 0 {
		t.Fatal("expected fields")
	}
	firstKey := res.fields[0].key
	if firstKey != "taskUUID" {
		t.Errorf("first field should be taskUUID, got %q", firstKey)
	}
	for _, f := range res.fields {
		if f.key == "taskType" {
			t.Error("taskType should be suppressed in table output")
		}
	}

	var hasImageURL bool
	for _, f := range res.fields {
		if f.key == "imageURL" {
			hasImageURL = true
			if f.value != "https://example.com/img.png" {
				t.Errorf("imageURL value: want URL, got %q", f.value)
			}
		}
	}
	if !hasImageURL {
		t.Error("imageURL should be present in output")
	}
}

// ---- buildDestPath tests ----

func TestBuildDestPath_SingleResult(t *testing.T) {
	path := buildDestPath("./outputs", "imageURL", "https://cdn.runware.ai/img/foo.png", 0, false)
	if path != "outputs/image.png" {
		t.Errorf("want outputs/image.png, got %q", path)
	}
}

func TestBuildDestPath_MultiResult(t *testing.T) {
	path := buildDestPath("./outputs", "imageURL", "https://cdn.runware.ai/img/foo.webp", 1, true)
	if path != "outputs/image-2.webp" {
		t.Errorf("want outputs/image-2.webp, got %q", path)
	}
}

func TestBuildDestPath_URLWithQueryString(t *testing.T) {
	path := buildDestPath("./outputs", "videoURL", "https://cdn.runware.ai/v/bar.mp4?token=xyz", 0, false)
	if path != "outputs/video.mp4" {
		t.Errorf("want outputs/video.mp4, got %q", path)
	}
}

func TestBuildDestPath_NoExtension(t *testing.T) {
	path := buildDestPath("./outputs", "imageURL", "https://cdn.runware.ai/img/foo", 0, false)
	if path != "outputs/image" {
		t.Errorf("want outputs/image (no ext), got %q", path)
	}
}

// ---- helpers ----

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || findSub(s, sub))
}

func findSub(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
