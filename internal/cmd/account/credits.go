package account

import (
	"fmt"
	"log/slog"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

type creditsResult struct {
	Balance float64         `json:"balance" yaml:"balance"`
	Today   api.UsagePeriod `json:"today" yaml:"today"`
	Last7d  api.UsagePeriod `json:"last_7d" yaml:"last_7d"`
	Last30d api.UsagePeriod `json:"last_30d" yaml:"last_30d"`
	Total   api.UsagePeriod `json:"total" yaml:"total"`
}

func (r creditsResult) Headers() []string {
	return []string{"Period", "Credits", "Requests"}
}

func (r creditsResult) Rows() [][]any {
	return [][]any{
		{"Balance", fmt.Sprintf("%.5f", r.Balance), "—"},
		{"Today", fmt.Sprintf("%.5f", r.Today.Credits), r.Today.Requests},
		{"Last 7 days", fmt.Sprintf("%.5f", r.Last7d.Credits), r.Last7d.Requests},
		{"Last 30 days", fmt.Sprintf("%.5f", r.Last30d.Credits), r.Last30d.Requests},
		{"Total", fmt.Sprintf("%.5f", r.Total.Credits), r.Total.Requests},
	}
}

func newCreditsCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "credits",
		Short: "Show current credit balance",
		RunE: func(cmd *cobra.Command, args []string) error {
			t := transport.TransportFromContext(cmd.Context())
			client := api.NewClient(t, slog.New(logger))

			result, err := client.AccountDetails(cmd.Context())
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), creditsResult{
				Balance: result.Balance,
				Today:   result.Usage.Today,
				Last7d:  result.Usage.Last7Days,
				Last30d: result.Usage.Last30Days,
				Total:   result.Usage.Total,
			})
		},
	}
}
