package auth

import (
	"log/slog"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

type authStatusResult struct {
	APIKey string `json:"api_key"`
	Status string `json:"status"`
}

func (r authStatusResult) Headers() []string {
	return []string{"Field", "Value"}
}

func (r authStatusResult) Rows() [][]any {
	return [][]any{
		{"API Key", r.APIKey},
		{"Status", r.Status},
	}
}

func newStatusCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current auth state",
		Example: `  # show current auth state
  runware auth status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := config.GetAPIKey()

			status := "not configured"
			maskedKey := "none"

			if key != "" {
				maskedKey = config.MaskKey(key)

				spin := cmdutil.NewSpinner("Checking auth status...")
				spin.Start()

				t, err := cmdutil.NewTransport(cmd, slog.New(logger))
				if err != nil {
					spin.Stop()
					status = "unreachable"
				} else {
					defer t.Close() //nolint:errcheck
					client := api.NewClient(t, slog.New(logger))
					_, err = client.Ping(cmd.Context())
					spin.Stop()
					switch {
					case err == nil:
						status = "valid"
					case transport.IsAuthError(err):
						status = "invalid"
					default:
						status = "unreachable"
					}
				}
			}

			return output.Print(cmdutil.FormatFor(cmd), authStatusResult{
				APIKey: maskedKey,
				Status: status,
			})
		},
	}
}
