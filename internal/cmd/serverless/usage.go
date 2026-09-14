package serverless

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// usageRanges lists the accepted --for values, in the order they are documented.
const usageRanges = "today, yesterday, this-month, or last-month"

// usageDimensions lists the accepted --group-by values, in the order they are documented.
const usageDimensions = "app, gpuType, day, or coverage"

// usageRulesLong is the help text both usage commands share.
const usageRulesLong = `The window is half-open (--from inclusive, --to exclusive), at most 31 days,
and clipped: a worker running across a boundary contributes only the part
inside it. --for cannot be combined with --from or --to.

Billable time runs from loading through ready, draining and stopping; pending,
pulling and stopped do not bill, and busy is queue occupancy rather than a
lifecycle transition. GPU time is summed per GPU, so four GPUs for one second
is four seconds. PAYG spend is provisional and not a settled charge; the
PAYG-equivalent value prices the same time at the catalogue rate whatever
covered it, so the difference is what reserved capacity saved.

Grouping by day splits a span at UTC midnight. The table ends with a Total row
and prints the resolved window to stderr; json and yaml output the API payload.`

// usageFlags is the flag set shared by serverless usage and apps usage.
type usageFlags struct {
	from     string
	to       string
	forRange string
	gpuType  string
	groupBy  []string
}

func newUsageCmd(logger *log.Logger) *cobra.Command {
	var flags usageFlags

	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Show account-wide usage and cost",
		Long: `Show GPU time and spend for the authenticated organisation over a time window.

` + usageRulesLong,
		Example: `  # last 24 hours, account-wide
  runware serverless usage

  # a UTC calendar range
  runware serverless usage --for this-month

  # an explicit window: start inclusive, end exclusive
  runware serverless usage --from 2026-09-01 --to 2026-10-01

  # spend per app per day
  runware serverless usage --for last-month --group-by app,day

  # what reserved capacity covered, by GPU type
  runware serverless usage --group-by gpuType,coverage

  # export
  runware serverless usage --for last-month --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUsage(cmd, logger, "", flags)
		},
	}

	addUsageFlags(cmd, &flags)
	return cmd
}

func newAppsUsageCmd(logger *log.Logger) *cobra.Command {
	var flags usageFlags

	cmd := &cobra.Command{
		Use:   "usage <appId>",
		Short: "Show usage and cost for a serverless application",
		Long: `Show GPU time and spend for one application over a time window.

This is the account-wide report filtered to one appId. The filter narrows what
is reported, not what is measured: commitment coverage depends on every app's
concurrent GPUs, so a per-app split between commitment and PAYG reflects the
organisation-wide allocation.

` + usageRulesLong,
		Example: `  # last 24 hours for one app
  runware serverless apps usage my-app

  # this month, per day
  runware serverless apps usage my-app --for this-month --group-by day

  # export
  runware serverless apps usage my-app --for last-month --format json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateUsageAppID(args[0]); err != nil {
				return err
			}
			return runUsage(cmd, logger, args[0], flags)
		},
	}

	addUsageFlags(cmd, &flags)
	return cmd
}

func addUsageFlags(cmd *cobra.Command, f *usageFlags) {
	cmd.Flags().StringVar(&f.from, "from", "", "Inclusive start of the window (RFC 3339 or YYYY-MM-DD UTC; default 24h before --to)")
	cmd.Flags().StringVar(&f.to, "to", "", "Exclusive end of the window (RFC 3339 or YYYY-MM-DD UTC; default now)")
	cmd.Flags().StringVar(&f.forRange, "for", "", "UTC calendar range instead of --from/--to ("+usageRanges+")")
	cmd.Flags().StringVar(&f.gpuType, "gpu-type", "", "Report only this GPU type (see 'serverless gpus')")
	cmd.Flags().StringSliceVar(&f.groupBy, "group-by", nil, "Group buckets by dimension, comma-separated ("+usageDimensions+")")
}

func runUsage(cmd *cobra.Command, logger *log.Logger, appID string, flags usageFlags) error {
	params, err := usageParamsFromFlags(flags, appID, time.Now())
	if err != nil {
		return err
	}

	spin := cmdutil.NewSpinner("Fetching usage...")
	spin.Start()

	client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
	summary, err := client.GetUsageSummary(cmd.Context(), params)
	if err != nil {
		spin.Stop()
		return err
	}
	spin.Stop()

	var dims []serverlessapi.UsageDimension
	if params.GroupBy != nil {
		dims = *params.GroupBy
	}
	return printUsage(cmdutil.FormatFor(cmd), summary, dims, cmd.ErrOrStderr())
}

