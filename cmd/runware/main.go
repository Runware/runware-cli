package main

import (
	"os"

	"github.com/runware/runware-cli/internal/cmd"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.SetVersionInfo(version, commit, date)
	if err := cmd.Root().Execute(); err != nil {
		os.Exit(1)
	}
}
