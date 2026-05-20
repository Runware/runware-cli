package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

var pingCmd = &cobra.Command{
	Use:   "ping",
	Short: "Check API connectivity",
	RunE: func(cmd *cobra.Command, args []string) error {
		key := config.GetAPIKey()
		if key == "" {
			output.Error("No API key configured. Run 'runware auth login' to authenticate.")
			return api.ErrNoAPIKey
		}

		client := api.NewClient(key, config.GetBaseURL(), flagVerbose)

		start := time.Now()
		_, err := client.Ping(context.Background())
		elapsed := time.Since(start)

		if err != nil {
			if api.IsAuthError(err) {
				output.Error("Authentication failed. Run 'runware auth login' to set your API key.")
				return err
			}
			output.Error(fmt.Sprintf("Ping failed: %s", err))
			return err
		}

		env := config.GetEnvironment()
		latencyMs := elapsed.Milliseconds()

		format := output.ParseFormat(getFormat())
		data := map[string]any{
			"status":      "ok",
			"latency_ms":  latencyMs,
			"environment": env,
		}

		if format == output.FormatTable {
			output.Success(fmt.Sprintf("Runware API: OK (%dms) — environment: %s", latencyMs, env))
			return nil
		}

		return output.Print(format, data, nil, nil)
	},
}
