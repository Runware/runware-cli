package cmd

import (
	"fmt"
	"os"

	"github.com/runware/runware-cli/internal/cmd/account"
	"github.com/runware/runware-cli/internal/cmd/auth"
	cmdcompletion "github.com/runware/runware-cli/internal/cmd/completion"
	cmdconfig "github.com/runware/runware-cli/internal/cmd/config"
	"github.com/runware/runware-cli/internal/cmd/inference"
	"github.com/runware/runware-cli/internal/cmd/model"
	"github.com/runware/runware-cli/internal/cmd/ping"
	"github.com/runware/runware-cli/internal/cmd/preset"
	cmdversion "github.com/runware/runware-cli/internal/cmd/version"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

// SetVersionInfo passes build-time version info down to the version command.
func SetVersionInfo(v, c, d string) {
	cmdversion.SetVersionInfo(v, c, d)
}

// NewRootCmd builds and returns the root cobra command.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "runware",
		Short: "CLI tool for the Runware inference API",
		Long:  "A command-line tool for interacting with the Runware inference API.\nGenerate images, search models, manage your account, and more.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return config.Init()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringP("format", "F", "", "CLI output format: table, json, yaml")
	root.PersistentFlags().BoolP("verbose", "v", false, "Show request/response details")
	root.PersistentFlags().Bool("debug", false, "Show full debug output")

	root.RegisterFlagCompletionFunc("format", func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{"table", "json", "yaml"}, cobra.ShellCompDirectiveNoFileComp
	})

	root.AddCommand(
		auth.New(),
		ping.New(),
		inference.New(),
		model.New(),
		account.New(),
		cmdconfig.New(),
		preset.New(),
		cmdversion.New(),
		cmdcompletion.New(),
	)

	return root
}

// Execute runs the root command.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

// Root returns the root command for tools like doc generators.
func Root() *cobra.Command {
	return NewRootCmd()
}
