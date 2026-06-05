package run

import (
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/schema"
	"github.com/spf13/cobra"
)

// test-local string constants to satisfy the goconst linter.
const (
	testFieldMessages = "messages"
	testFieldRole     = "role"
	testFieldContent  = "content"
	testMsg0Role      = "messages.0.role"
	testMsg0Content   = "messages.0.content"
	testMsg1Role      = "messages.1.role"
	testMsg1Content   = "messages.1.content"
	test3DURL1        = "https://cdn.runware.ai/3d/a.glb"
	test3DURL2        = "https://cdn.runware.ai/3d/b.glb"
	testUUIDKey       = "uuid"
)

// ---- collectCompletions array-index tests ----

func TestCollectCompletions_StaysAtIndex0WhenPartiallyFilled(t *testing.T) {
	// Bug scenario: messages.0.role is set but messages.0.content is not.
	// The completer must still suggest messages.0.content=, not messages.1.*.
	itemNode := schema.Node{
		Type: schema.TypeObject,
		Properties: map[string]schema.Node{
			testFieldRole:    {Type: schema.TypeString},
			testFieldContent: {Type: schema.TypeString},
		},
	}
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldMessages: {
				Type:  schema.TypeArray,
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
	itemNode := schema.Node{
		Type: schema.TypeObject,
		Properties: map[string]schema.Node{
			testFieldRole:    {Type: schema.TypeString},
			testFieldContent: {Type: schema.TypeString},
		},
	}
	node := schema.Node{
		Properties: map[string]schema.Node{
			testFieldMessages: {
				Type:  schema.TypeArray,
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
