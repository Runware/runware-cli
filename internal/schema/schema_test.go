package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/runware/runware-cli/internal/schema"
)

// test-local string constants to satisfy the goconst linter.
const (
	testFieldTaskType       = "taskType"
	testFieldModel          = "model"
	testFieldPositivePrompt = "positivePrompt"
	testFieldDeliveryMethod = "deliveryMethod"
	testFieldHeight         = "height"
	testFieldWidth          = "width"
	testFieldMessages       = "messages"
	testFieldSpeech         = "speech"
	testFieldText           = "text"
	testFieldRole           = "role"
	testFieldContent        = "content"
	testValHello            = "Hello"
	testValUser             = "user"
	testMsg0Role            = "messages.0.role"
	testDeliveryMethodAsync = "async"
	testDeliveryMethodSync  = "sync"
)

// ---- ExtractTaskType tests ----

func TestExtractTaskType_Const(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldTaskType: {Const: json.RawMessage(`"imageInference"`)},
		},
	}
	got, ok := schema.ExtractTaskType(node)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "imageInference" {
		t.Errorf("expected imageInference, got %q", got)
	}
}

func TestExtractTaskType_Enum(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldTaskType: {Enum: []json.RawMessage{json.RawMessage(`"videoInference"`)}},
		},
	}
	got, ok := schema.ExtractTaskType(node)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "videoInference" {
		t.Errorf("expected videoInference, got %q", got)
	}
}

func TestExtractTaskType_Default(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldTaskType: {Default: json.RawMessage(`"textInference"`)},
		},
	}
	got, ok := schema.ExtractTaskType(node)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "textInference" {
		t.Errorf("expected textInference, got %q", got)
	}
}

func TestExtractTaskType_ConstTakesPrecedenceOverEnum(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldTaskType: {
				Const: json.RawMessage(`"audioInference"`),
				Enum:  []json.RawMessage{json.RawMessage(`"imageInference"`)},
			},
		},
	}
	got, ok := schema.ExtractTaskType(node)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "audioInference" {
		t.Errorf("const should take precedence; got %q", got)
	}
}

func TestExtractTaskType_MissingProperty(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldModel: {Type: schema.TypeString},
		},
	}
	_, ok := schema.ExtractTaskType(node)
	if ok {
		t.Error("expected ok=false when taskType property is absent")
	}
}

func TestExtractTaskType_EmptyProperties(t *testing.T) {
	_, ok := schema.ExtractTaskType(schema.Node{})
	if ok {
		t.Error("expected ok=false for empty schema")
	}
}

// ---- ValidateRequired tests ----

func TestValidateRequired_AllPresent(t *testing.T) {
	node := schema.Node{
		Required: []string{testFieldModel, testFieldPositivePrompt, testFieldTaskType},
	}
	payload := map[string]any{
		testFieldModel:          "runware:101@1",
		testFieldPositivePrompt: "test",
	}
	if err := schema.ValidateRequired(node, payload); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateRequired_MissingField(t *testing.T) {
	node := schema.Node{
		Required: []string{testFieldModel, testFieldPositivePrompt},
	}
	payload := map[string]any{
		testFieldModel: "runware:101@1",
	}
	err := schema.ValidateRequired(node, payload)
	if err == nil {
		t.Fatal("expected error for missing positivePrompt")
	}
	if !containsString(err.Error(), testFieldPositivePrompt) {
		t.Errorf("error should mention missing field; got: %v", err)
	}
}

func TestValidateRequired_AutoFieldsSkipped(t *testing.T) {
	node := schema.Node{
		Required: []string{testFieldTaskType, "taskUUID", testFieldDeliveryMethod, testFieldModel},
	}
	payload := map[string]any{
		testFieldModel: "runware:101@1",
	}
	if err := schema.ValidateRequired(node, payload); err != nil {
		t.Errorf("auto fields must not trigger missing-required error; got: %v", err)
	}
}

func TestValidateRequired_MultipleMissing(t *testing.T) {
	node := schema.Node{
		Required: []string{testFieldPositivePrompt, testFieldWidth, testFieldHeight},
	}
	payload := map[string]any{}
	err := schema.ValidateRequired(node, payload)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, field := range []string{testFieldPositivePrompt, testFieldWidth, testFieldHeight} {
		if !containsString(err.Error(), field) {
			t.Errorf("error should mention %q; got: %v", field, err)
		}
	}
}

// ---- ParseKV tests ----

func TestParseKV_StringDefault(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldPositivePrompt: {Type: schema.TypeString},
		},
	}
	path, v, err := schema.ParseKV("positivePrompt=A serene landscape", node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(path) != 1 || path[0] != testFieldPositivePrompt {
		t.Errorf("path: want [positivePrompt], got %v", path)
	}
	if v != "A serene landscape" {
		t.Errorf("value: want 'A serene landscape', got %v", v)
	}
}

