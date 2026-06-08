package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const (
	DefaultBaseURL   = "https://api.runware.ai/v1"
	DefaultWSBaseURL = "wss://ws-api.runware.ai/v1"
	MaskedKeySuffix  = "•••••"
	DefaultOutputDir = "./outputs"
	DefaultFormat    = "table"
)

// Defaults holds default values for commands.
type Defaults struct {
	OutputDir string `mapstructure:"output_dir" yaml:"output_dir"`
	Format    string `mapstructure:"format" yaml:"format"`
}

// Preset is a named set of inference parameters for use with the run command.
// Model is stored separately because it is structurally special: it is needed
// to fetch the JSON Schema during shell completion and is passed as the first
// positional argument to client.Run. All other parameters are stored as
// arbitrary key=value string pairs, consistent with how the run command
// accepts them on the command line.
type Preset struct {
	Model  string            `yaml:"model,omitempty"`
	Params map[string]string `yaml:"params,omitempty"`
}

// Config is the full configuration structure.
type Config struct {
	APIKey   string            `mapstructure:"api_key" yaml:"api_key,omitempty"`
	Defaults Defaults          `mapstructure:"defaults" yaml:"defaults"`
	Presets  map[string]Preset `mapstructure:"presets" yaml:"presets,omitempty"`
}

var configDir string

// Init sets up Viper to read from ~/.runware/config.yaml and sets defaults.
func Init() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot find home directory: %w", err)
	}

	configDir = filepath.Join(home, ".runware")

	viper.SetDefault("defaults.output_dir", DefaultOutputDir)
	viper.SetDefault("defaults.format", DefaultFormat)

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(configDir)

	// Environment variable overrides
	viper.SetEnvPrefix("")
	viper.BindEnv("api_key", "RUNWARE_API_KEY") //nolint:errcheck,gosec

	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return fmt.Errorf("error reading config: %w", err)
		}
		// Config file not found is fine — we use defaults
	}

	return nil
}

// Get returns the current merged config.
func Get() *Config {
	var cfg Config
	viper.Unmarshal(&cfg) //nolint:errcheck,gosec
	return &cfg
}

// GetAPIKey returns the API key.
func GetAPIKey() string {
	return viper.GetString("api_key")
}

// GetBaseURL returns the HTTP API base URL.
// RUNWARE_BASE_URL overrides the default; this is not exposed in the user-facing
// config file and is intended for internal testing only.
func GetBaseURL() string {
	if v := os.Getenv("RUNWARE_BASE_URL"); v != "" {
		return v
	}
	return DefaultBaseURL
}

// GetWSBaseURL returns the WebSocket API base URL.
// RUNWARE_WS_BASE_URL overrides the default; this is not exposed in the
// user-facing config file and is intended for internal testing only.
func GetWSBaseURL() string {
	if v := os.Getenv("RUNWARE_WS_BASE_URL"); v != "" {
		return v
	}
	return DefaultWSBaseURL
}

// ConfigDir returns the config directory path.
func ConfigDir() string {
	return configDir
}

// ConfigPath returns the full path to the config file.
func ConfigPath() string {
	return filepath.Join(configDir, "config.yaml")
}

// EnsureConfigDir creates the config directory if it doesn't exist.
func EnsureConfigDir() error {
	return os.MkdirAll(configDir, 0700)
}

// Save writes the config to disk and reloads Viper so subsequent Get() calls reflect the change.
func Save(cfg *Config) error {
	if err := EnsureConfigDir(); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("cannot marshal config: %w", err)
	}

	if err := os.WriteFile(ConfigPath(), data, 0600); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}

	// Re-read so Viper picks up the changes
	viper.ReadInConfig() //nolint:errcheck,gosec

	return nil
}

// SetAPIKey saves the API key.
func SetAPIKey(key string) error {
	cfg := Get()
	cfg.APIKey = key
	return Save(cfg)
}

// RemoveAPIKey clears the stored API key.
func RemoveAPIKey() error {
	cfg := Get()
	cfg.APIKey = ""
	return Save(cfg)
}

// GetPreset returns a named preset, or nil if not found.
func GetPreset(name string) *Preset {
	cfg := Get()
	if cfg.Presets == nil {
		return nil
	}
	p, ok := cfg.Presets[name]
	if !ok {
		return nil
	}
	return &p
}

// SavePreset saves a named preset.
func SavePreset(name string, preset Preset) error {
	cfg := Get()
	if cfg.Presets == nil {
		cfg.Presets = make(map[string]Preset)
	}
	cfg.Presets[name] = preset
	return Save(cfg)
}

// DeletePreset removes a named preset.
func DeletePreset(name string) error {
	cfg := Get()
	if cfg.Presets == nil {
		return nil
	}
	delete(cfg.Presets, name)
	return Save(cfg)
}

// ListPresets returns all preset names.
func ListPresets() []string {
	cfg := Get()
	names := make([]string, 0, len(cfg.Presets))
	for name := range cfg.Presets {
		names = append(names, name)
	}
	return names
}

// MaskKey masks an API key for display, showing the first 16 characters followed by "•••••".
// If the key is shorter than 16 characters, only "•••••" is shown.
func MaskKey(key string) string {
	if len(key) < 16 {
		return MaskedKeySuffix
	}
	return key[:16] + "-" + MaskedKeySuffix
}
