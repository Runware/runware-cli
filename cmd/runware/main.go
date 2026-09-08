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

// run executes the root command under a context that SIGINT and SIGTERM
// cancel, so long-running commands stop cleanly, and returns the exit status.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Once the first signal has cancelled the context, hand the signals back so
	// a second Ctrl-C kills a command that does not stop on its own.
	go func() {
		<-ctx.Done()
		stop()
	}()

	rootCmd, logger := cmd.NewRoot()
	err := rootCmd.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return exitInterrupted
	}
	cmdutil.PrintError(logger, cmdutil.FormatFor(rootCmd), err)
	return 1
}