func TestParseKV_Integer(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldWidth: {Type: schema.TypeInteger},
		},
	}
	_, v, err := schema.ParseKV("width=1024", node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != int64(1024) {
		t.Errorf("want int64(1024), got %v (%T)", v, v)
	}
}

func TestParseKV_Number(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			"cfg": {Type: "number"},
		},
	}
	_, v, err := schema.ParseKV("cfg=3.5", node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != float64(3.5) {
		t.Errorf("want float64(3.5), got %v (%T)", v, v)
	}
}

func TestParseKV_Boolean(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			"includeCost": {Type: "boolean"},
		},
	}
	_, v, err := schema.ParseKV("includeCost=true", node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != true {
		t.Errorf("want true, got %v", v)
	}
}

func TestParseKV_Array(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldMessages: {Type: schema.TypeArray},
		},
	}
	_, v, err := schema.ParseKV(`messages=[{"role":"user","content":"Hi"}]`, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := v.([]any)
	if !ok || len(arr) != 1 {
		t.Errorf("want []any with 1 element, got %T %v", v, v)
	}
}

func TestParseKV_NoSchema_BestEffortNumber(t *testing.T) {
	_, v, err := schema.ParseKV("seed=42", schema.Node{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != float64(42) {
		t.Errorf("want float64(42) via best-effort, got %v (%T)", v, v)
	}
}

func TestParseKV_NoSchema_PlainString(t *testing.T) {
	_, v, err := schema.ParseKV("prompt=hello world", schema.Node{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "hello world" {
		t.Errorf("want 'hello world', got %v", v)
	}
}

func TestParseKV_MissingEquals(t *testing.T) {
	_, _, err := schema.ParseKV("noequals", schema.Node{})
	if err == nil {
		t.Error("expected error for missing '='")
	}
}

func TestParseKV_EmptyKey(t *testing.T) {
	_, _, err := schema.ParseKV("=value", schema.Node{})
	if err == nil {
		t.Error("expected error for empty key")
	}
}

func TestParseKV_InvalidInteger(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			"steps": {Type: schema.TypeInteger},
		},
	}
	_, _, err := schema.ParseKV("steps=notanumber", node)
	if err == nil {
		t.Error("expected error for invalid integer")
	}
}

func TestParseKV_DotNotation_ObjectPath(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldSpeech: {
				Type: schema.TypeObject,
				Properties: map[string]schema.Node{
					testFieldText: {Type: schema.TypeString},
				},
			},
		},
	}
	path, v, err := schema.ParseKV("speech.text=Hello", node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(path) != 2 || path[0] != testFieldSpeech || path[1] != testFieldText {
		t.Errorf("path: want [speech text], got %v", path)
	}
	if v != testValHello {
		t.Errorf("value: want Hello, got %v", v)
	}
}

func TestParseKV_DotNotation_ArrayExplicitIndex(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldMessages: {
				Type: schema.TypeArray,
				Items: &schema.Node{
					Type: schema.TypeObject,
					Properties: map[string]schema.Node{
						testFieldRole:    {Type: schema.TypeString},
						testFieldContent: {Type: schema.TypeString},
					},
				},
			},
		},
	}
	path, v, err := schema.ParseKV("messages.0.role=user", node)
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
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldMessages: {
				Type: schema.TypeArray,
				Items: &schema.Node{
					Type: schema.TypeObject,
					Properties: map[string]schema.Node{
						testFieldRole: {Type: schema.TypeString},
					},
				},
			},
		},
	}
	path, v, err := schema.ParseKV("messages.role=user", node)
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
	node := schema.Node{
		Properties: map[string]schema.Node{
			"settings": {
				Type: schema.TypeObject,
				Properties: map[string]schema.Node{
					"maxTokens": {Type: schema.TypeInteger},
				},
			},
		},
	}
	path, v, err := schema.ParseKV("settings.maxTokens=512", node)
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
	_, _, err := schema.ParseKV("speech..text=Hello", schema.Node{})
	if err == nil {
		t.Error("expected error for empty path segment")
	}
}

