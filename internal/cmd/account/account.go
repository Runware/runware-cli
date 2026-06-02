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

			data := map[string]any{
				"balance":  result.Balance,
				"today":    result.Usage.Today,
				"last_7d":  result.Usage.Last7Days,
				"last_30d": result.Usage.Last30Days,
				"total":    result.Usage.Total,
			}

			return output.Print(cmdutil.FormatFor(cmd), data, &output.Table{
				Headers: []string{"Period", "Credits", "Requests"},
				Rows: [][]any{
					{"Balance", fmt.Sprintf("%.5f", result.Balance), "—"},
					{"Today", fmt.Sprintf("%.5f", result.Usage.Today.Credits), result.Usage.Today.Requests},
					{"Last 7 days", fmt.Sprintf("%.5f", result.Usage.Last7Days.Credits), result.Usage.Last7Days.Requests},
					{"Last 30 days", fmt.Sprintf("%.5f", result.Usage.Last30Days.Credits), result.Usage.Last30Days.Requests},
					{"Total", fmt.Sprintf("%.5f", result.Usage.Total.Credits), result.Usage.Total.Requests},
				},
			})
		},
	}
}
