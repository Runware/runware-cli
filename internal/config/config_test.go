package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaskKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"abcdEFGHijklMNOPqrstUVWXyz0123456", "abcdEFGHijklMNOP-" + MaskedKeySuffix},
		{"abcdEFGHijklMNOP", "abcdEFGHijklMNOP-" + MaskedKeySuffix},
		{"short", MaskedKeySuffix},
		{"123456789012345", MaskedKeySuffix},
		{"", MaskedKeySuffix},
	}

	for _, tt := range tests {
		got := MaskKey(tt.input)
		if got != tt.expected {
			t.Errorf("MaskKey(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSaveAndLoad(t *testing.T) {
	// Use a temp dir for config
	tmpDir := t.TempDir()
	configDir = tmpDir

	cfg := &Config{
		APIKey: "test-key-12345678",
		Defaults: Defaults{
			OutputDir: "./test-output",
			Format:    "json",
		},
		Presets: map[string]Preset{
			"fast": {
				Model: "fast-model",
				Params: map[string]string{
					"width":  "256",
					"height": "256",
					"steps":  "1",
				},
			},
		},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify file was created
	path := filepath.Join(tmpDir, "config.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not created: %v", err)
	}

	// Read it back
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	// Basic checks on YAML content
	content := string(data)
	if len(content) == 0 {
		t.Fatal("config file is empty")
	}
}

func TestPresetOperations(t *testing.T) {
	tmpDir := t.TempDir()
	configDir = tmpDir

	// Start with empty config
	cfg := &Config{
		Defaults: Defaults{
			OutputDir: DefaultOutputDir,
			Format:    DefaultFormat,
		},
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Save a preset
	preset := Preset{
		Model: "test-model",
		Params: map[string]string{
			"width":  "768",
			"height": "768",
			"steps":  "20",
		},
	}
	if err := SavePreset("test-preset", preset); err != nil {
		t.Fatalf("SavePreset() error: %v", err)
	}

	// Retrieve it
	got := GetPreset("test-preset")
	if got == nil {
		t.Fatal("GetPreset() returned nil")
	}
	if got.Model != "test-model" {
		t.Errorf("preset model = %q, want %q", got.Model, "test-model")
	}
	if got.Params["width"] != "768" {
		t.Errorf("preset width = %q, want %q", got.Params["width"], "768")
	}

	// Non-existent preset
	if p := GetPreset("nonexistent"); p != nil {
		t.Errorf("GetPreset(nonexistent) = %v, want nil", p)
	}

	// Delete
	if err := DeletePreset("test-preset"); err != nil {
		t.Fatalf("DeletePreset() error: %v", err)
	}
	if p := GetPreset("test-preset"); p != nil {
		t.Errorf("GetPreset after delete = %v, want nil", p)
	}
}

func TestPresetMixedCaseRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configDir = tmpDir
	t.Setenv("RUNWARE_API_KEY", "")

	preset := Preset{
		Model: "runware:100@1",
		Params: map[string]string{
			"positivePrompt": "a cat",
			"CFGScale":       "7",
		},
	}
	if err := SavePreset("MyPreset", preset); err != nil {
		t.Fatalf("SavePreset() error: %v", err)
	}

	got := GetPreset("MyPreset")
	if got == nil {
		t.Fatal("GetPreset(MyPreset) returned nil")
	}
	if got.Params["positivePrompt"] != "a cat" {
		t.Errorf("positivePrompt = %q, want %q (keys: %v)", got.Params["positivePrompt"], "a cat", got.Params)
	}
	if got.Params["CFGScale"] != "7" {
		t.Errorf("CFGScale = %q, want %q (keys: %v)", got.Params["CFGScale"], "7", got.Params)
	}

	// Preset names are case-sensitive too.
	if p := GetPreset("mypreset"); p != nil {
		t.Errorf("GetPreset(mypreset) = %v, want nil", p)
	}

	names := ListPresets()
	found := false
	for _, n := range names {
		if n == "MyPreset" {
			found = true
		}
	}
	if !found {
		t.Errorf("ListPresets() = %v, want to contain %q", names, "MyPreset")
	}

	// The file on disk must keep the original casing.
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	if !strings.Contains(string(data), "positivePrompt") {
		t.Errorf("config file does not contain %q:\n%s", "positivePrompt", data)
	}

	// A subsequent unrelated save must not rewrite the file with lowercased
	// keys (the original corruption symptom).
	if err := SetAPIKey("test-key-12345678"); err != nil {
		t.Fatalf("SetAPIKey() error: %v", err)
	}
	got = GetPreset("MyPreset")
	if got == nil {
		t.Fatal("GetPreset(MyPreset) after SetAPIKey returned nil")
	}
	if got.Params["positivePrompt"] != "a cat" {
		t.Errorf("positivePrompt after SetAPIKey = %q, want %q (keys: %v)", got.Params["positivePrompt"], "a cat", got.Params)
	}
}

func TestGetDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	configDir = tmpDir
	t.Setenv("RUNWARE_API_KEY", "")

	cfg := Get()
	if cfg.Defaults.OutputDir != DefaultOutputDir {
		t.Errorf("OutputDir = %q, want %q", cfg.Defaults.OutputDir, DefaultOutputDir)
	}
	if cfg.Defaults.Format != DefaultFormat {
		t.Errorf("Format = %q, want %q", cfg.Defaults.Format, DefaultFormat)
	}
	if cfg.Defaults.Transport != DefaultTransport {
		t.Errorf("Transport = %q, want %q", cfg.Defaults.Transport, DefaultTransport)
	}
	if cfg.APIKey != "" {
		t.Errorf("APIKey = %q, want empty", cfg.APIKey)
	}
}

func TestTransportRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configDir = tmpDir
	t.Setenv("RUNWARE_API_KEY", "")

	cfg := Get()
	cfg.Defaults.Transport = "http"
	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if got := Get().Defaults.Transport; got != "http" {
		t.Errorf("Transport = %q, want %q", got, "http")
	}
}

func TestAPIKeyEnvOverride(t *testing.T) {
	tmpDir := t.TempDir()
	configDir = tmpDir

	if err := Save(&Config{APIKey: "file-key"}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	t.Setenv("RUNWARE_API_KEY", "env-key")
	if got := Get().APIKey; got != "env-key" {
		t.Errorf("Get().APIKey = %q, want %q", got, "env-key")
	}
	if got := GetAPIKey(); got != "env-key" {
		t.Errorf("GetAPIKey() = %q, want %q", got, "env-key")
	}

	t.Setenv("RUNWARE_API_KEY", "")
	if got := GetAPIKey(); got != "file-key" {
		t.Errorf("GetAPIKey() = %q, want %q", got, "file-key")
	}
}

func TestConfigPath(t *testing.T) {
	tmpDir := t.TempDir()
	configDir = tmpDir

	expected := filepath.Join(tmpDir, "config.yaml")
	if got := ConfigPath(); got != expected {
		t.Errorf("ConfigPath() = %q, want %q", got, expected)
	}
}
