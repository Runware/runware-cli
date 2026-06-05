package run

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// test-local string constants to satisfy the goconst linter.
const (
	testFieldSpeech   = "speech"
	testFieldRole     = "role"
	testFieldContent  = "content"
	testFieldMessages = "messages"
	testValUser       = "user"
	testValHello      = "Hello"
	testMsg0Role      = "messages.0.role"
	testMsg0Content   = "messages.0.content"
	testMsg1Role      = "messages.1.role"
	testMsg1Content   = "messages.1.content"
	test3DURL1        = "https://cdn.runware.ai/3d/a.glb"
	test3DURL2        = "https://cdn.runware.ai/3d/b.glb"
	testUUIDKey       = "uuid"
)

// ---- extractTaskType tests ----

func TestExtractTaskType_Const(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			fieldTaskType: {
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
			fieldTaskType: {
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
			fieldTaskType: {
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
			fieldTaskType: {
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
			fieldModel: {Type: schemaTypeString},
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
		Required: []string{fieldModel, fieldPositivePrompt, fieldTaskType},
	}
	payload := map[string]any{
		fieldModel:          "runware:101@1",
		fieldPositivePrompt: "test",
	}
	if err := validateRequired(schema, payload); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateRequired_MissingField(t *testing.T) {
	schema := schemaNode{
		Required: []string{fieldModel, fieldPositivePrompt},
	}
	payload := map[string]any{
		fieldModel: "runware:101@1",
		// positivePrompt missing
	}
	err := validateRequired(schema, payload)
	if err == nil {
		t.Fatal("expected error for missing positivePrompt")
	}
	if !containsString(err.Error(), fieldPositivePrompt) {
		t.Errorf("error should mention missing field; got: %v", err)
	}
}

func TestValidateRequired_AutoFieldsSkipped(t *testing.T) {
	// taskType, taskUUID, deliveryMethod are injected automatically and should
	// never be reported as missing even if schema lists them as required.
	schema := schemaNode{
		Required: []string{fieldTaskType, fieldTaskUUID, fieldDeliveryMethod, fieldModel},
	}
	payload := map[string]any{
		fieldModel: "runware:101@1",
	}
	if err := validateRequired(schema, payload); err != nil {
		t.Errorf("auto fields must not trigger missing-required error; got: %v", err)
	}
}

func TestValidateRequired_MultipleMissing(t *testing.T) {
	schema := schemaNode{
		Required: []string{fieldPositivePrompt, fieldWidth, fieldHeight},
	}
	payload := map[string]any{}
	err := validateRequired(schema, payload)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, field := range []string{fieldPositivePrompt, fieldWidth, fieldHeight} {
		if !containsString(err.Error(), field) {
			t.Errorf("error should mention %q; got: %v", field, err)
		}
	}
}

// ---- parseKV tests ----

func TestParseKV_StringDefault(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			fieldPositivePrompt: {Type: schemaTypeString},
		},
	}
	path, v, err := parseKV("positivePrompt=A serene landscape", schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(path) != 1 || path[0] != fieldPositivePrompt {
		t.Errorf("path: want [positivePrompt], got %v", path)
	}
	if v != "A serene landscape" {
		t.Errorf("value: want 'A serene landscape', got %v", v)
	}
}

func TestParseKV_Integer(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			fieldWidth: {Type: schemaTypeInteger},
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
			testFieldMessages: {Type: schemaTypeArray},
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
			"steps": {Type: schemaTypeInteger},
		},
	}
	_, _, err := parseKV("steps=notanumber", schema)
	if err == nil {
		t.Error("expected error for invalid integer")
	}
}

// ---- parseKV dot-notation tests ----

func TestParseKV_DotNotation_ObjectPath(t *testing.T) {
	// speech.text=Hello → path ["speech","text"], value "Hello"
	schema := schemaNode{
		Properties: map[string]schemaNode{
			testFieldSpeech: {
				Type: schemaTypeObject,
				Properties: map[string]schemaNode{
					fieldText: {Type: schemaTypeString},
				},
			},
		},
	}
	path, v, err := parseKV("speech.text=Hello", schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(path) != 2 || path[0] != testFieldSpeech || path[1] != fieldText {
		t.Errorf("path: want [speech text], got %v", path)
	}
	if v != testValHello {
		t.Errorf("value: want Hello, got %v", v)
	}
}

func TestParseKV_DotNotation_ArrayExplicitIndex(t *testing.T) {
	// messages.0.role=user → path ["messages","0","role"], value "user"
	schema := schemaNode{
		Properties: map[string]schemaNode{
			testFieldMessages: {
				Type: schemaTypeArray,
				Items: &schemaNode{
					Type: schemaTypeObject,
					Properties: map[string]schemaNode{
						testFieldRole:    {Type: schemaTypeString},
						testFieldContent: {Type: schemaTypeString},
					},
				},
			},
		},
	}
	path, v, err := parseKV("messages.0.role=user", schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(path) != 3 || path[0] != testFieldMessages || path[1] != "0" || path[2] != testFieldRole {
		t.Errorf("path: want [messages 0 role], got %v", path)
	}
	if v != testValUser {
		t.Errorf("value: want user, got %v", v)
	}
}

func TestParseKV_DotNotation_ArrayAutoIndex(t *testing.T) {
	// messages.role=user (no index) → auto-insert "0" → path ["messages","0","role"]
	schema := schemaNode{
		Properties: map[string]schemaNode{
			testFieldMessages: {
				Type: schemaTypeArray,
				Items: &schemaNode{
					Type: schemaTypeObject,
					Properties: map[string]schemaNode{
						testFieldRole: {Type: schemaTypeString},
					},
				},
			},
		},
	}
	path, v, err := parseKV("messages.role=user", schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(path) != 3 || path[0] != testFieldMessages || path[1] != "0" || path[2] != testFieldRole {
		t.Errorf("path: want [messages 0 role], got %v", path)
	}
	if v != testValUser {
		t.Errorf("value: want user, got %v", v)
	}
}

func TestParseKV_DotNotation_NestedObjectCoercesType(t *testing.T) {
	// settings.maxTokens=512 should coerce to int64 via sub-schema
	schema := schemaNode{
		Properties: map[string]schemaNode{
			"settings": {
				Type: schemaTypeObject,
				Properties: map[string]schemaNode{
					"maxTokens": {Type: schemaTypeInteger},
				},
			},
		},
	}
	path, v, err := parseKV("settings.maxTokens=512", schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(path) != 2 || path[0] != "settings" || path[1] != "maxTokens" {
		t.Errorf("path: want [settings maxTokens], got %v", path)
	}
	if v != int64(512) {
		t.Errorf("value: want int64(512), got %v (%T)", v, v)
	}
}

func TestParseKV_EmptySegment(t *testing.T) {
	_, _, err := parseKV("speech..text=Hello", schemaNode{})
	if err == nil {
		t.Error("expected error for empty path segment")
	}
}

// ---- normalizeProvidedKey tests ----

func TestNormalizeProvidedKey_AutoIndexSugar(t *testing.T) {
	// "messages.role=user" should expand to "messages.0.role" via parseKV sugar.
	node := schemaNode{
		Properties: map[string]schemaNode{
			testFieldMessages: {
				Type: schemaTypeArray,
				Items: &schemaNode{
					Type: schemaTypeObject,
					Properties: map[string]schemaNode{
						testFieldRole:    {Type: schemaTypeString},
						testFieldContent: {Type: schemaTypeString},
					},
				},
			},
		},
	}
	got := normalizeProvidedKey("messages.role=user", node)
	if got != testMsg0Role {
		t.Errorf("want messages.0.role, got %q", got)
	}
}

func TestNormalizeProvidedKey_ExplicitIndexUnchanged(t *testing.T) {
	// An explicit index passes through unchanged.
	node := schemaNode{
		Properties: map[string]schemaNode{
			testFieldMessages: {
				Type: schemaTypeArray,
				Items: &schemaNode{
					Type: schemaTypeObject,
					Properties: map[string]schemaNode{
						testFieldRole: {Type: schemaTypeString},
					},
				},
			},
		},
	}
	got := normalizeProvidedKey("messages.0.role=user", node)
	if got != testMsg0Role {
		t.Errorf("want messages.0.role, got %q", got)
	}
}

func TestNormalizeProvidedKey_FlatField(t *testing.T) {
	// A flat field passes through as-is.
	node := schemaNode{
		Properties: map[string]schemaNode{
			fieldWidth: {Type: schemaTypeInteger},
		},
	}
	got := normalizeProvidedKey(fieldWidth+"=1024", node)
	if got != fieldWidth {
		t.Errorf("want %q, got %q", fieldWidth, got)
	}
}

func TestNormalizeProvidedKey_FallbackOnMalformed(t *testing.T) {
	// parseKV fails when there is no "=" — verbatim key portion is returned.
	got := normalizeProvidedKey("width", schemaNode{})
	if got != "width" {
		t.Errorf("want width, got %q", got)
	}
}

// ---- allLeafsProvided tests ----

func TestAllLeafsProvided_Complete(t *testing.T) {
	node := schemaNode{
		Properties: map[string]schemaNode{
			testFieldRole:    {Type: schemaTypeString},
			testFieldContent: {Type: schemaTypeString},
		},
	}
	provided := map[string]struct{}{
		testMsg0Role:    {},
		testMsg0Content: {},
	}
	if !allLeafsProvided("messages.0", node, provided) {
		t.Error("want true when all leaf fields are provided")
	}
}

func TestAllLeafsProvided_Missing(t *testing.T) {
	node := schemaNode{
		Properties: map[string]schemaNode{
			testFieldRole:    {Type: schemaTypeString},
			testFieldContent: {Type: schemaTypeString},
		},
	}
	// Only role provided — content is missing.
	provided := map[string]struct{}{
		testMsg0Role: {},
	}
	if allLeafsProvided("messages.0", node, provided) {
		t.Error("want false when a leaf field is absent")
	}
}

func TestAllLeafsProvided_Empty(t *testing.T) {
	node := schemaNode{
		Properties: map[string]schemaNode{
			testFieldRole: {Type: schemaTypeString},
		},
	}
	if allLeafsProvided("messages.0", node, map[string]struct{}{}) {
		t.Error("want false when provided is empty")
	}
}

// ---- collectCompletions array-index tests ----

func TestCollectCompletions_StaysAtIndex0WhenPartiallyFilled(t *testing.T) {
	// Bug scenario: messages.0.role is set but messages.0.content is not.
	// The completer must still suggest messages.0.content=, not messages.1.*.
	itemNode := schemaNode{
		Type: schemaTypeObject,
		Properties: map[string]schemaNode{
			testFieldRole:    {Type: schemaTypeString},
			testFieldContent: {Type: schemaTypeString},
		},
	}
	node := schemaNode{
		Properties: map[string]schemaNode{
			testFieldMessages: {
				Type:  schemaTypeArray,
				Items: &itemNode,
			},
		},
	}
	provided := map[string]struct{}{
		testMsg0Role: {},
	}
	nextIdx := func(prefix string) int {
		if prefix == testFieldMessages {
			return 1
		}
		return 0
	}

	completions := collectCompletions("", node, provided, "", nextIdx)
	keys := completionKeys(completions)

	for _, k := range keys {
		if strings.HasPrefix(k, "messages.1.") {
			t.Errorf("should not suggest index 1 while index 0 is incomplete; got %q", k)
		}
	}
	found := false
	for _, k := range keys {
		if k == testMsg0Content {
			found = true
		}
	}
	if !found {
		t.Errorf("expected messages.0.content= in completions; got %v", keys)
	}
}

func TestCollectCompletions_AdvancesToIndex1WhenIndex0Complete(t *testing.T) {
	// Once messages.0.role AND messages.0.content are both set, suggest index 1.
	itemNode := schemaNode{
		Type: schemaTypeObject,
		Properties: map[string]schemaNode{
			testFieldRole:    {Type: schemaTypeString},
			testFieldContent: {Type: schemaTypeString},
		},
	}
	node := schemaNode{
		Properties: map[string]schemaNode{
			testFieldMessages: {
				Type:  schemaTypeArray,
				Items: &itemNode,
			},
		},
	}
	provided := map[string]struct{}{
		testMsg0Role:    {},
		testMsg0Content: {},
	}
	nextIdx := func(prefix string) int {
		if prefix == testFieldMessages {
			return 1
		}
		return 0
	}

	completions := collectCompletions("", node, provided, "", nextIdx)
	keys := completionKeys(completions)

	for _, k := range keys {
		if strings.HasPrefix(k, "messages.0.") {
			t.Errorf("index 0 is complete; should not suggest %q", k)
		}
	}
	found := false
	for _, k := range keys {
		if k == testMsg1Role || k == testMsg1Content {
			found = true
		}
	}
	if !found {
		t.Errorf("expected messages.1.* completions; got %v", keys)
	}
}

// ---- deepSet tests ----

func TestDeepSet_FlatKey(t *testing.T) {
	payload := map[string]any{}
	deepSet(payload, []string{"width"}, int64(1024))
	if payload["width"] != int64(1024) {
		t.Errorf("want 1024, got %v", payload["width"])
	}
}

func TestDeepSet_NestedObject_SingleCall(t *testing.T) {
	payload := map[string]any{}
	deepSet(payload, []string{testFieldSpeech, fieldText}, testValHello)
	speech, ok := payload[testFieldSpeech].(map[string]any)
	if !ok {
		t.Fatalf("expected speech to be map[string]any, got %T", payload[testFieldSpeech])
	}
	if speech[fieldText] != testValHello {
		t.Errorf("want Hello, got %v", speech[fieldText])
	}
}

func TestDeepSet_NestedObject_TwoCallsMerge(t *testing.T) {
	// Two calls with the same parent must merge into the same map.
	payload := map[string]any{}
	deepSet(payload, []string{testFieldSpeech, fieldText}, testValHello)
	deepSet(payload, []string{testFieldSpeech, "voice"}, "English_expressive_narrator")
	speech, ok := payload[testFieldSpeech].(map[string]any)
	if !ok {
		t.Fatalf("expected speech to be map[string]any")
	}
	if speech[fieldText] != testValHello {
		t.Errorf("text: want Hello, got %v", speech[fieldText])
	}
	if speech["voice"] != "English_expressive_narrator" {
		t.Errorf("voice: want English_expressive_narrator, got %v", speech["voice"])
	}
}

func TestDeepSet_ArrayExplicitIndex(t *testing.T) {
	payload := map[string]any{}
	deepSet(payload, []string{testFieldMessages, "0", testFieldRole}, testValUser)
	deepSet(payload, []string{testFieldMessages, "0", testFieldContent}, testValHello)
	msgs, ok := payload[testFieldMessages].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected messages to be []any with 1 element")
	}
	m, ok := msgs[0].(map[string]any)
	if !ok {
		t.Fatalf("expected messages[0] to be map[string]any")
	}
	if m[testFieldRole] != testValUser || m[testFieldContent] != testValHello {
		t.Errorf("unexpected message contents: %v", m)
	}
}

func TestDeepSet_ArrayMultipleIndices(t *testing.T) {
	// Simulates a multi-turn conversation.
	payload := map[string]any{}
	deepSet(payload, []string{testFieldMessages, "0", testFieldRole}, testValUser)
	deepSet(payload, []string{testFieldMessages, "0", testFieldContent}, "What is Go?")
	deepSet(payload, []string{testFieldMessages, "1", testFieldRole}, "assistant")
	deepSet(payload, []string{testFieldMessages, "1", testFieldContent}, "A compiled language.")
	msgs, ok := payload[testFieldMessages].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %T %v", payload[testFieldMessages], payload[testFieldMessages])
	}
	m0 := msgs[0].(map[string]any)
	m1 := msgs[1].(map[string]any)
	if m0[testFieldRole] != testValUser || m1[testFieldRole] != "assistant" {
		t.Errorf("unexpected roles: %v, %v", m0[testFieldRole], m1[testFieldRole])
	}
}

func TestDeepSet_ScalarArray_GapSlots(t *testing.T) {
	// Setting index 2 directly must pad with nil, not map[string]any{},
	// so scalar arrays (e.g. inputs.images) don't get spurious object placeholders.
	payload := map[string]any{}
	deepSet(payload, []string{"inputs", "images", "2"}, "https://example.com/img.jpg")

	imgs, ok := payload["inputs"].(map[string]any)
	if !ok {
		t.Fatalf("expected inputs to be map[string]any, got %T", payload["inputs"])
	}
	sl, ok := imgs["images"].([]any)
	if !ok || len(sl) != 3 {
		t.Fatalf("expected images to be []any of length 3, got %T len=%d", imgs["images"], len(sl))
	}
	if sl[0] != nil {
		t.Errorf("slot 0: want nil, got %T %v", sl[0], sl[0])
	}
	if sl[1] != nil {
		t.Errorf("slot 1: want nil, got %T %v", sl[1], sl[1])
	}
	if sl[2] != "https://example.com/img.jpg" {
		t.Errorf("slot 2: want URL string, got %v", sl[2])
	}
}

func TestDeepSet_ObjectArray_NilSlotMerge(t *testing.T) {
	// nil gap slots must still be promotable to maps when later written to,
	// so object arrays (e.g. messages) continue to merge correctly.
	payload := map[string]any{}
	deepSet(payload, []string{testFieldMessages, "1", testFieldRole}, "assistant")
	// messages[0] was never set — should be nil, not {}.
	msgs, ok := payload[testFieldMessages].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("expected []any of length 2, got %T len=%d", payload[testFieldMessages], len(msgs))
	}
	if msgs[0] != nil {
		t.Errorf("gap slot 0: want nil, got %T %v", msgs[0], msgs[0])
	}
	m1, ok := msgs[1].(map[string]any)
	if !ok {
		t.Fatalf("slot 1: expected map[string]any, got %T", msgs[1])
	}
	if m1[testFieldRole] != "assistant" {
		t.Errorf("slot 1 role: want assistant, got %v", m1[testFieldRole])
	}
}

func TestDeepSet_DeepObjectPath(t *testing.T) {
	payload := map[string]any{}
	deepSet(payload, []string{"settings", "systemPrompt"}, "You are helpful.")
	settings, ok := payload["settings"].(map[string]any)
	if !ok {
		t.Fatalf("expected settings map")
	}
	if settings["systemPrompt"] != "You are helpful." {
		t.Errorf("want 'You are helpful.', got %v", settings["systemPrompt"])
	}
}

// ---- buildRunResult tests ----

func TestBuildRunResult_PriorityFields(t *testing.T) {
	parsed := map[string]any{
		fieldTaskType: "imageInference",
		fieldTaskUUID: "abc-123",
		fieldImageURL: "https://example.com/img.png",
		"seed":        float64(42),
	}
	res := buildRunResult(parsed)

	// taskUUID should appear before imageURL; taskType should be suppressed.
	if len(res.fields) == 0 {
		t.Fatal("expected fields")
	}
	firstKey := res.fields[0].key
	if firstKey != fieldTaskUUID {
		t.Errorf("first field should be taskUUID, got %q", firstKey)
	}
	for _, f := range res.fields {
		if f.key == fieldTaskType {
			t.Error("taskType should be suppressed in table output")
		}
	}

	var hasImageURL bool
	for _, f := range res.fields {
		if f.key == fieldImageURL {
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

// ---- buildRunResult — 3D inference ----

func TestBuildRunResult_3DOutputFiles(t *testing.T) {
	parsed := map[string]any{
		fieldTaskType: taskType3D,
		fieldTaskUUID: "uuid-3d",
		fieldOutputs: map[string]any{
			fieldOutputFiles: []any{
				map[string]any{fieldOutputURL: "https://cdn.runware.ai/3d/model.glb", testUUIDKey: "file-uuid-1"},
			},
		},
		"seed": float64(99),
	}
	res := buildRunResult(parsed)

	// The raw "outputs" blob must not appear — it should be expanded into a "file" row.
	for _, f := range res.fields {
		if f.key == fieldOutputs {
			t.Error("outputs should be suppressed; individual file rows expected instead")
		}
	}

	var fileRow *runResultField
	for i := range res.fields {
		if res.fields[i].key == "file" {
			fileRow = &res.fields[i]
		}
	}
	if fileRow == nil {
		t.Fatal("expected a 'file' row in table output")
	}
	if fileRow.value != "https://cdn.runware.ai/3d/model.glb" {
		t.Errorf("file row value: want URL, got %q", fileRow.value)
	}
}

func TestBuildRunResult_3DOutputFiles_Multi(t *testing.T) {
	parsed := map[string]any{
		fieldTaskType: taskType3D,
		fieldTaskUUID: "uuid-3d-multi",
		fieldOutputs: map[string]any{
			fieldOutputFiles: []any{
				map[string]any{fieldOutputURL: test3DURL1},
				map[string]any{fieldOutputURL: test3DURL2},
			},
		},
	}
	res := buildRunResult(parsed)

	keys := make(map[string]string)
	for _, f := range res.fields {
		keys[f.key] = f.value
	}

	if v, ok := keys["file.1"]; !ok || v != test3DURL1 {
		t.Errorf("file.1: want first URL, got %q (ok=%v)", v, ok)
	}
	if v, ok := keys["file.2"]; !ok || v != test3DURL2 {
		t.Errorf("file.2: want second URL, got %q (ok=%v)", v, ok)
	}
	if _, present := keys[fieldOutputs]; present {
		t.Error("raw outputs blob should be suppressed")
	}
}

// ---- extractOutputFileURLs ----

func TestExtractOutputFileURLs_Valid(t *testing.T) {
	parsed := map[string]any{
		fieldOutputs: map[string]any{
			fieldOutputFiles: []any{
				map[string]any{fieldOutputURL: test3DURL1, testUUIDKey: "u1"},
				map[string]any{fieldOutputURL: test3DURL2, testUUIDKey: "u2"},
			},
		},
	}
	urls := extractOutputFileURLs(parsed)
	if len(urls) != 2 {
		t.Fatalf("want 2 URLs, got %d", len(urls))
	}
	if urls[0] != test3DURL1 {
		t.Errorf("urls[0]: got %q", urls[0])
	}
	if urls[1] != test3DURL2 {
		t.Errorf("urls[1]: got %q", urls[1])
	}
}

func TestExtractOutputFileURLs_Absent(t *testing.T) {
	parsed := map[string]any{fieldImageURL: "https://example.com/img.png"}
	if urls := extractOutputFileURLs(parsed); len(urls) != 0 {
		t.Errorf("want nil/empty, got %v", urls)
	}
}

func TestExtractOutputFileURLs_MissingURL(t *testing.T) {
	// File entries without a "url" field should be skipped gracefully.
	parsed := map[string]any{
		fieldOutputs: map[string]any{
			fieldOutputFiles: []any{
				map[string]any{testUUIDKey: "u1"}, // no url
			},
		},
	}
	if urls := extractOutputFileURLs(parsed); len(urls) != 0 {
		t.Errorf("want empty, got %v", urls)
	}
}

// ---- buildDestPath tests ----

func TestBuildDestPath_SingleResult(t *testing.T) {
	p := buildDestPath("./outputs", "imageURL", "https://cdn.runware.ai/img/foo.png", 0, false)
	if p != "outputs/foo.png" {
		t.Errorf("want outputs/foo.png, got %q", p)
	}
}

func TestBuildDestPath_MultiResult(t *testing.T) {
	// Multi-result: URL filename is preserved as-is (CDN URLs are unique per result).
	p := buildDestPath("./outputs", "imageURL", "https://cdn.runware.ai/img/foo.webp", 1, true)
	if p != "outputs/foo.webp" {
		t.Errorf("want outputs/foo.webp, got %q", p)
	}
}

func TestBuildDestPath_URLWithQueryString(t *testing.T) {
	p := buildDestPath("./outputs", "videoURL", "https://cdn.runware.ai/v/bar.mp4?token=xyz", 0, false)
	if p != "outputs/bar.mp4" {
		t.Errorf("want outputs/bar.mp4, got %q", p)
	}
}

func TestBuildDestPath_NoExtension(t *testing.T) {
	// URL with no extension: filename is still preserved from URL path.
	p := buildDestPath("./outputs", "imageURL", "https://cdn.runware.ai/img/foo", 0, false)
	if p != "outputs/foo" {
		t.Errorf("want outputs/foo, got %q", p)
	}
}

func TestBuildDestPath_FallbackNoPath(t *testing.T) {
	// URL with no path segment falls back to generic stem.
	p := buildDestPath("./outputs", "imageURL", "https://cdn.runware.ai/", 0, false)
	if p != "outputs/image" {
		t.Errorf("want outputs/image, got %q", p)
	}
}

func TestBuildDestPath_FallbackMulti(t *testing.T) {
	// Fallback path still appends index for multi-result.
	p := buildDestPath("./outputs", "videoURL", "https://cdn.runware.ai/", 1, true)
	if p != "outputs/video-2" {
		t.Errorf("want outputs/video-2, got %q", p)
	}
}

// ---- helpers ----

// completionKeys extracts the bare key (no "=" suffix, no description) from a
// slice of cobra completions. cobra.CompletionWithDesc formats entries as
// "key=\tdescription", so we strip the tab+description and trailing "=".
func completionKeys(completions []cobra.Completion) []string {
	keys := make([]string, 0, len(completions))
	for _, c := range completions {
		s := c
		if idx := strings.IndexByte(s, '\t'); idx != -1 {
			s = s[:idx]
		}
		s = strings.TrimSuffix(s, "=")
		keys = append(keys, s)
	}
	return keys
}

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

// ---- validateAllOf tests ----

// klingaiSchema returns a schemaNode that mirrors the allOf constraints from
// the klingai:5@3 schema: width and height must together match one of three
// fixed 1080p combinations, and each requires the other.
func klingaiSchema() schemaNode {
	return schemaNode{
		AllOf: []schemaNode{
			{
				DependentRequired: map[string][]string{
					fieldWidth:  {fieldHeight},
					fieldHeight: {fieldWidth},
				},
			},
			{
				OneOf: []schemaNode{
					{
						Title: "1080p (16:9)",
						Properties: map[string]schemaNode{
							fieldWidth:  {Const: json.RawMessage(`1920`)},
							fieldHeight: {Const: json.RawMessage(`1080`)},
						},
					},
					{
						Title: "1080p (1:1)",
						Properties: map[string]schemaNode{
							fieldWidth:  {Const: json.RawMessage(`1080`)},
							fieldHeight: {Const: json.RawMessage(`1080`)},
						},
					},
					{
						Title: "1080p (9:16)",
						Properties: map[string]schemaNode{
							fieldWidth:  {Const: json.RawMessage(`1080`)},
							fieldHeight: {Const: json.RawMessage(`1920`)},
						},
					},
				},
			},
		},
	}
}

func TestValidateAllOf_MissingBothDimensions(t *testing.T) {
	// The reproducer: the example command omits width and height entirely.
	payload := map[string]any{
		fieldPositivePrompt: "Ocean waves at sunset",
		"duration":          float64(10),
	}
	err := validateAllOf(klingaiSchema(), payload)
	if err == nil {
		t.Fatal("expected error for missing width and height")
	}
	if !containsString(err.Error(), fieldHeight) || !containsString(err.Error(), fieldWidth) {
		t.Errorf("error should mention both missing fields; got: %v", err)
	}
	// Should list valid combinations.
	if !containsString(err.Error(), "1080p") {
		t.Errorf("error should show valid combinations; got: %v", err)
	}
}

func TestValidateAllOf_ValidCombination_16x9(t *testing.T) {
	payload := map[string]any{
		fieldPositivePrompt: "Ocean waves at sunset",
		fieldWidth:          int64(1920),
		fieldHeight:         int64(1080),
	}
	if err := validateAllOf(klingaiSchema(), payload); err != nil {
		t.Errorf("expected no error for valid 16:9 combination; got: %v", err)
	}
}

func TestValidateAllOf_ValidCombination_1x1(t *testing.T) {
	payload := map[string]any{
		fieldWidth:  int64(1080),
		fieldHeight: int64(1080),
	}
	if err := validateAllOf(klingaiSchema(), payload); err != nil {
		t.Errorf("expected no error for valid 1:1 combination; got: %v", err)
	}
}

func TestValidateAllOf_ValidCombination_9x16(t *testing.T) {
	payload := map[string]any{
		fieldWidth:  int64(1080),
		fieldHeight: int64(1920),
	}
	if err := validateAllOf(klingaiSchema(), payload); err != nil {
		t.Errorf("expected no error for valid 9:16 combination; got: %v", err)
	}
}

func TestValidateAllOf_InvalidCombination(t *testing.T) {
	payload := map[string]any{
		fieldWidth:  int64(800),
		fieldHeight: int64(600),
	}
	err := validateAllOf(klingaiSchema(), payload)
	if err == nil {
		t.Fatal("expected error for invalid 800×600 combination")
	}
	if !containsString(err.Error(), fieldWidth) {
		t.Errorf("error should mention the constrained fields; got: %v", err)
	}
	if !containsString(err.Error(), "1080p") {
		t.Errorf("error should list valid combinations; got: %v", err)
	}
}

func TestValidateAllOf_DependentRequired_WidthWithoutHeight(t *testing.T) {
	payload := map[string]any{
		fieldWidth: int64(1920),
		// height intentionally absent
	}
	err := validateAllOf(klingaiSchema(), payload)
	if err == nil {
		t.Fatal("expected error when width is provided without height")
	}
	if !containsString(err.Error(), fieldHeight) {
		t.Errorf("error should mention the missing dependent field; got: %v", err)
	}
}

func TestValidateAllOf_DependentRequired_HeightWithoutWidth(t *testing.T) {
	payload := map[string]any{
		fieldHeight: int64(1080),
		// width intentionally absent
	}
	err := validateAllOf(klingaiSchema(), payload)
	if err == nil {
		t.Fatal("expected error when height is provided without width")
	}
	if !containsString(err.Error(), fieldWidth) {
		t.Errorf("error should mention the missing dependent field; got: %v", err)
	}
}

func TestValidateAllOf_EmptyAllOf(t *testing.T) {
	schema := schemaNode{} // no AllOf
	payload := map[string]any{"positivePrompt": "test"}
	if err := validateAllOf(schema, payload); err != nil {
		t.Errorf("expected no error for schema with no allOf; got: %v", err)
	}
}

func TestValidateAllOf_NoBranchConsts(t *testing.T) {
	// oneOf branches without any const properties — should be a no-op.
	schema := schemaNode{
		AllOf: []schemaNode{
			{
				OneOf: []schemaNode{
					{Properties: map[string]schemaNode{"format": {Type: schemaTypeString}}},
					{Properties: map[string]schemaNode{"format": {Type: schemaTypeString}}},
				},
			},
		},
	}
	if err := validateAllOf(schema, map[string]any{}); err != nil {
		t.Errorf("expected no error when no branch has const properties; got: %v", err)
	}
}

// ---- extractDeliveryMethod tests ----

// TestExtractDeliveryMethod_OneOfSingleAsync mirrors the klingai:5@3 schema where
// deliveryMethod has a single oneOf branch with const "async".
func TestExtractDeliveryMethod_OneOfSingleAsync(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			fieldDeliveryMethod: {
				Type:    schemaTypeString,
				Default: json.RawMessage(`"async"`),
				OneOf: []schemaNode{
					{Const: json.RawMessage(`"async"`)},
				},
			},
		},
	}
	opts, def := extractDeliveryMethod(schema)
	if len(opts) != 1 || opts[0] != deliveryMethodAsync {
		t.Errorf("expected options [async], got %v", opts)
	}
	if def != deliveryMethodAsync {
		t.Errorf("expected default async, got %q", def)
	}
}

// TestExtractDeliveryMethod_EnumMultiple: model supporting both sync and async via enum.
func TestExtractDeliveryMethod_EnumMultiple(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			fieldDeliveryMethod: {
				Type:    schemaTypeString,
				Default: json.RawMessage(`"sync"`),
				Enum:    []json.RawMessage{json.RawMessage(`"sync"`), json.RawMessage(`"async"`)},
			},
		},
	}
	opts, def := extractDeliveryMethod(schema)
	if len(opts) != 2 || opts[0] != deliveryMethodSync || opts[1] != deliveryMethodAsync {
		t.Errorf("expected options [sync async], got %v", opts)
	}
	if def != deliveryMethodSync {
		t.Errorf("expected default sync, got %q", def)
	}
}

// TestExtractDeliveryMethod_Absent: schema with no deliveryMethod property.
func TestExtractDeliveryMethod_Absent(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			fieldPositivePrompt: {Type: schemaTypeString},
		},
	}
	opts, def := extractDeliveryMethod(schema)
	if opts != nil {
		t.Errorf("expected nil options, got %v", opts)
	}
	if def != "" {
		t.Errorf("expected empty default, got %q", def)
	}
}

