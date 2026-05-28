package cmd

import (
	"fmt"
	"os"

	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	flagFormat  string
	flagVerbose bool
	flagDebug   bool

	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// SetVersionInfo is called from main to inject build-time version info.
func SetVersionInfo(v, c, d string) {
	version = v
	commit = c
	date = d
}

var rootCmd = &cobra.Command{
	Use:   "runware",
	Short: "CLI tool for the Runware inference API",
	Long:  "A command-line tool for interacting with the Runware inference API.\nGenerate images, search models, manage your account, and more.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return config.Init()
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagFormat, "format", "", "Output format: table, json, yaml")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Show request/response details")
	rootCmd.PersistentFlags().BoolVar(&flagDebug, "debug", false, "Show full debug output")

	rootCmd.RegisterFlagCompletionFunc("format", func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{"table", "json", "yaml"}, cobra.ShellCompDirectiveNoFileComp
	})

	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(pingCmd)
	rootCmd.AddCommand(imageInferenceCmd)
	rootCmd.AddCommand(videoInferenceCmd)
	rootCmd.AddCommand(audioInferenceCmd)
	rootCmd.AddCommand(textInferenceCmd)
	rootCmd.AddCommand(modelSearchCmd)
	rootCmd.AddCommand(accountCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(presetCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(completionCmd)
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

// getFormat returns the output format, preferring the flag, then config default.
func getFormat() string {
	if flagFormat != "" {
		return flagFormat
	}
	return config.Get().Defaults.Format
}
