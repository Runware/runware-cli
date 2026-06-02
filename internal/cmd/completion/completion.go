package completion

import (
	"os"

	"github.com/spf13/cobra"
)

// New returns the completion command for generating shell completion scripts.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion scripts",
		Example: `  # generate bash completions
  runware completion bash > /etc/bash_completion.d/runware

  # generate zsh completions
  runware completion zsh > ~/.zfunc/_runware

  # generate fish completions
  runware completion fish > ~/.config/fish/completions/runware.fish`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish"},
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(os.Stdout)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			default:
				return cmd.Help()
			}
		},
	}
}