// TestExtractDeliveryMethod_BareConst: deliveryMethod expressed as a bare const.
func TestExtractDeliveryMethod_BareConst(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			fieldDeliveryMethod: {
				Type:  schemaTypeString,
				Const: json.RawMessage(`"async"`),
			},
		},
	}
	opts, def := extractDeliveryMethod(schema)
	if len(opts) != 1 || opts[0] != deliveryMethodAsync {
		t.Errorf("expected options [async], got %v", opts)
	}
	// No default set.
	if def != "" {
		t.Errorf("expected empty default, got %q", def)
	}
}

// ---- resolveDeliveryMethod tests ----

func asyncOnlySchema() schemaNode {
	return schemaNode{
		Properties: map[string]schemaNode{
			fieldDeliveryMethod: {
				Type:    schemaTypeString,
				Default: json.RawMessage(`"async"`),
				OneOf:   []schemaNode{{Const: json.RawMessage(`"async"`)}},
			},
		},
	}
}

// TestResolveDeliveryMethod_PayloadWins: explicit KV arg beats everything.
func TestResolveDeliveryMethod_PayloadWins(t *testing.T) {
	payload := map[string]any{fieldDeliveryMethod: deliveryMethodSync}
	got := resolveDeliveryMethod(deliveryMethodAsync, payload, asyncOnlySchema())
	if got != deliveryMethodSync {
		t.Errorf("payload value should win; got %q", got)
	}
}