// usageParamsFromFlags maps the usage flags 1:1 onto the getUsageSummary query.
// now anchors --for so the derived window is deterministic in tests.
func usageParamsFromFlags(flags usageFlags, appID string, now time.Time) (*serverlessapi.GetUsageSummaryParams, error) {
	params := &serverlessapi.GetUsageSummaryParams{}

	if flags.forRange != "" {
		if flags.from != "" || flags.to != "" {
			return nil, fmt.Errorf("--for cannot be combined with --from or --to")
		}
		from, to, err := usageRange(flags.forRange, now)
		if err != nil {
			return nil, err
		}
		params.From = &from
		params.To = to
	} else {
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
		params.From = from
		params.To = to
	}

	if appID != "" {
		params.AppId = &appID
	}
	if flags.gpuType != "" {
		gpuType := flags.gpuType
		params.GpuType = &gpuType
	}

	groupBy, err := parseUsageDimensions(flags.groupBy)
	if err != nil {
		return nil, err
	}
	if len(groupBy) > 0 {
		params.GroupBy = &groupBy
	}
	return params, nil
}

// validateUsageAppID rejects a blank appId. An empty one drops the filter, and
// the per-app command then reports the whole organisation.
func validateUsageAppID(appID string) error {
	if strings.TrimSpace(appID) == "" {
		return fmt.Errorf("appId is required")
	}
	return nil
}

// usageRange resolves a --for name to a UTC calendar window anchored at now.
// Ranges that end now leave the upper bound unset so the API resolves "now"
// itself and reports it back as the window end.
func usageRange(name string, now time.Time) (time.Time, *time.Time, error) {
	now = now.UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	switch name {
	case "today":
		return day, nil, nil
	case "yesterday":
		return day.AddDate(0, 0, -1), &day, nil
	case "this-month":
		return month, nil, nil
	case "last-month":
		return month.AddDate(0, -1, 0), &month, nil
	default:
		return time.Time{}, nil, fmt.Errorf("invalid --for %q (want %s)", name, usageRanges)
	}
}

// parseUsageTime reads a --from/--to value as RFC 3339, or as a calendar date
// read as 00:00 UTC. The zero timestamp is rejected locally because the API
// answers it with 422 rather than treating it as unset.
func parseUsageTime(flag, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t, err = time.Parse(time.DateOnly, value)
		if err != nil {
			return nil, fmt.Errorf("invalid %s %q (want RFC 3339 or YYYY-MM-DD)", flag, value)
		}
	}
	if t.IsZero() {
		return nil, fmt.Errorf("%s must not be the zero timestamp", flag)
	}
	t = t.UTC()
	return &t, nil
}

// parseUsageDimensions validates --group-by values in the order given and
// rejects repeats, which the API refuses as 422.
func parseUsageDimensions(values []string) ([]serverlessapi.UsageDimension, error) {
	dims := make([]serverlessapi.UsageDimension, 0, len(values))
	seen := make(map[serverlessapi.UsageDimension]struct{}, len(values))
	for _, value := range values {
		dim, err := parseValidFlag[serverlessapi.UsageDimension]("--group-by", value, usageDimensions)
		if err != nil {
			return nil, err
		}
		if dim == nil {
			return nil, fmt.Errorf("invalid --group-by %q (want %s)", value, usageDimensions)
		}
		if _, dup := seen[*dim]; dup {
			return nil, fmt.Errorf("--group-by %q given more than once", value)
		}
		seen[*dim] = struct{}{}
		dims = append(dims, *dim)
	}
	return dims, nil
}

// printUsage prints a usage summary. The window goes to errOut so a defaulted
// --from/--to is visible.
func printUsage(format output.Format, summary *serverlessapi.UsageSummary, dims []serverlessapi.UsageDimension, errOut io.Writer) error {
	switch format {
	case output.FormatJSON, output.FormatYAML:
		return output.Print(format, summary)
	default:
		if err := output.Print(format, usageResult{summary: summary, dims: dims}); err != nil {
			return err
		}
		_, err := fmt.Fprintf(errOut, "\n%s\n", usageWindowLine(summary))
		return err
	}
}

// usageWindowLine states the window the API resolved and the instant the
// figures describe.
func usageWindowLine(summary *serverlessapi.UsageSummary) string {
	return fmt.Sprintf("Window: %s to %s (end exclusive), calculated at %s",
		summary.From.UTC().Format(time.RFC3339),
		summary.To.UTC().Format(time.RFC3339),
		summary.CalculatedAt.UTC().Format(time.RFC3339))
}

// usageDimensionOrder fixes the dimension column order whatever order
// --group-by named them in.
var usageDimensionOrder = []serverlessapi.UsageDimension{
	serverlessapi.UsageDimensionApp,
	serverlessapi.UsageDimensionGpuType,
	serverlessapi.UsageDimensionDay,
	serverlessapi.UsageDimensionCoverage,
}

