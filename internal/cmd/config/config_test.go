package config

import (
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
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
			Format:    testJSON,
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

// TestSetDoesNotPersistEnvAPIKey verifies that an API key supplied only via
// RUNWARE_API_KEY is not written into the config file by `config set`.
func TestSetDoesNotPersistEnvAPIKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNWARE_API_KEY", "env-only-secret")
	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}

	if err := config.Save(&config.Config{Defaults: config.Defaults{}}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	cmd := newSetCmd(log.New(io.Discard))
	if err := cmd.RunE(cmd, []string{"format", testJSON}); err != nil {
		t.Fatalf("set RunE error: %v", err)
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
			value: testJSON,
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
		{
			key:   keyEnvironment,
			value: testStaging,
			check: func(cfg *config.Config) string { return cfg.Defaults.Environment },
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

const (
	testStaging     = "staging"
	testDevelopment = "development"
	testJSON        = "json"
)

// withConfigHome gives the test an empty config file and no RUNWARE_ENV.
func withConfigHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNWARE_API_KEY", "")
	t.Setenv(config.EnvironmentFromEnvVar, "")
	if err := config.Init(); err != nil {
		t.Fatalf("config.Init() error: %v", err)
	}
}

// setEnvironmentCase is a name given to `config set environment` and the
// environment the config holds after it.
type setEnvironmentCase struct {
	name    string
	value   string
	wantErr bool
	want    string
}

func TestSetEnvironmentChecksTheName(t *testing.T) {
	cases := []setEnvironmentCase{
		{
			name:    "a name the API refuses",
			value:   "Staging",
			wantErr: true,
		},
		{
			name:  "a valid name",
			value: testStaging,
			want:  testStaging,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withConfigHome(t)
			cmd := newSetCmd(log.New(io.Discard))

			err := cmd.RunE(cmd, []string{keyEnvironment, tc.value})

			if gotErr := err != nil && errors.Is(err, serverlessapi.ErrEnvironmentName); gotErr != tc.wantErr {
				t.Fatalf("set %s: err = %v, want the name rule %v", tc.value, err, tc.wantErr)
			}
			if got := config.Get().Defaults.Environment; got != tc.want {
				t.Fatalf("environment = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResetClearsTheEnvironment(t *testing.T) {
	withConfigHome(t)
	if err := config.Save(&config.Config{
		Defaults: config.Defaults{
			Environment: testStaging,
		},
	}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	cmd := newResetCmd(log.New(io.Discard))
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("reset RunE error: %v", err)
	}
	if name, _ := config.GetEnvironment(); name != "" {
		t.Fatalf("environment = %q after reset, want production (empty)", name)
	}
}

// showEnvironmentCase is a default environment, a `config show` format and
// the environment it names.
type showEnvironmentCase struct {
	name       string
	configured string
	envVar     string
	format     string
	want       string
}

func TestShowNamesTheConfiguredEnvironment(t *testing.T) {
	cases := []showEnvironmentCase{
		{
			name:   "unset",
			format: testJSON,
			want:   "production",
		},
		{
			name:       "config",
			configured: testStaging,
			format:     testJSON,
			want:       testStaging,
		},
		{
			name:       "config under another RUNWARE_ENV",
			configured: testStaging,
			envVar:     testDevelopment,
			format:     testJSON,
			want:       testStaging,
		},
		{
			name:       "config in a table",
			configured: testStaging,
			format:     "table",
			want:       testStaging,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withConfigHome(t)
			if err := config.Save(&config.Config{
				Defaults: config.Defaults{
					Environment: tc.configured,
				},
			}); err != nil {
				t.Fatalf("Save() error: %v", err)
			}
			t.Setenv(config.EnvironmentFromEnvVar, tc.envVar)

			if got := showOutput(t, tc.format); !showNamesEnvironment(got, tc.format, tc.want) {
				t.Fatalf("config show = %s, want environment %q", got, tc.want)
			}
		})
	}
}

// showNamesEnvironment reports whether `config show` output in format names
// environment.
func showNamesEnvironment(output, format, environment string) bool {
	if format == testJSON {
		return strings.Contains(output, `"environment": "`+environment+`"`)
	}
	return environmentRowNames(output, environment)
}

// showOutput runs `config show` in format and returns what it printed.
func showOutput(t *testing.T, format string) string {
	t.Helper()
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = stdout
	defer func() { os.Stdout = old }()
	cmd := newShowCmd()
	cmd.PersistentFlags().String("format", format, "")
	runErr := cmd.RunE(cmd, nil)
	_ = stdout.Close()
	if runErr != nil {
		t.Fatalf("show RunE error: %v", runErr)
	}
	data, err := os.ReadFile(stdout.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// environmentRowNames reports whether a `config show` table has an environment
// row with value.
func environmentRowNames(table, value string) bool {
	for line := range strings.Lines(table) {
		if strings.Contains(line, keyEnvironment) && strings.Contains(line, value) {
			return true
		}
	}
	return false
}

// overrideWarningCase is a config change under a RUNWARE_ENV and whether it
// warns that RUNWARE_ENV overrides it.
type overrideWarningCase struct {
	name   string
	envVar string
	args   []string
	want   bool
}

func TestSetWarnsWhenRunwareEnvOverridesTheEnvironment(t *testing.T) {
	cases := []overrideWarningCase{
		{
			name:   "another environment",
			envVar: testDevelopment,
			args:   []string{keyEnvironment, testStaging},
			want:   true,
		},
		{
			name:   "the same environment",
			envVar: testStaging,
			args:   []string{keyEnvironment, testStaging},
		},
		{
			name:   "while RUNWARE_ENV is production",
			envVar: config.ProductionEnvironment,
			args:   []string{keyEnvironment, testStaging},
			want:   true,
		},
		{
			name: "without RUNWARE_ENV",
			args: []string{keyEnvironment, testStaging},
		},
		{
			name:   "another key",
			envVar: testDevelopment,
			args:   []string{keyTransport, "http"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withConfigHome(t)
			t.Setenv(config.EnvironmentFromEnvVar, tc.envVar)
			var logs strings.Builder
			cmd := newSetCmd(log.New(&logs))

			if err := cmd.RunE(cmd, tc.args); err != nil {
				t.Fatal(err)
			}
			if got := warnedOverride(logs.String()); got != tc.want {
				t.Fatalf("warned = %v, want %v (logs %q)", got, tc.want, logs.String())
			}
		})
	}
}

func TestResetWarnsWhenRunwareEnvOverridesTheEnvironment(t *testing.T) {
	cases := []overrideWarningCase{
		{
			name:   "another environment",
			envVar: testDevelopment,
			want:   true,
		},
		{
			name:   "while RUNWARE_ENV is production",
			envVar: config.ProductionEnvironment,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withConfigHome(t)
			t.Setenv(config.EnvironmentFromEnvVar, tc.envVar)
			var logs strings.Builder
			cmd := newResetCmd(log.New(&logs))

			if err := cmd.RunE(cmd, nil); err != nil {
				t.Fatal(err)
			}
			if got := warnedOverride(logs.String()); got != tc.want {
				t.Fatalf("warned = %v, want %v (logs %q)", got, tc.want, logs.String())
			}
		})
	}
}

// warnedOverride reports whether logs warn that RUNWARE_ENV overrides the
// configured environment.
func warnedOverride(logs string) bool {
	return strings.Contains(logs, "RUNWARE_ENV overrides it")
}

func TestSetCompletesTheEnvironmentKey(t *testing.T) {
	cmd := newSetCmd(log.New(io.Discard))
	keys, _ := cmd.ValidArgsFunction(cmd, nil, "")
	if !slices.Contains(keys, keyEnvironment) {
		t.Fatalf("config set completes %v, want %q among them", keys, keyEnvironment)
	}
}
