//go:build internal

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestGetBaseURL(t *testing.T) {
	t.Run("default when nothing set", func(t *testing.T) {
		viper.Reset()
		if got := GetBaseURL(); got != DefaultBaseURL {
			t.Errorf("GetBaseURL() = %q, want %q", got, DefaultBaseURL)
		}
	})

	t.Run("config value overrides default", func(t *testing.T) {
		viper.Reset()
		viper.Set("base_url", "https://config.example.com/v1")
		if got := GetBaseURL(); got != "https://config.example.com/v1" {
			t.Errorf("GetBaseURL() = %q, want %q", got, "https://config.example.com/v1")
		}
	})

	t.Run("env var overrides config file value", func(t *testing.T) {
		// Write a real config file so the value lives in Viper's config layer
		// (lower precedence than env), mirroring runtime behavior.
		tmpDir := t.TempDir()
		configDir = tmpDir
		path := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(path, []byte("base_url: https://config.example.com/v1\n"), 0600); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}

		t.Setenv("RUNWARE_BASE_URL", "https://env.example.com/v1")

		// Mirror Init()'s Viper wiring, but pointed at the temp dir.
		viper.Reset()
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(tmpDir)
		viper.BindEnv("base_url", "RUNWARE_BASE_URL") //nolint:errcheck,gosec
		if err := viper.ReadInConfig(); err != nil {
			t.Fatalf("ReadInConfig() error: %v", err)
		}

		if got := GetBaseURL(); got != "https://env.example.com/v1" {
			t.Errorf("GetBaseURL() = %q, want %q", got, "https://env.example.com/v1")
		}
	})
}
