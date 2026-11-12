package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/runware/runware-cli/internal/buildinfo"
	"github.com/runware/runware-cli/internal/cmd"
	"github.com/runware/runware-cli/internal/cmdutil"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// exitInterrupted is the conventional status for a run stopped by SIGINT.
const exitInterrupted = 130

func main() {
	buildinfo.Set(version, commit, date)
	os.Exit(run())
}

// run executes the root command for the process arguments.
func run() int {
	return runArgs(os.Args[1:])
}

// runArgs executes the root command for args under a context that SIGINT and
// SIGTERM cancel, so long-running commands stop cleanly, and returns the exit
// status. A refused command returns 1, which is what a deploy step in CI reads.
func runArgs(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Once the first signal has canceled the context, hand the signals back so
	// a second Ctrl-C kills a command that does not stop on its own.
	go func() {
		<-ctx.Done()
		stop()
	}()

	rootCmd, logger := cmd.NewRoot()
	rootCmd.SetArgs(args)
	err := rootCmd.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		// The status alone reports an interrupt, unless the error carries
		// what the user needs to resume the work it cut short.
		if cmdutil.IsResumable(err) {
			cmdutil.PrintError(logger, cmdutil.FormatFor(rootCmd), err)
		}
		return exitInterrupted
	}
	cmdutil.PrintError(logger, cmdutil.FormatFor(rootCmd), err)
	return 1
}
