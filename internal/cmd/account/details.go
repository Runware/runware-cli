package account

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// accountDetailsResult wraps the full API response for display purposes.
// JSON and YAML output the struct directly; the table renderer flattens
// everything into a two-column key/value layout.
type accountDetailsResult struct {
	OrganizationName string           `json:"organization_name"    yaml:"organization_name"`
	OrganizationUUID string           `json:"organization_uuid"    yaml:"organization_uuid"`
	AIRSource        string           `json:"air_source,omitempty" yaml:"air_source,omitempty"`
	Balance          float64          `json:"balance"              yaml:"balance"`
	Usage            api.AccountUsage `json:"usage"                yaml:"usage"`
	Team             []api.TeamMember `json:"team,omitempty"       yaml:"team,omitempty"`
	APIKeys          []api.APIKeyInfo `json:"api_keys,omitempty"   yaml:"api_keys,omitempty"`
}

func (r accountDetailsResult) Headers() []string {
	return []string{"Field", "Value"}
}

func (r accountDetailsResult) Rows() [][]any {
	rows := [][]any{
		{"Organization", r.OrganizationName},
		{"Org UUID", r.OrganizationUUID},
	}
	if r.AIRSource != "" {
		rows = append(rows, []any{"AIR Source", r.AIRSource})
	}

	// Balance + usage
	rows = append(rows,
		[]any{"Balance", fmt.Sprintf("%.5f", r.Balance)},
		[]any{"Usage Today (credits)", fmt.Sprintf("%.5f", r.Usage.Today.Credits)},
		[]any{"Usage Today (requests)", r.Usage.Today.Requests},
		[]any{"Usage Last 7d (credits)", fmt.Sprintf("%.5f", r.Usage.Last7Days.Credits)},
		[]any{"Usage Last 7d (requests)", r.Usage.Last7Days.Requests},
		[]any{"Usage Last 30d (credits)", fmt.Sprintf("%.5f", r.Usage.Last30Days.Credits)},
		[]any{"Usage Last 30d (requests)", r.Usage.Last30Days.Requests},
		[]any{"Usage Total (credits)", fmt.Sprintf("%.5f", r.Usage.Total.Credits)},
		[]any{"Usage Total (requests)", r.Usage.Total.Requests},
	)

	// Team members
	for i, m := range r.Team {
		joined := ""
		if !m.JoinedAt.IsZero() {
			joined = " joined " + m.JoinedAt.Format("2006-01-02")
		}
		rows = append(rows, []any{
			fmt.Sprintf("Team [%d]", i+1),
			fmt.Sprintf("%s <%s> [%s]%s", m.Name, m.Email, strings.Join(m.Roles, ", "), joined),
		})
	}

	// API keys
	for i, k := range r.APIKeys {
		status := "disabled"
		if k.Enabled {
			status = "enabled"
		}
		lastUsed := "never"
		if !k.LastUsedAt.IsZero() {
			lastUsed = k.LastUsedAt.Format("2006-01-02")
		}
		desc := ""
		if k.Description != "" {
			desc = " — " + k.Description
		}
		rows = append(rows, []any{
			fmt.Sprintf("API Key [%d]", i+1),
			fmt.Sprintf("%s %s%s (%s, %d requests, last used %s)",
				k.Name, k.APIKey, desc, status, k.Requests, lastUsed),
		})
	}

	return rows
}

func newDetailsCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "details",
		Short: "Show account details, team, API keys, and usage",
		Example: `  # show full account details
  runware account details`,
		RunE: func(cmd *cobra.Command, args []string) error {
			spin := cmdutil.NewSpinner("Fetching account details...")
			spin.Start()

			t, err := cmdutil.NewTransport(cmd, slog.New(logger))
			if err != nil {
				spin.Stop()
				return err
			}
			defer t.Close() //nolint:errcheck
			client := api.NewClient(t, slog.New(logger))

			result, err := client.AccountDetails(cmd.Context())
			if err != nil {
				spin.Stop()
				return err
			}

			spin.Stop()
			return output.Print(cmdutil.FormatFor(cmd), accountDetailsResult{
				OrganizationName: result.OrganizationName,
				OrganizationUUID: result.OrganizationUUID.String(),
				AIRSource:        result.AIRSource,
				Balance:          result.Balance,
				Usage:            result.Usage,
				Team:             result.Team,
				APIKeys:          result.APIKeys,
			})
		},
	}
}
