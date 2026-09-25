package serverless

import (
	"context"
	"errors"
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

func waitTimeoutErr(err error, subject string, timeout time.Duration) error {
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if timeout > 0 {
		return fmt.Errorf("timed out waiting for %s after %s", subject, timeout)
	}
	return fmt.Errorf("timed out waiting for %s", subject)
}

func waitForApp(ctx context.Context, client *serverlessapi.Client, appID string, interval, timeout time.Duration) (*serverlessapi.App, error) {
	waitCtx, cancel := waitContext(ctx, timeout)
	defer cancel()

	app, err := client.WaitApp(waitCtx, appID, interval)
	return app, waitTimeoutErr(err, "application "+appID, timeout)
}
