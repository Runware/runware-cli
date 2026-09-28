package serverless

import (
	"context"
	"fmt"
	"time"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/spf13/cobra"
)

func addAppWaitFlags(cmd *cobra.Command, wait *bool, timeout, pollInterval *time.Duration) {
	cmd.Flags().BoolVar(wait, "wait", false, "Poll until the application is active or failed")
	cmd.Flags().DurationVar(timeout, "timeout", 0, "Maximum time to wait (0 = no limit)")
	cmd.Flags().DurationVar(pollInterval, "poll-interval", 2*time.Second, "Polling interval when waiting for the application")
}

func waitContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, timeout)
}

func validateWaitFlags(cmd *cobra.Command, waiting bool, timeout time.Duration) error {
	if timeout < 0 {
		return fmt.Errorf("--timeout must not be negative")
	}
	if cmd.Flags().Changed("timeout") && !waiting {
		if cmd.Flags().Lookup("sync") != nil {
			return fmt.Errorf("--timeout requires --wait or --sync")
		}
		return fmt.Errorf("--timeout requires --wait")
	}
	return nil
}

// waitTimeoutErr rewrites err only when the wait context itself expired.
// A single slow request also surfaces as context.DeadlineExceeded via the
// HTTP client timeout, and that must not be reported as the wait budget.
func waitTimeoutErr(waitCtx context.Context, err error, subject string, timeout time.Duration) error {
	if err == nil || waitCtx.Err() != context.DeadlineExceeded {
		return err
	}
	return fmt.Errorf("timed out waiting for %s after %s", subject, timeout)
}

func waitForApp(ctx context.Context, client *serverlessapi.Client, appID string, interval, timeout time.Duration) (*serverlessapi.App, error) {
	waitCtx, cancel := waitContext(ctx, timeout)
	defer cancel()

	app, err := client.WaitApp(waitCtx, appID, interval)
	return app, waitTimeoutErr(waitCtx, err, "application "+appID, timeout)
}