// ---- ManagedFields / IsAuto / IsProtected tests ----

// TestManagedFields_AllAutoExcluded verifies that every ManagedField is treated
// as auto (i.e. IsAuto returns true for every key in ManagedFields).
func TestManagedFields_AllAutoExcluded(t *testing.T) {
	for field := range schema.ManagedFields {
		if !schema.IsAuto(field) {
			t.Errorf("IsAuto(%q) = false; every ManagedField must be auto", field)
		}
	}
}

// TestManagedFields_ProtectedHintsNonEmpty ensures every protected field has a hint.
func TestManagedFields_ProtectedHintsNonEmpty(t *testing.T) {
	for field, mf := range schema.ManagedFields {
		if mf.Protected && mf.Hint == "" {
			t.Errorf("ManagedFields[%q].Protected=true but Hint is empty", field)
		}
	}
}

func TestIsAuto_ManagedFieldsReturnsTrue(t *testing.T) {
	for _, field := range []string{testFieldModel, testFieldTaskType, "taskUUID", testFieldDeliveryMethod} {
		if !schema.IsAuto(field) {
			t.Errorf("IsAuto(%q) = false; expected true", field)
		}
	}
}

func TestIsAuto_UnknownFieldReturnsFalse(t *testing.T) {
	if schema.IsAuto(testFieldPositivePrompt) {
		t.Errorf("IsAuto(%q) = true; expected false for non-managed field", testFieldPositivePrompt)
	}
}

func TestIsProtected_ModelRejected(t *testing.T) {
	hint, blocked := schema.IsProtected(testFieldModel)
	if !blocked {
		t.Fatal("expected model to be protected")
	}
	if hint == "" {
		t.Error("expected a non-empty hint for model")
	}
}

func TestIsProtected_TaskUUIDRejected(t *testing.T) {
	_, blocked := schema.IsProtected("taskUUID")
	if !blocked {
		t.Error("expected taskUUID to be protected")
	}
}

func TestIsProtected_TaskTypeRejected(t *testing.T) {
	_, blocked := schema.IsProtected(testFieldTaskType)
	if !blocked {
		t.Error("expected taskType to be protected")
	}
}

// TestIsProtected_DeliveryMethodAllowed confirms deliveryMethod is managed
// (auto) but not protected — it may be overridden via --delivery-method or
// a key=value argument.
func TestIsProtected_DeliveryMethodAllowed(t *testing.T) {
	if !schema.IsAuto(testFieldDeliveryMethod) {
		t.Error("deliveryMethod must be auto (managed by the run command)")
	}
	_, blocked := schema.IsProtected(testFieldDeliveryMethod)
	if blocked {
		t.Error("deliveryMethod must not be protected — it is user-overridable")
	}
}

