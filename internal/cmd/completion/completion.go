package completion

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const (
	shellBash       = "bash"
	shellZsh        = "zsh"
	shellFish       = "fish"
	shellPowerShell = "powershell"
)

// detectShell returns the name of the current shell by inspecting well-known
// environment variables that shells set automatically. Returns an empty string
// if the shell cannot be determined.
func detectShell() string {
	switch {
	case os.Getenv("FISH_VERSION") != "":
		return shellFish
	case os.Getenv("ZSH_VERSION") != "":
		return shellZsh
	case os.Getenv("BASH_VERSION") != "":
		return shellBash
	case os.Getenv("PSModulePath") != "":
		return shellPowerShell
	}
	// Fallback: parse $SHELL (Unix only; not set on Windows/PowerShell).
	switch filepath.Base(os.Getenv("SHELL")) {
	case shellBash:
		return shellBash
	case shellZsh:
		return shellZsh
	case shellFish:
		return shellFish
	case "pwsh", shellPowerShell:
		return shellPowerShell
	}
	return ""
}

// NewCmd returns the completion command for generating shell completion scripts.
func NewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for runware.

When called without an argument, runware attempts to detect your current shell
by inspecting environment variables (FISH_VERSION, ZSH_VERSION, BASH_VERSION,
PSModulePath). Pass an explicit shell name to override.`,
  # Auto-detect shell and load completions for the current session
  # Bash/Zsh:
  source <(runware completion)
  # Fish:
  source (runware completion | psub)
  # Bash — Linux system-wide
  runware completion bash | sudo tee /etc/bash_completion.d/runware

  # Bash — macOS (Homebrew bash-completion@2)
  runware completion bash > $(brew --prefix)/etc/bash_completion.d/runware

  # Bash — per-user
  runware completion bash >> ~/.bash_completion

  # Zsh
  mkdir -p ~/.zfunc && runware completion zsh > ~/.zfunc/_runware

  # Fish
  runware completion fish > ~/.config/fish/completions/runware.fish

  # PowerShell — add to $PROFILE
  Invoke-Expression (& runware completion powershell | Out-String)`,
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{shellBash, shellZsh, shellFish, shellPowerShell},
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := ""
			if len(args) == 1 {
				shell = args[0]
			} else {
				shell = detectShell()
				if shell == "" {
					return fmt.Errorf("could not detect shell; specify one explicitly: bash | zsh | fish | powershell")
				}
			}

			root := cmd.Root()
			switch shell {
			case shellBash:
				return root.GenBashCompletion(os.Stdout)
			case shellZsh:
				return root.GenZshCompletion(os.Stdout)
			case shellFish:
				return root.GenFishCompletion(os.Stdout, true)
			case shellPowerShell:
				return root.GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell %q; supported: bash | zsh | fish | powershell", shell)
			}
		},
	}
}
