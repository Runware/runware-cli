package preset

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

func newSaveCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "save <name> <model> [key=value ...]",
		Short: "Save a named preset",
		Long: `Save a named preset for use with the run command.

The model AIR is required and is stored as the preset's default model.
Additional parameters are passed as key=value pairs using the same syntax
as the run command, and the same schema-driven shell completion is available.`,
		Example: `  # save a preset with model and dimensions
  runware preset save portrait runware:100@1 width=512 height=768

  # save a preset with steps and cfg scale
  runware preset save fast runware:100@1 steps=20 CFGScale=7

  # save a preset for a text model with a system prompt
  runware preset save mychat minimax:m3@0 messages.0.role=system messages.0.content="You are a helpful assistant"`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			model := args[1]
			kvArgs := args[2:]

			params := make(map[string]string, len(kvArgs))
			for _, kv := range kvArgs {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					return fmt.Errorf("invalid parameter %q: must be in key=value form", kv)
				}
				if k == "" {
					return fmt.Errorf("invalid parameter %q: key must not be empty", kv)
				}
				params[k] = v
			}

			p := config.Preset{
				Model:  model,
				Params: params,
			}
			if len(params) == 0 {
				p.Params = nil
			}

			if err := config.SavePreset(name, p); err != nil {
				return fmt.Errorf("failed to save preset: %w", err)
			}

			logger.Info("✓ Preset saved", "name", name)
			return nil
		},
		// Schema-driven key=value completion: args[1] is the model AIR.
		ValidArgsFunction: cmdutil.MakeSchemaArgCompleter(1),
	}

	return cmd
}
