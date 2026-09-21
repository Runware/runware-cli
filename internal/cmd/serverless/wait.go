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

func waitForApp(ctx context.Context, client *serverlessapi.Client, appID string, interval, timeout time.Duration) (*serverlessapi.App, error) {
	waitCtx, cancel := waitContext(ctx, timeout)
	defer cancel()

	app, err := client.WaitApp(waitCtx, appID, interval)
	if err == nil {
		return app, nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		if timeout > 0 {
			return nil, fmt.Errorf("timed out waiting for application %s after %s", appID, timeout)
		}
		return nil, fmt.Errorf("timed out waiting for application %s", appID)
	}
	return nil, err
}
