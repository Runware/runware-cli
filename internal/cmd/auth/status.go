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
				err := verifyCredentials(cmd, slog.New(logger))
				spin.Stop()

				switch {
				case err == nil:
					status = "valid"
				case transport.IsAuthError(err):
					status = "invalid"
				case transport.IsConnectionError(err):
					status = "unreachable"
				default:
					status = "error"
				}
				if err != nil {
					logger.Debug("credential check failed", "err", err)
				}
			}

			return output.Print(cmdutil.FormatFor(cmd), authStatusResult{
				APIKey: maskedKey,
				Status: status,
			})
		},
	}
}

func verifyCredentials(cmd *cobra.Command, logger *slog.Logger) error {
	t, err := cmdutil.NewTransport(cmd, logger)
	if err != nil {
		return err
	}
	defer t.Close() //nolint:errcheck
	client := api.NewClient(t, logger)
	_, err = client.Ping(cmd.Context())
	return err
}
