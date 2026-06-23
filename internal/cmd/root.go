package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/cmd/account"
	"github.com/runware/runware-cli/internal/cmd/auth"
	cmdcompletion "github.com/runware/runware-cli/internal/cmd/completion"
	cmdconfig "github.com/runware/runware-cli/internal/cmd/config"
	"github.com/runware/runware-cli/internal/cmd/model"
	"github.com/runware/runware-cli/internal/cmd/ping"
	"github.com/runware/runware-cli/internal/cmd/preset"
	cmdrun "github.com/runware/runware-cli/internal/cmd/run"
	cmdupload "github.com/runware/runware-cli/internal/cmd/upload"
	cmdversion "github.com/runware/runware-cli/internal/cmd/version"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// newLogger constructs the charmbracelet/log logger for the CLI.
// It writes to stderr and suppresses timestamps, which are not useful in a CLI context.
func newLogger() *log.Logger {
	return log.NewWithOptions(os.Stderr, log.Options{ReportTimestamp: false})
}

// NewRootCmd builds and returns the root cobra command.
func NewRootCmd(logger *log.Logger) *cobra.Command {
	// Initialize config eagerly so subcommand constructors can read defaults
	// for use in flag descriptions (help text). Errors are intentionally
	// swallowed here — Init() is idempotent and PersistentPreRunE calls it
	// again, surfacing any real error to the user at command-run time.
	_ = config.Init()
	cfg := config.Get()

	root := &cobra.Command{
		Use:   "runware",
		Short: "CLI tool for the Runware API",
		Long: `A command-line tool for interacting with the full Runware API.
Run image generation, video generation, audio generation, 3D, upscaling, background removal, captioning, search models, and more.

Use of Runware services is subject to our Terms of Service (https://runware.ai/terms) and Privacy Policy (https://runware.ai/privacy).`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose"); verbose {
				logger.SetLevel(log.DebugLevel)
			}
			if f, _ := cmd.Root().PersistentFlags().GetString("format"); f != "" {
				if !output.ValidFormat(f) {
					return fmt.Errorf("invalid format %q: must be one of: %s", f, strings.Join(output.ValidFormats(), ", "))
				}
			}
			if t, _ := cmd.Root().PersistentFlags().GetString("transport"); t != "" {
				if !transport.ValidTransport(t) {
					return fmt.Errorf("invalid transport %q: must be one of: %s", t, strings.Join(transport.ValidTransports(), ", "))
				}
			}
			return config.Init()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringP("format", "F", cfg.Defaults.Format, "CLI output format: table, json, yaml")
	root.PersistentFlags().BoolP("verbose", "v", false, "Show request/response details")
	root.PersistentFlags().Bool("debug", false, "Show full debug output")
	root.PersistentFlags().String("transport", cfg.Defaults.Transport, "Transport protocol: ws (WebSocket) or http (REST)")

	root.RegisterFlagCompletionFunc("format", func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		formats := output.ValidFormats()
		completions := make([]cobra.Completion, len(formats))
		copy(completions, formats)
		return completions, cobra.ShellCompDirectiveNoFileComp
	})

	root.RegisterFlagCompletionFunc("transport", func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		transports := transport.ValidTransports()
		completions := make([]cobra.Completion, len(transports))
		copy(completions, transports)
		return completions, cobra.ShellCompDirectiveNoFileComp
	})

	root.AddCommand(
		auth.NewCmd(logger),
		ping.NewCmd(logger),
		model.NewCmd(logger),
		account.NewCmd(logger),
		cmdconfig.NewCmd(logger),
		preset.NewCmd(logger),
		cmdrun.NewCmd(logger),
		cmdrun.NewResultCmd(logger),
		cmdupload.NewCmd(logger),
		cmdversion.NewCmd(),
		cmdcompletion.NewCmd(),
	)

	return root
}

// Root returns the root command for tools like doc generators.
func Root() *cobra.Command {
	return NewRootCmd(newLogger())
}

// NewRoot builds the root command and returns it together with the logger.
// Use this in main so the same logger can be used for catch-all error printing.
func NewRoot() (*cobra.Command, *log.Logger) {
	logger := newLogger()
	return NewRootCmd(logger), logger
}
