package cmd

import (
	"log/slog"
	"os"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api/transport"
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

// newLogger constructs the charmbracelet/log logger for the CLI.
// It writes to stderr and suppresses timestamps, which are not useful in a CLI context.
func newLogger() *log.Logger {
	return log.NewWithOptions(os.Stderr, log.Options{ReportTimestamp: false})
}

// SetVersionInfo passes build-time version info down to the version command.
func SetVersionInfo(v, c, d string) {
	cmdversion.SetVersionInfo(v, c, d)
}

// NewRootCmd builds and returns the root cobra command.
func NewRootCmd(logger *log.Logger) *cobra.Command {
	root := &cobra.Command{
		Use:   "runware",
		Short: "CLI tool for the Runware inference API",
		Long:  "A command-line tool for interacting with the Runware inference API.\nGenerate images, search models, manage your account, and more.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose"); verbose {
				logger.SetLevel(log.DebugLevel)
			}
			if err := config.Init(); err != nil {
				return err
			}
			t := transport.NewHTTPTransport(config.GetAPIKey(), config.GetBaseURL(), slog.New(logger))
			cmd.SetContext(transport.WithTransport(cmd.Context(), t))
			return nil
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
		auth.NewCmd(logger),
		ping.NewCmd(logger),
		inference.NewCmd(),
		model.NewCmd(),
		account.NewCmd(logger),
		cmdconfig.NewCmd(logger),
		preset.NewCmd(logger),
		cmdversion.NewCmd(),
		cmdcompletion.NewCmd(),
	)

	return root
}

// Root returns the root command for tools like doc generators.
func Root() *cobra.Command {
	return NewRootCmd(newLogger())
}