// ---- NormalizeProvidedKey tests ----

func TestNormalizeProvidedKey_AutoIndexSugar(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldMessages: {
				Type: schema.TypeArray,
				Items: &schema.Node{
					Type: schema.TypeObject,
					Properties: map[string]schema.Node{
						testFieldRole:    {Type: schema.TypeString},
						testFieldContent: {Type: schema.TypeString},
					},
				},
			},
		},
	}
	got := schema.NormalizeProvidedKey("messages.role=user", node)
	if got != testMsg0Role {
		t.Errorf("want messages.0.role, got %q", got)
	}
}

func TestNormalizeProvidedKey_ExplicitIndexUnchanged(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldMessages: {
				Type: schema.TypeArray,
				Items: &schema.Node{
					Type: schema.TypeObject,
					Properties: map[string]schema.Node{
						testFieldRole: {Type: schema.TypeString},
					},
				},
			},
		},
	}
	got := schema.NormalizeProvidedKey("messages.0.role=user", node)
	if got != testMsg0Role {
		t.Errorf("want messages.0.role, got %q", got)
	}
}

func TestNormalizeProvidedKey_FlatField(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldWidth: {Type: schema.TypeInteger},
		},
	}
	got := schema.NormalizeProvidedKey("width=1024", node)
	if got != testFieldWidth {
		t.Errorf("want %q, got %q", testFieldWidth, got)
	}
}

func TestNormalizeProvidedKey_FallbackOnMalformed(t *testing.T) {
	got := schema.NormalizeProvidedKey("width", schema.Node{})
	if got != testFieldWidth {
		t.Errorf("want %q, got %q", testFieldWidth, got)
	}
}

// ---- AllLeafsProvided tests ----

func TestAllLeafsProvided_Complete(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldRole:    {Type: schema.TypeString},
			testFieldContent: {Type: schema.TypeString},
		},
	}
	provided := map[string]struct{}{
		"messages.0.role":    {},
		"messages.0.content": {},
	}
	if !schema.AllLeafsProvided("messages.0", node, provided) {
		t.Error("want true when all leaf fields are provided")
	}
}

func TestAllLeafsProvided_Missing(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldRole:    {Type: schema.TypeString},
			testFieldContent: {Type: schema.TypeString},
		},
	}
	provided := map[string]struct{}{
		testMsg0Role: {},
	}
	if schema.AllLeafsProvided("messages.0", node, provided) {
		t.Error("want false when a leaf field is absent")
	}
}

func TestAllLeafsProvided_Empty(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldRole: {Type: schema.TypeString},
		},
	}
	if schema.AllLeafsProvided("messages.0", node, map[string]struct{}{}) {
		t.Error("want false when provided is empty")
	}
}

// ---- DeepSet tests ----

func TestDeepSet_FlatKey(t *testing.T) {
	payload := map[string]any{}
	schema.DeepSet(payload, []string{testFieldWidth}, int64(1024))
	if payload[testFieldWidth] != int64(1024) {
		t.Errorf("want 1024, got %v", payload[testFieldWidth])
	}
}

func TestDeepSet_NestedObject_SingleCall(t *testing.T) {
	payload := map[string]any{}
	schema.DeepSet(payload, []string{testFieldSpeech, testFieldText}, testValHello)
	speech, ok := payload[testFieldSpeech].(map[string]any)
	if !ok {
		t.Fatalf("expected speech to be map[string]any, got %T", payload[testFieldSpeech])
	}
	if speech[testFieldText] != testValHello {
		t.Errorf("want Hello, got %v", speech[testFieldText])
	}
}

