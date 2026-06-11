package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	DefaultBaseURL   = "https://api.runware.ai/v1"
	DefaultWSBaseURL = "wss://ws-api.runware.ai/v1"
	MaskedKeySuffix  = "•••••"
	DefaultOutputDir = "./outputs"
	DefaultFormat    = "table"
	// DefaultTransport matches transport.SchemeWS; kept as a literal so the
	// config package stays free of internal imports.
	DefaultTransport = "ws"
)

// Defaults holds default values for commands.
type Defaults struct {
	OutputDir string `yaml:"output_dir"`
	Format    string `yaml:"format"`
	Transport string `yaml:"transport"`
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
	APIKey   string            `yaml:"api_key,omitempty"`
	Defaults Defaults          `yaml:"defaults"`
	Presets  map[string]Preset `yaml:"presets,omitempty"`
}

var configDir string

// ValidDefaultsKeys returns the set of config keys that live under the
// "defaults" namespace and can be set via `config set`.
func ValidDefaultsKeys() map[string]struct{} {
	return map[string]struct{}{
		"output_dir": {},
		"format":     {},
		"transport":  {},
	}
}

// Init locates the config directory (~/.runware) and validates that the
// config file, if present, parses cleanly so startup fails loudly on a
// corrupt file.
func Init() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot find home directory: %w", err)
	}

	configDir = filepath.Join(home, ".runware")

	if _, err := load(); err != nil {
		return err
	}

	return nil
}

// load reads and parses the config file. A missing file is not an error; it
// yields a zero config.
func load() (*Config, error) {
	var cfg Config
	if configDir == "" {
		// Init has not run; behave as if no config file exists rather than
		// reading a relative ./config.yaml.
		return &cfg, nil
	}
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &cfg, nil
		}
		return nil, fmt.Errorf("error reading config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("error reading config: %w", err)
	}
	return &cfg, nil
}

// Get returns the current merged config: file values with fallback defaults
// applied and the RUNWARE_API_KEY environment variable taking precedence
// over the file's API key.
func Get() *Config {
	cfg, err := load()
	if err != nil {
		cfg = &Config{}
	}
	if cfg.Defaults.OutputDir == "" {
		cfg.Defaults.OutputDir = DefaultOutputDir
	}
	if cfg.Defaults.Format == "" {
		cfg.Defaults.Format = DefaultFormat
	}
	if cfg.Defaults.Transport == "" {
		cfg.Defaults.Transport = DefaultTransport
	}
	if v := os.Getenv("RUNWARE_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	return cfg
}

// GetAPIKey returns the API key.
func GetAPIKey() string {
	return Get().APIKey
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

// Save writes the config to disk. Subsequent Get() calls re-read the file,
// so they reflect the change.
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
