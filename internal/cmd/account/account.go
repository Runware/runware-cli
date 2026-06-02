package account

import (
	"context"
	"fmt"

	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Account information and credits",
		Example: `  # show current credit balance
  runware account credits`,
	}
	cmd.AddCommand(newCreditsCmd())
	return cmd
}

func newCreditsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "credits",
		Short: "Show current credit balance",
		RunE: func(cmd *cobra.Command, args []string) error {
			key := config.GetAPIKey()
			if key == "" {
				output.Error("No API key configured. Run 'runware auth login' to authenticate.")
				return api.ErrNoAPIKey
			}

			verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")
			client := api.NewClient(key, config.GetBaseURL(), verbose)
			result, err := client.AccountDetails(context.Background())
			if err != nil {
				if api.IsAuthError(err) {
					output.Error("Authentication failed. Run 'runware auth login' to set your API key.")
				}
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

type creditsResult struct {
	Balance float64         `json:"balance"`
	Today   api.UsagePeriod `json:"today"`
	Last7d  api.UsagePeriod `json:"last_7d"`
	Last30d api.UsagePeriod `json:"last_30d"`
	Total   api.UsagePeriod `json:"total"`
}

func (r creditsResult) Headers() []string { return []string{"Period", "Credits", "Requests"} }
func (r creditsResult) Rows() [][]any {
	return [][]any{
		{"Balance", fmt.Sprintf("%.5f", r.Balance), "—"},
		{"Today", fmt.Sprintf("%.5f", r.Today.Credits), r.Today.Requests},
		{"Last 7 days", fmt.Sprintf("%.5f", r.Last7d.Credits), r.Last7d.Requests},
		{"Last 30 days", fmt.Sprintf("%.5f", r.Last30d.Credits), r.Last30d.Requests},
		{"Total", fmt.Sprintf("%.5f", r.Total.Credits), r.Total.Requests},
	}
}
