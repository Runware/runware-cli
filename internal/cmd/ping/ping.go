package ping

import (
	"log/slog"
	"time"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

type pingResult struct {
	Status    string `json:"status" yaml:"status"`
	LatencyMs int64  `json:"latency_ms" yaml:"latency_ms"`
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
			t, err := cmdutil.NewTransport(cmd, slog.New(logger))
			if err != nil {
				return err
			}
			defer t.Close() //nolint:errcheck
			client := api.NewClient(t, slog.New(logger))

			start := time.Now()
			_, err = client.Ping(cmd.Context())
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), pingResult{
				Status:    "ok",
				LatencyMs: time.Since(start).Milliseconds(),
			})
		},
	}
}
