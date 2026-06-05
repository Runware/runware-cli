package config

import (
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
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

// defaultsKeys are config keys that live under the "defaults" namespace.
var defaultsKeys = map[string]struct{}{
	"model":         {},
	"width":         {},
	"height":        {},
	"steps":         {},
	"cfg_scale":     {},
	"scheduler":     {},
	"output_dir":    {},
	"output_format": {},
	"format":        {},
}

// normalizeConfigKey maps shorthand keys like "steps" to "defaults.steps".
func normalizeConfigKey(key string) string {
	if _, ok := defaultsKeys[key]; ok {
		return "defaults." + key
	}
	return key
}
