package cmdutil

import (
	"slices"
	"testing"

	"github.com/spf13/cobra"
)

// test-local string constants to satisfy the goconst linter.
const (
	testModelAIR = "runware:101@1"
	testWidthKV  = "width=512"
)

func TestAnnotatePresetCompletions(t *testing.T) {
	completions := []cobra.Completion{
		cobra.CompletionWithDesc("width=", "Width of the image"),
		cobra.CompletionWithDesc("height=", "Height of the image"),
		"steps=",
	}
	presetKeys := map[string]string{
		"width": "512",
		"steps": "20",
	}

	got := annotatePresetCompletions(completions, presetKeys)

	want := []cobra.Completion{
		cobra.CompletionWithDesc("width=", "[preset: 512] Width of the image"),
		cobra.CompletionWithDesc("height=", "Height of the image"),
		cobra.CompletionWithDesc("steps=", "[preset: 20] "),
	}
	if !slices.Equal(got, want) {
		t.Errorf("annotatePresetCompletions() = %v, want %v", got, want)
	}
}

func TestSplitModelArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantModel string
		wantKV    []string
	}{
		{
			name:      "model with params",
			args:      []string{testModelAIR, testWidthKV},
			wantModel: testModelAIR,
			wantKV:    []string{testWidthKV},
		},
		{
			name:      "params only, model omitted",
			args:      []string{"positivePrompt=a red mountain", testWidthKV},
			wantModel: "",
			wantKV:    []string{"positivePrompt=a red mountain", testWidthKV},
		},
		{
			name:      "model only",
			args:      []string{testModelAIR},
			wantModel: testModelAIR,
			wantKV:    []string{},
		},
		{
			name:      "no args",
			args:      nil,
			wantModel: "",
			wantKV:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, kv := SplitModelArgs(tt.args)
			if model != tt.wantModel {
				t.Errorf("model = %q, want %q", model, tt.wantModel)
			}
			if !slices.Equal(kv, tt.wantKV) {
				t.Errorf("kvArgs = %v, want %v", kv, tt.wantKV)
			}
		})
	}
}