func TestDeepSet_NestedObject_TwoCallsMerge(t *testing.T) {
	payload := map[string]any{}
	schema.DeepSet(payload, []string{testFieldSpeech, testFieldText}, testValHello)
	schema.DeepSet(payload, []string{testFieldSpeech, "voice"}, "English_expressive_narrator")
	speech, ok := payload[testFieldSpeech].(map[string]any)
	if !ok {
		t.Fatalf("expected speech to be map[string]any")
	}
	if speech[testFieldText] != testValHello {
		t.Errorf("text: want Hello, got %v", speech[testFieldText])
	}
	if speech["voice"] != "English_expressive_narrator" {
		t.Errorf("voice: want English_expressive_narrator, got %v", speech["voice"])
	}
}

func TestDeepSet_ArrayExplicitIndex(t *testing.T) {
	payload := map[string]any{}
	schema.DeepSet(payload, []string{testFieldMessages, "0", testFieldRole}, testValUser)
	schema.DeepSet(payload, []string{testFieldMessages, "0", testFieldContent}, testValHello)
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
	payload := map[string]any{}
	schema.DeepSet(payload, []string{testFieldMessages, "0", testFieldRole}, testValUser)
	schema.DeepSet(payload, []string{testFieldMessages, "0", testFieldContent}, "What is Go?")
	schema.DeepSet(payload, []string{testFieldMessages, "1", testFieldRole}, "assistant")
	schema.DeepSet(payload, []string{testFieldMessages, "1", testFieldContent}, "A compiled language.")
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
	payload := map[string]any{}
	schema.DeepSet(payload, []string{"inputs", "images", "2"}, "https://example.com/img.jpg")

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
	payload := map[string]any{}
	schema.DeepSet(payload, []string{testFieldMessages, "1", testFieldRole}, "assistant")
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
	schema.DeepSet(payload, []string{"settings", "systemPrompt"}, "You are helpful.")
	settings, ok := payload["settings"].(map[string]any)
	if !ok {
		t.Fatalf("expected settings map")
	}
	if settings["systemPrompt"] != "You are helpful." {
		t.Errorf("want 'You are helpful.', got %v", settings["systemPrompt"])
	}
}

// ---- ValidateAllOf tests ----

func klingaiSchema() schema.Node {
	return schema.Node{
		AllOf: []schema.Node{
			{
				DependentRequired: map[string][]string{
					testFieldWidth:  {testFieldHeight},
					testFieldHeight: {testFieldWidth},
				},
			},
			{
				OneOf: []schema.Node{
					{
						Title: "1080p (16:9)",
						Properties: map[string]schema.Node{
							testFieldWidth:  {Const: json.RawMessage(`1920`)},
							testFieldHeight: {Const: json.RawMessage(`1080`)},
						},
					},
					{
						Title: "1080p (1:1)",
						Properties: map[string]schema.Node{
							testFieldWidth:  {Const: json.RawMessage(`1080`)},
							testFieldHeight: {Const: json.RawMessage(`1080`)},
						},
					},
					{
						Title: "1080p (9:16)",
						Properties: map[string]schema.Node{
							testFieldWidth:  {Const: json.RawMessage(`1080`)},
							testFieldHeight: {Const: json.RawMessage(`1920`)},
						},
					},
				},
			},
		},
	}
}

func TestValidateAllOf_MissingBothDimensions(t *testing.T) {
	payload := map[string]any{
		testFieldPositivePrompt: "Ocean waves at sunset",
		"duration":              float64(10),
	}
	err := schema.ValidateAllOf(klingaiSchema(), payload)
	if err == nil {
		t.Fatal("expected error for missing width and height")
	}
	if !containsString(err.Error(), testFieldHeight) || !containsString(err.Error(), testFieldWidth) {
		t.Errorf("error should mention both missing fields; got: %v", err)
	}
	if !containsString(err.Error(), "1080p") {
		t.Errorf("error should show valid combinations; got: %v", err)
	}
}

