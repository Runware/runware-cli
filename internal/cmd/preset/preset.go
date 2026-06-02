package preset

import (
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

// New returns the preset command with list, show, save, and delete subcommands.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preset",
		Short: "Manage named presets",
	}
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newShowCmd())
	cmd.AddCommand(newSaveCmd())
	cmd.AddCommand(newDeleteCmd())
	return cmd
}

func completePresetNames(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	config.Init() //nolint:errcheck,gosec
	return config.ListPresets(), cobra.ShellCompDirectiveNoFileComp
}
