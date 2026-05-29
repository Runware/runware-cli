package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
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
			Model:        "test-model",
			Width:        512,
			Height:       512,
			Steps:        10,
			CFGScale:     7.0,
			Scheduler:    "euler",
			OutputDir:    "./test-output",
			OutputFormat: "jpg",
			Format:       "json",
		},
		Presets: map[string]Preset{
			"fast": {
				Model:  "fast-model",
				Width:  256,
				Height: 256,
				Steps:  1,
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

	// Point Viper at the temp dir so ReadInConfig works after Save
	viper.Reset()
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(tmpDir)

	// Start with empty config
	cfg := &Config{
		Defaults: Defaults{
			Model:  DefaultModel,
			Width:  DefaultWidth,
			Height: DefaultHeight,
		},
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Save a preset
	preset := Preset{
		Model:  "test-model",
		Width:  768,
		Height: 768,
		Steps:  20,
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
	if got.Width != 768 {
		t.Errorf("preset width = %d, want %d", got.Width, 768)
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

func TestConfigPath(t *testing.T) {
	tmpDir := t.TempDir()
	configDir = tmpDir

	expected := filepath.Join(tmpDir, "config.yaml")
	if got := ConfigPath(); got != expected {
		t.Errorf("ConfigPath() = %q, want %q", got, expected)
	}
}