func TestValidateAllOf_ValidCombination_16x9(t *testing.T) {
	payload := map[string]any{
		testFieldPositivePrompt: "Ocean waves at sunset",
		testFieldWidth:          int64(1920),
		testFieldHeight:         int64(1080),
	}
	if err := schema.ValidateAllOf(klingaiSchema(), payload); err != nil {
		t.Errorf("expected no error for valid 16:9 combination; got: %v", err)
	}
}

func TestValidateAllOf_ValidCombination_1x1(t *testing.T) {
	payload := map[string]any{
		testFieldWidth:  int64(1080),
		testFieldHeight: int64(1080),
	}
	if err := schema.ValidateAllOf(klingaiSchema(), payload); err != nil {
		t.Errorf("expected no error for valid 1:1 combination; got: %v", err)
	}
}

func TestValidateAllOf_ValidCombination_9x16(t *testing.T) {
	payload := map[string]any{
		testFieldWidth:  int64(1080),
		testFieldHeight: int64(1920),
	}
	if err := schema.ValidateAllOf(klingaiSchema(), payload); err != nil {
		t.Errorf("expected no error for valid 9:16 combination; got: %v", err)
	}
}

func TestValidateAllOf_InvalidCombination(t *testing.T) {
	payload := map[string]any{
		testFieldWidth:  int64(800),
		testFieldHeight: int64(600),
	}
	err := schema.ValidateAllOf(klingaiSchema(), payload)
	if err == nil {
		t.Fatal("expected error for invalid 800×600 combination")
	}
	if !containsString(err.Error(), testFieldWidth) {
		t.Errorf("error should mention the constrained fields; got: %v", err)
	}
	if !containsString(err.Error(), "1080p") {
		t.Errorf("error should list valid combinations; got: %v", err)
	}
}

func TestValidateAllOf_DependentRequired_WidthWithoutHeight(t *testing.T) {
	payload := map[string]any{
		testFieldWidth: int64(1920),
		// height intentionally absent
	}
	err := schema.ValidateAllOf(klingaiSchema(), payload)
	if err == nil {
		t.Fatal("expected error when width is provided without height")
	}
	if !containsString(err.Error(), testFieldHeight) {
		t.Errorf("error should mention the missing dependent field; got: %v", err)
	}
}

func TestValidateAllOf_DependentRequired_HeightWithoutWidth(t *testing.T) {
	payload := map[string]any{
		testFieldHeight: int64(1080),
		// width intentionally absent
	}
	err := schema.ValidateAllOf(klingaiSchema(), payload)
	if err == nil {
		t.Fatal("expected error when height is provided without width")
	}
	if !containsString(err.Error(), testFieldWidth) {
		t.Errorf("error should mention the missing dependent field; got: %v", err)
	}
}

func TestValidateAllOf_EmptyAllOf(t *testing.T) {
	if err := schema.ValidateAllOf(schema.Node{}, map[string]any{testFieldPositivePrompt: "test"}); err != nil {
		t.Errorf("expected no error for schema with no allOf; got: %v", err)
	}
}

func TestValidateAllOf_NoBranchConsts(t *testing.T) {
	node := schema.Node{
		AllOf: []schema.Node{
			{
				OneOf: []schema.Node{
					{Properties: map[string]schema.Node{"format": {Type: schema.TypeString}}},
					{Properties: map[string]schema.Node{"format": {Type: schema.TypeString}}},
				},
			},
		},
	}
	if err := schema.ValidateAllOf(node, map[string]any{}); err != nil {
		t.Errorf("expected no error when no branch has const properties; got: %v", err)
	}
}

// ---- ExtractDeliveryMethod tests ----

func TestExtractDeliveryMethod_OneOfSingleAsync(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldDeliveryMethod: {
				Type:    schema.TypeString,
				Default: json.RawMessage(`"async"`),
				OneOf:   []schema.Node{{Const: json.RawMessage(`"async"`)}},
			},
		},
	}
	opts, def := schema.ExtractDeliveryMethod(node)
	if len(opts) != 1 || opts[0] != testDeliveryMethodAsync {
		t.Errorf("expected options [async], got %v", opts)
	}
	if def != testDeliveryMethodAsync {
		t.Errorf("expected default async, got %q", def)
	}
}

