package config

import (
	"testing"

	"github.com/runware/runware-cli/internal/config"
)

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
