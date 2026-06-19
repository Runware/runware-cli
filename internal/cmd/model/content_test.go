package model

import (
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

func TestModelExamplesRows_PromptColumn(t *testing.T) {
	r := modelExamples{examples: []api.ModelExample{
		{ID: "ex-1", Title: "", Capability: "io:text-to-image",
			Request: map[string]any{"positivePrompt": "A red fox in snow"}},
	}}
	rows := r.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// An empty title renders as an em-dash, matching `model show`'s orDash helper.
	if rows[0][0] != "—" {
		t.Errorf("empty title should render as an em-dash, got: %v", rows[0][0])
	}
	if rows[0][2] != "A red fox in snow" {
		t.Errorf("prompt column should show positivePrompt, got: %v", rows[0][2])
	}
}

func TestPromptPreview(t *testing.T) {
	// positivePrompt wins, and whitespace is collapsed.
	if got := promptPreview(map[string]any{"positivePrompt": "  hello   world\n"}, 60); got != "hello world" {
		t.Errorf("positivePrompt: got %q", got)
	}
	// Falls back to the last user message's content.
	msgReq := map[string]any{"messages": []any{
		map[string]any{"role": "user", "content": "Describe the video"},
	}}
	if got := promptPreview(msgReq, 60); got != "Describe the video" {
		t.Errorf("messages: got %q", got)
	}
	// Long text is truncated with an ellipsis.
	got := promptPreview(map[string]any{"positivePrompt": "abcdefghijklmnop"}, 10)
	if r := []rune(got); len(r) != 10 || r[len(r)-1] != '…' {
		t.Errorf("truncate: got %q (len %d)", got, len([]rune(got)))
	}
	// No recognizable prompt yields an empty string.
	if got := promptPreview(map[string]any{"width": 1024}, 60); got != "" {
		t.Errorf("no prompt: got %q", got)
	}
}
