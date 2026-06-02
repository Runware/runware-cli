package auth

import (
	"context"
	"log/slog"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
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
				client := api.NewClient(key, config.GetBaseURL(), slog.New(logger))
				_, err := client.Ping(context.Background())
				if err != nil {
					status = "invalid"
				} else {
					status = "valid"
				}
			}

			return output.Print(cmdutil.FormatFor(cmd), authStatusResult{
				APIKey: maskedKey,
				Status: status,
			})
		},
	}
}
