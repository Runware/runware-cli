package ping

import (
	"context"
	"log/slog"
	"time"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

type pingResult struct {
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
}

func (r pingResult) Headers() []string {
	return []string{"Status", "Latency (ms)"}
}

func (r pingResult) Rows() [][]any {
	return [][]any{{r.Status, r.LatencyMs}}
}

// NewCmd returns the ping command for checking API connectivity.
func NewCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "Check API connectivity",
		Example: `  # check API connectivity
  runware ping`,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := config.GetAPIKey()
			if key == "" {
				logger.Error("No API key configured. Run 'runware auth login' to authenticate.")
				return api.ErrNoAPIKey
			}

			client := api.NewClient(key, config.GetBaseURL(), slog.New(logger))
			start := time.Now()
			_, err := client.Ping(context.Background())
			if err != nil {
				if api.IsAuthError(err) {
					logger.Error("Authentication failed. Run 'runware auth login' to set your API key.")
					return err
				}
				logger.Error("Ping failed", "err", err)
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), pingResult{
				Status:    "ok",
				LatencyMs: time.Since(start).Milliseconds(),
			})
		},
	}
}