func TestExtractDeliveryMethod_EnumMultiple(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldDeliveryMethod: {
				Type:    schema.TypeString,
				Default: json.RawMessage(`"sync"`),
				Enum:    []json.RawMessage{json.RawMessage(`"sync"`), json.RawMessage(`"async"`)},
			},
		},
	}
	opts, def := schema.ExtractDeliveryMethod(node)
	if len(opts) != 2 || opts[0] != testDeliveryMethodSync || opts[1] != testDeliveryMethodAsync {
		t.Errorf("expected options [sync async], got %v", opts)
	}
	if def != testDeliveryMethodSync {
		t.Errorf("expected default sync, got %q", def)
	}
}

func TestExtractDeliveryMethod_Absent(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldPositivePrompt: {Type: schema.TypeString},
		},
	}
	opts, def := schema.ExtractDeliveryMethod(node)
	if opts != nil {
		t.Errorf("expected nil options, got %v", opts)
	}
	if def != "" {
		t.Errorf("expected empty default, got %q", def)
	}
}

func TestExtractDeliveryMethod_BareConst(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldDeliveryMethod: {
				Type:  schema.TypeString,
				Const: json.RawMessage(`"async"`),
			},
		},
	}
	opts, def := schema.ExtractDeliveryMethod(node)
	if len(opts) != 1 || opts[0] != testDeliveryMethodAsync {
		t.Errorf("expected options [async], got %v", opts)
	}
	if def != "" {
		t.Errorf("expected empty default, got %q", def)
	}
}

// ---- ResolveDeliveryMethod tests ----

func asyncOnlyNode() schema.Node {
	return schema.Node{
		Properties: map[string]schema.Node{
			testFieldDeliveryMethod: {
				Type:    schema.TypeString,
				Default: json.RawMessage(`"async"`),
				OneOf:   []schema.Node{{Const: json.RawMessage(`"async"`)}},
			},
		},
	}
}

func TestResolveDeliveryMethod_PayloadWins(t *testing.T) {
	payload := map[string]any{testFieldDeliveryMethod: testDeliveryMethodSync}
	got := schema.ResolveDeliveryMethod(testDeliveryMethodAsync, payload, asyncOnlyNode())
	if got != testDeliveryMethodSync {
		t.Errorf("payload value should win; got %q", got)
	}
}

func TestResolveDeliveryMethod_FlagBeatsSchema(t *testing.T) {
	got := schema.ResolveDeliveryMethod(testDeliveryMethodSync, map[string]any{}, asyncOnlyNode())
	if got != testDeliveryMethodSync {
		t.Errorf("flag value should win over schema; got %q", got)
	}
}

func TestResolveDeliveryMethod_SchemaDefault(t *testing.T) {
	got := schema.ResolveDeliveryMethod("", map[string]any{}, asyncOnlyNode())
	if got != testDeliveryMethodAsync {
		t.Errorf("expected schema default async; got %q", got)
	}
}

func TestResolveDeliveryMethod_FallsBackToFirstOption(t *testing.T) {
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldDeliveryMethod: {
				Type: schema.TypeString,
				Enum: []json.RawMessage{json.RawMessage(`"sync"`), json.RawMessage(`"async"`)},
			},
		},
	}
	got := schema.ResolveDeliveryMethod("", map[string]any{}, node)
	if got != testDeliveryMethodSync {
		t.Errorf("expected first option sync; got %q", got)
	}
}

func TestResolveDeliveryMethod_EmptyWhenAbsent(t *testing.T) {
	got := schema.ResolveDeliveryMethod("", map[string]any{}, schema.Node{})
	if got != "" {
		t.Errorf("expected empty string; got %q", got)
	}
}

// ---- helpers ----

func containsString(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
