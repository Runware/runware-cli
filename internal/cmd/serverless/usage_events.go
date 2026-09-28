package serverless

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

// usageEventFlags is the flag set of serverless usage events.
type usageEventFlags struct {
	app    string
	from   string
	to     string
	limit  int
	cursor string
}

func newUsageEventsCmd(logger *log.Logger) *cobra.Command {
	var flags usageEventFlags

	cmd := &cobra.Command{
		Use:   "events",
		Short: "List usage events",
		Long: `List worker state transitions for the authenticated organization, newest first.

Each row is one ledger entry. --from is inclusive and --to is exclusive on the
time the transition happened. There is no default window: omit both to read
the latest page of the ledger. Spend over a window is 'serverless usage'.

Price/s is the catalog rate per GPU-second at that moment, including time a
reservation covered. Coverage itself is on the usage summary.

Replay a cursor with the same --app, --from, --to, and --limit.`,
		Example: `  # the latest page of the ledger
  runware serverless usage events

  # one app over a UTC day
  runware serverless usage events --app my-app --from 2026-09-01 --to 2026-09-02

  # page through results
  runware serverless usage events --limit 50 --cursor <nextCursor>`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			params, err := usageEventParamsFromFlags(flags)
			if err != nil {
				return err
			}

			spin := cmdutil.NewSpinner("Fetching usage events...")
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.ListUsageEvents(cmd.Context(), params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printPage(cmdutil.FormatFor(cmd), page, usageEventsResult(page.Data), cmd.ErrOrStderr(), extraUsageEventCursorFlags(flags))
		},
	}

	cmd.Flags().StringVar(&flags.app, "app", "", "Report only this app's transitions")
	cmd.Flags().StringVar(&flags.from, "from", "", "Inclusive start on event time (RFC 3339 or YYYY-MM-DD UTC)")
	cmd.Flags().StringVar(&flags.to, "to", "", "Exclusive end on event time (RFC 3339 or YYYY-MM-DD UTC)")
	cmd.Flags().IntVar(&flags.limit, "limit", 0, "Maximum number of events to return (1-100)")
	cmd.Flags().StringVar(&flags.cursor, "cursor", "", "Pagination cursor from a previous nextCursor (reuse the same --app/--from/--to/--limit)")
	return cmd
}

// usageEventParamsFromFlags maps the events flags onto GET /v1/usage.
// An empty flag set returns nil so the request carries no query string.
func usageEventParamsFromFlags(flags usageEventFlags) (*serverlessapi.ListUsageEventsParams, error) {
	if err := validateListLimit(flags.limit); err != nil {
		return nil, err
	}
	if flags.app != "" && strings.TrimSpace(flags.app) == "" {
		return nil, fmt.Errorf("invalid --app %q", flags.app)
	}
	from, err := parseUsageTime("--from", flags.from)
	if err != nil {
		return nil, err
	}
	to, err := parseUsageTime("--to", flags.to)
	if err != nil {
		return nil, err
	}
	if from != nil && to != nil && !from.Before(*to) {
		return nil, fmt.Errorf("--from must be before --to")
	}

	params := &serverlessapi.ListUsageEventsParams{}
	params.Limit, params.Cursor = listPageParams(flags.limit, flags.cursor)
	if flags.app != "" {
		app := flags.app
		params.AppId = &app
	}
	params.From = from
	params.To = to
	if params.Limit == nil && params.Cursor == nil && params.AppId == nil && params.From == nil && params.To == nil {
		return nil, nil
	}
	return params, nil
}

func extraUsageEventCursorFlags(flags usageEventFlags) string {
	parts := appendFlag(nil, "--app", flags.app)
	parts = appendFlag(parts, "--from", flags.from)
	parts = appendFlag(parts, "--to", flags.to)
	if flags.limit > 0 {
		parts = appendFlag(parts, "--limit", fmt.Sprint(flags.limit))
	}
	return strings.Join(parts, " ")
}

// usageEventsResult renders a page of usage events as a table. JSON and YAML
// print the API page, not this view.
type usageEventsResult []serverlessapi.UsageEvent

func (r usageEventsResult) Headers() []string {
	return []string{colTime, colApp, colWorker, colEvent, colGPUs, colGPUType, colPricePerGPU}
}

func (r usageEventsResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		e := &r[i]
		gpuType := ""
		if e.GpuType != nil {
			gpuType = *e.GpuType
		}
		price := ""
		if e.PricePerSecond != nil {
			price = formatMoney(*e.PricePerSecond)
		}
		rows[i] = []any{
			e.OccurredAt.UTC().Format(time.RFC3339),
			e.AppId,
			e.WorkerId.String(),
			string(e.EventType),
			e.GpuCount,
			gpuType,
			price,
		}
	}
	return rows
}
