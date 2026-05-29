//go:build !internal

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// TestGetBaseURL verifies the public build is hard-locked to the production
// endpoint and cannot be redirected via the base_url config key or the
// RUNWARE_BASE_URL env var.
func TestGetBaseURL(t *testing.T) {
	t.Run("default when nothing set", func(t *testing.T) {
		viper.Reset()
		if got := GetBaseURL(); got != DefaultBaseURL {
			t.Errorf("GetBaseURL() = %q, want %q", got, DefaultBaseURL)
		}
	})

	t.Run("ignores config value", func(t *testing.T) {
		viper.Reset()
		viper.Set("base_url", "https://evil.example.com/v1")
		if got := GetBaseURL(); got != DefaultBaseURL {
			t.Errorf("GetBaseURL() = %q, want locked %q", got, DefaultBaseURL)
		}
	})

	t.Run("ignores env var even with config file", func(t *testing.T) {
		// Mirror the internal test's setup exactly: a config file value plus an
		// env var. The public build must ignore both and stay on the default.
		tmpDir := t.TempDir()
		configDir = tmpDir
		path := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(path, []byte("base_url: https://config.example.com/v1\n"), 0600); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}

		t.Setenv("RUNWARE_BASE_URL", "https://env.example.com/v1")

		viper.Reset()
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(tmpDir)
		viper.BindEnv("base_url", "RUNWARE_BASE_URL") //nolint:errcheck,gosec
		if err := viper.ReadInConfig(); err != nil {
			t.Fatalf("ReadInConfig() error: %v", err)
		}

		if got := GetBaseURL(); got != DefaultBaseURL {
			t.Errorf("GetBaseURL() = %q, want locked %q", got, DefaultBaseURL)
		}
	})
}
