package config

import (
	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

const (
	keyOutputDir = "output_dir"
	keyFormat    = "format"
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
