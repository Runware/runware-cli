package preset

import (
	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

// NewCmd returns the preset command with list, show, save, and delete subcommands.
func NewCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preset",
		Short: "Manage named presets",
	}
	cmd.AddCommand(newListCmd(logger))
	cmd.AddCommand(newShowCmd())
	cmd.AddCommand(newSaveCmd(logger))
	cmd.AddCommand(newDeleteCmd(logger))
	return cmd
}

func completePresetNames(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	config.Init() //nolint:errcheck,gosec
	return config.ListPresets(), cobra.ShellCompDirectiveNoFileComp
}
