package preset

import (
	"fmt"

	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [name]",
		Short: "Delete a preset",
		Example: `  # delete a preset
  runware preset delete portrait`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completePresetNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if config.GetPreset(name) == nil {
				return fmt.Errorf("preset '%s' not found", name)
			}

			if err := config.DeletePreset(name); err != nil {
				return fmt.Errorf("failed to delete preset: %w", err)
			}

			output.Success(fmt.Sprintf("Preset '%s' deleted", name))
			return nil
		},
	}
}