// TestResolveDeliveryMethod_FlagBeatsSchema: --delivery-method flag beats schema default.
func TestResolveDeliveryMethod_FlagBeatsSchema(t *testing.T) {
	got := resolveDeliveryMethod(deliveryMethodSync, map[string]any{}, asyncOnlySchema())
	if got != deliveryMethodSync {
		t.Errorf("flag value should win over schema; got %q", got)
	}
}

// TestResolveDeliveryMethod_SchemaDefault: no payload, no flag → use schema default.
func TestResolveDeliveryMethod_SchemaDefault(t *testing.T) {
	got := resolveDeliveryMethod("", map[string]any{}, asyncOnlySchema())
	if got != deliveryMethodAsync {
		t.Errorf("expected schema default async; got %q", got)
	}
}

// TestResolveDeliveryMethod_FallsBackToFirstOption: schema has options but no default.
func TestResolveDeliveryMethod_FallsBackToFirstOption(t *testing.T) {
	schema := schemaNode{
		Properties: map[string]schemaNode{
			fieldDeliveryMethod: {
				Type: schemaTypeString,
				Enum: []json.RawMessage{json.RawMessage(`"sync"`), json.RawMessage(`"async"`)},
				// no Default
			},
		},
	}
	got := resolveDeliveryMethod("", map[string]any{}, schema)
	if got != deliveryMethodSync {
		t.Errorf("expected first option sync; got %q", got)
	}
}

// TestResolveDeliveryMethod_EmptyWhenAbsent: no deliveryMethod in schema and no flag.
func TestResolveDeliveryMethod_EmptyWhenAbsent(t *testing.T) {
	got := resolveDeliveryMethod("", map[string]any{}, schemaNode{})
	if got != "" {
		t.Errorf("expected empty string; got %q", got)
	}
}
