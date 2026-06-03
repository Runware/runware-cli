package main

import (
	"os"

	"github.com/runware/runware-cli/internal/cmd"
	"github.com/runware/runware-cli/internal/cmdutil"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.SetVersionInfo(version, commit, date)
	rootCmd, logger := cmd.NewRoot()
	if err := rootCmd.Execute(); err != nil {
		cmdutil.PrintError(logger, err)
		os.Exit(1)
	}
}
