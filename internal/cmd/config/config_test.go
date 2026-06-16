package config

import (
	"io"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/config"
)

// TestResetPreservesPresetsAndAPIKey verifies that `config reset` restores the
// default values while leaving the API key and saved presets untouched.
func TestResetPreservesPresetsAndAPIKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNWARE_API_KEY", "")
	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	seed := &config.Config{
		APIKey: "secret-key-1234567890",
		Defaults: config.Defaults{
			OutputDir: "./custom-output",
			Format:    "json",
			Transport: "http",
		},
		Presets: map[string]config.Preset{
			"testpreset": {
				Model: "runware:100@1",
				Params: map[string]string{
					"width": "512",
				},
			},
		},
	}
	if err := config.Save(seed); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	cmd := newResetCmd(log.New(io.Discard))
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("reset RunE error: %v", err)
	}

	got := config.Get()

	if got.APIKey != seed.APIKey {
		t.Errorf("APIKey = %q, want %q (should be preserved)", got.APIKey, seed.APIKey)
	}

	p := config.GetPreset("testpreset")
	if p == nil {
		t.Fatal("preset 'testpreset' was deleted by reset, want it preserved")
	}
	if p.Model != "runware:100@1" || p.Params["width"] != "512" {
		t.Errorf("preset mutated by reset: got %+v", *p)
	}

	if got.Defaults.OutputDir != config.DefaultOutputDir {
		t.Errorf("OutputDir = %q, want %q", got.Defaults.OutputDir, config.DefaultOutputDir)
	}
	if got.Defaults.Format != config.DefaultFormat {
		t.Errorf("Format = %q, want %q", got.Defaults.Format, config.DefaultFormat)
	}
	if got.Defaults.Transport != config.DefaultTransport {
		t.Errorf("Transport = %q, want %q", got.Defaults.Transport, config.DefaultTransport)
	}
}

// TestResetDoesNotPersistEnvAPIKey verifies that an API key supplied only via
// RUNWARE_API_KEY is not written into the config file by `config reset`.
func TestResetDoesNotPersistEnvAPIKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNWARE_API_KEY", "env-only-secret")
	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	// Seed a config file with no stored api_key.
	if err := config.Save(&config.Config{Defaults: config.Defaults{}}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	cmd := newResetCmd(log.New(io.Discard))
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("reset RunE error: %v", err)
	}

	onDisk, err := config.FileConfig()
	if err != nil {
		t.Fatalf("FileConfig() error: %v", err)
	}
	if onDisk.APIKey != "" {
		t.Errorf("api_key written to file = %q, want empty (env key must not be persisted)", onDisk.APIKey)
	}
}

func TestApplyConfigValue_KnownKeys(t *testing.T) {
	tests := []struct {
		key   string
		value string
		check func(cfg *config.Config) string
	}{
		{
			key:   "api_key",
			value: "test-key",
			check: func(cfg *config.Config) string { return cfg.APIKey },
		},
		{
			key:   keyOutputDir,
			value: "./elsewhere",
			check: func(cfg *config.Config) string { return cfg.Defaults.OutputDir },
		},
		{
			key:   "defaults." + keyOutputDir,
			value: "./elsewhere",
			check: func(cfg *config.Config) string { return cfg.Defaults.OutputDir },
		},
		{
			key:   keyFormat,
			value: "json",
			check: func(cfg *config.Config) string { return cfg.Defaults.Format },
		},
		{
			key:   "defaults." + keyFormat,
			value: "yaml",
			check: func(cfg *config.Config) string { return cfg.Defaults.Format },
		},
		{
			key:   keyTransport,
			value: "http",
			check: func(cfg *config.Config) string { return cfg.Defaults.Transport },
		},
		{
			key:   "defaults." + keyTransport,
			value: "ws",
			check: func(cfg *config.Config) string { return cfg.Defaults.Transport },
		},
	}

	for _, tt := range tests {
		var cfg config.Config
		if err := applyConfigValue(&cfg, tt.key, tt.value); err != nil {
			t.Errorf("applyConfigValue(%q) error: %v", tt.key, err)
			continue
		}
		if got := tt.check(&cfg); got != tt.value {
			t.Errorf("applyConfigValue(%q) set %q, want %q", tt.key, got, tt.value)
		}
	}
}

func TestApplyConfigValue_UnknownKey(t *testing.T) {
	var cfg config.Config
	err := applyConfigValue(&cfg, "bogus", "1")
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
}
