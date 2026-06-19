package model

import (
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/api"
)

func TestModelPricingRows(t *testing.T) {
	r := modelPricing{p: &api.ModelPricing{
		PricingExamples: []api.PricingExample{
			{Configuration: "Input", Price: "$2"},
			{Configuration: "Output", Price: "$12"},
		},
	}}
	rows := r.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0][0] != "Input" || rows[0][1] != "$2" {
		t.Errorf("unexpected first row: %v", rows[0])
	}
}

func TestRequestToCommand(t *testing.T) {
	req := map[string]any{
		"taskType":       "imageInference",
		"taskUUID":       "abc-123",
		"positivePrompt": "a red fox",
		"width":          float64(1024),
		"CFGScale":       3.5,
		"acceleration":   "high",
	}
	req[fieldModel] = "runware:100@1"
	cmd := requestToCommand("runware:100@1", req)

	if !strings.HasPrefix(cmd, "runware run runware:100@1 ") {
		t.Fatalf("unexpected prefix: %s", cmd)
	}
	for _, skipped := range []string{"taskType=", "taskUUID=", "model="} {
		if strings.Contains(cmd, skipped) {
			t.Errorf("should skip %q: %s", skipped, cmd)
		}
	}
	for _, want := range []string{`positivePrompt="a red fox"`, "width=1024", "CFGScale=3.5", "acceleration=high"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("missing %q in: %s", want, cmd)
		}
	}
}

func TestRequestToCommand_Nested(t *testing.T) {
	req := map[string]any{
		"settings": map[string]any{"maxTokens": float64(900), "temperature": 0.3},
		"messages": []any{map[string]any{"role": "user", "content": "Describe the video"}},
	}
	cmd := requestToCommand("google:gemini@3.1-pro", req)

	for _, want := range []string{
		"settings.maxTokens=900",
		"settings.temperature=0.3",
		"messages.0.role=user",
		`messages.0.content="Describe the video"`,
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("missing %q in: %s", want, cmd)
		}
	}
}
