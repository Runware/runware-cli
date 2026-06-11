package config

import (
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

const (
	keyAPIKey    = "api_key"
	keyOutputDir = "output_dir"
	keyFormat    = "format"
	keyTransport = "transport"
)

// NewCmd returns the config command with show, set, reset, and path subcommands.
func NewCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration",
	}
	cmd.AddCommand(newShowCmd())
	cmd.AddCommand(newSetCmd(logger))
	cmd.AddCommand(newResetCmd(logger))
	cmd.AddCommand(newPathCmd())
	return cmd
}

// normalizeConfigKey maps shorthand keys like "steps" to "defaults.steps".
func normalizeConfigKey(key string) string {
	if _, ok := config.ValidDefaultsKeys()[key]; ok {
		return "defaults." + key
	}
	return key
}

// applyConfigValue sets the config field addressed by key (shorthand or
// fully-qualified) to value. Unknown keys are an error.
func applyConfigValue(cfg *config.Config, key, value string) error {
	switch normalizeConfigKey(key) {
	case keyAPIKey:
		cfg.APIKey = value
	case "defaults." + keyOutputDir:
		cfg.Defaults.OutputDir = value
	case "defaults." + keyFormat:
		cfg.Defaults.Format = value
	case "defaults." + keyTransport:
		cfg.Defaults.Transport = value
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return nil
}
