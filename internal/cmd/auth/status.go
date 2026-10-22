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

// credentialStatus is what auth status reports about the configured API key.
type credentialStatus string

const (
	statusNotConfigured credentialStatus = "not configured"
	statusValid         credentialStatus = "valid"
	statusInvalid       credentialStatus = "invalid"
	statusUnreachable   credentialStatus = "unreachable"
	statusError         credentialStatus = "error"
)

type authStatusResult struct {
	APIKey string           `json:"api_key"`
	Status credentialStatus `json:"status"`
}

func (r authStatusResult) Headers() []string {
	return []string{"Field", "Value"}
}

func (r authStatusResult) Rows() [][]any {
	return [][]any{
		{"API Key", r.APIKey},
		{"Status", string(r.Status)},
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

			status := statusNotConfigured
			maskedKey := "none"

			if key != "" {
				maskedKey = config.MaskKey(key)

				spin := cmdutil.NewSpinner("Checking auth status...")
				spin.Start()
				err := verifyCredentials(cmd, slog.New(logger))
				spin.Stop()

				if ctxErr := cmd.Context().Err(); ctxErr != nil {
					return ctxErr
				}
				if err != nil {
					logger.Debug("credential check failed", "err", err)
				}
				status = classifyCredentialError(err)
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

func classifyCredentialError(err error) credentialStatus {
	switch {
	case err == nil:
		return statusValid
	case transport.IsAuthError(err):
		return statusInvalid
	case transport.IsConnectionError(err):
		return statusUnreachable
	default:
		return statusError
	}
}
