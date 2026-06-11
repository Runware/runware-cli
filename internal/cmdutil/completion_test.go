package cmdutil

import (
	"slices"
	"strings"
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

func TestSanitizePresetValue(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain value untouched",
			in:   "512",
			want: "512",
		},
		{
			name: "tabs and newlines become spaces",
			in:   "a red\tmountain\nat dawn\r",
			want: "a red mountain at dawn ",
		},
		{
			name: "long value truncated with ellipsis",
			in:   strings.Repeat("x", 50),
			want: strings.Repeat("x", 40) + "…",
		},
		{
			name: "multi-byte runes not split",
			in:   strings.Repeat("é", 50),
			want: strings.Repeat("é", 40) + "…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizePresetValue(tt.in); got != tt.want {
				t.Errorf("sanitizePresetValue(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestAnnotatePresetCompletions_SanitizesValue(t *testing.T) {
	completions := []cobra.Completion{
		cobra.CompletionWithDesc("positivePrompt=", "Text instruction"),
	}
	presetKeys := map[string]string{
		"positivePrompt": "a red\nmountain",
	}

	got := annotatePresetCompletions(completions, presetKeys)

	want := []cobra.Completion{
		cobra.CompletionWithDesc("positivePrompt=", "[preset: a red mountain] Text instruction"),
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
