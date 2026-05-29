package cmd

import (
	"context"
	"fmt"

	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "Account information and credits TEST",
}

var accountCreditsCmd = &cobra.Command{
	Use:   "credits",
	Short: "Show current credit balance",
	RunE: func(cmd *cobra.Command, args []string) error {
		key := config.GetAPIKey()
		if key == "" {
			output.Error("No API key configured. Run 'runware auth login' to authenticate.")
			return api.ErrNoAPIKey
		}

		client := api.NewClient(key, config.GetBaseURL(), flagVerbose)
		result, err := client.AccountDetails(context.Background())
		if err != nil {
			if api.IsAuthError(err) {
				output.Error("Authentication failed. Run 'runware auth login' to set your API key.")
			}
			return err
		}

		format := output.ParseFormat(getFormat())

		data := map[string]any{
			"balance":  result.Balance,
			"today":    result.Usage.Today,
			"last_7d":  result.Usage.Last7Days,
			"last_30d": result.Usage.Last30Days,
			"total":    result.Usage.Total,
		}

		return output.Print(format, data,
			[]any{"Period", "Credits", "Requests"},
			[][]any{
				{"Balance", fmt.Sprintf("%.5f", result.Balance), "—"},
				{"Today", fmt.Sprintf("%.5f", result.Usage.Today.Credits), result.Usage.Today.Requests},
				{"Last 7 days", fmt.Sprintf("%.5f", result.Usage.Last7Days.Credits), result.Usage.Last7Days.Requests},
				{"Last 30 days", fmt.Sprintf("%.5f", result.Usage.Last30Days.Credits), result.Usage.Last30Days.Requests},
				{"Total", fmt.Sprintf("%.5f", result.Usage.Total.Credits), result.Usage.Total.Requests},
			},
		)
	},
}

func init() {
	accountCmd.AddCommand(accountCreditsCmd)
}