// usageResult renders a usage summary as a table. An ungrouped summary is the
// total alone, since its single bucket equals it.
type usageResult struct {
	summary *serverlessapi.UsageSummary
	dims    []serverlessapi.UsageDimension
}

func (r usageResult) columns() []serverlessapi.UsageDimension {
	requested := make(map[serverlessapi.UsageDimension]struct{}, len(r.dims))
	for _, d := range r.dims {
		requested[d] = struct{}{}
	}
	cols := make([]serverlessapi.UsageDimension, 0, len(requested))
	for _, d := range usageDimensionOrder {
		if _, ok := requested[d]; ok {
			cols = append(cols, d)
		}
	}
	return cols
}

func (r usageResult) Headers() []string {
	cols := r.columns()
	headers := make([]string, 0, len(cols)+3)
	for _, d := range cols {
		headers = append(headers, usageDimensionHeader(d))
	}
	return append(headers, colGPUTime, colPAYGSpend, colPAYGValue)
}

func (r usageResult) Rows() [][]any {
	cols := r.columns()
	if len(cols) == 0 {
		return [][]any{appendUsageAmounts(nil, &r.summary.Total)}
	}

	rows := make([][]any, 0, len(r.summary.Buckets)+1)
	for i := range r.summary.Buckets {
		b := &r.summary.Buckets[i]
		row := make([]any, 0, len(cols)+3)
		for _, d := range cols {
			row = append(row, usageDimensionValue(d, b))
		}
		rows = append(rows, appendUsageAmounts(row, b))
	}

	total := make([]any, 0, len(cols)+3)
	total = append(total, "Total")
	for range cols[1:] {
		total = append(total, "")
	}
	return append(rows, appendUsageAmounts(total, &r.summary.Total))
}

func appendUsageAmounts(row []any, b *serverlessapi.UsageBucket) []any {
	return append(row, formatGPUTime(b.GpuMilliseconds), formatMoney(b.PaygSpend), formatMoney(b.PaygEquivalentValue))
}

func usageDimensionHeader(d serverlessapi.UsageDimension) string {
	switch d {
	case serverlessapi.UsageDimensionApp:
		return colApp
	case serverlessapi.UsageDimensionGpuType:
		return colGPUType
	case serverlessapi.UsageDimensionDay:
		return colDay
	case serverlessapi.UsageDimensionCoverage:
		return colCoverage
	default:
		return string(d)
	}
}

// usageDimensionValue reads the bucket field a dimension groups on.
func usageDimensionValue(d serverlessapi.UsageDimension, b *serverlessapi.UsageBucket) string {
	switch d {
	case serverlessapi.UsageDimensionApp:
		return formatOptionalString(b.AppId)
	case serverlessapi.UsageDimensionGpuType:
		return formatOptionalString(b.GpuType)
	case serverlessapi.UsageDimensionDay:
		if b.Day == nil {
			return ""
		}
		return b.Day.Format(time.DateOnly)
	case serverlessapi.UsageDimensionCoverage:
		if b.Coverage == nil {
			return ""
		}
		return string(*b.Coverage)
	default:
		return ""
	}
}

// Millisecond spans of the units formatGPUTime prints.
const (
	msPerSecond = 1000
	msPerMinute = 60 * msPerSecond
	msPerHour   = 60 * msPerMinute
)

// formatGPUTime renders GPU-milliseconds as an exact duration (e.g. 20m35s,
// 1h2m3.5s), so nothing is rounded away from a billed figure. Hours come from
// the integer rather than time.Duration, which counts nanoseconds and wraps
// negative past 292 GPU-years: time is summed per GPU, so one 31-day window
// reaches that at about 3,400 concurrent GPUs.
func formatGPUTime(ms int64) string {
	if ms < msPerHour {
		return (time.Duration(ms) * time.Millisecond).String()
	}
	hours, rem := ms/msPerHour, ms%msPerHour
	return fmt.Sprintf("%dh%dm%s", hours, rem/msPerMinute, formatGPUSeconds(rem%msPerMinute))
}

// formatGPUSeconds renders sub-minute milliseconds the way time.Duration does:
// whole seconds bare, a fraction with its trailing zeros trimmed.
func formatGPUSeconds(ms int64) string {
	secs, frac := ms/msPerSecond, ms%msPerSecond
	if frac == 0 {
		return fmt.Sprintf("%ds", secs)
	}
	return fmt.Sprintf("%d.%ss", secs, strings.TrimRight(fmt.Sprintf("%03d", frac), "0"))
}

func formatMoney(m serverlessapi.MoneyAmount) string {
	return m.Amount + " " + string(m.Currency)
}
