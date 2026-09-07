package serverless

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// newAppsCmd returns the "serverless apps" command group for managing
// deployed serverless applications.
func newAppsCmd(logger *log.Logger) *cobra.Command {
	cmd := stubGroup("apps", "Manage deployed serverless applications")
	cmd.Long = "Manage deployed serverless applications on the Runware platform."
	cmd.AddCommand(
		newAppsListCmd(logger),
		newAppsShowCmd(logger),
		newAppsEndpointsCmd(logger),
		newAppsInvokeCmd(logger),
		newAppsTasksCmd(logger),
		newAppsEnvCmd(logger),
		newAppsVersionsCmd(logger),
		newAppsBuildsCmd(logger),
		newAppsLogsCmd(),
		newAppsWorkersCmd(logger),
		newAppsScaleCmd(logger),
		newAppsUsageCmd(),
		newAppsStopCmd(logger),
		newAppsResumeCmd(logger),
		newAppsDeleteCmd(logger),
	)
	return cmd
}

func newAppsListCmd(logger *log.Logger) *cobra.Command {
	var (
		limit   int
		cursor  string
		status  string
		query   string
		gpuType string
		sort    string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List serverless applications",
		Example: `  # list all serverless applications
  runware serverless apps list

  # filter by status
  runware serverless apps list --status active

  # filter by name or ID substring
  runware serverless apps list --query demo --sort name

  # filter by GPU type
  runware serverless apps list --gpu-type h100 --status active

  # page through results
  runware serverless apps list --limit 20 --cursor <nextCursor>`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListLimit(limit); err != nil {
				return err
			}
			sortVal, err := parseAppSort(sort)
			if err != nil {
				return err
			}
			statusVal, err := parseAppStatus(status)
			if err != nil {
				return err
			}

			var params *serverlessapi.ListAppsParams
			if limit > 0 || cursor != "" || status != "" || query != "" || gpuType != "" || sort != "" {
				params = &serverlessapi.ListAppsParams{}
				params.Limit, params.Cursor = listPageParams(limit, cursor)
				params.Status = statusVal
				params.Sort = sortVal
				params.Q = optionalStringPtr(query)
				params.GpuType = optionalStringPtr(gpuType)
			}

			spin := cmdutil.NewSpinner("Fetching applications...")
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.ListApps(cmd.Context(), params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printPage(cmdutil.FormatFor(cmd), page, appsResult(page.Data), cmd.ErrOrStderr(), extraListCursorFlags(query, gpuType, sort, status))
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of applications to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor (reuse the same --query/--gpu-type/--sort/--status)")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (active, initializing, stopped, …)")
	cmd.Flags().StringVar(&query, "query", "", "Filter by substring on name or ID")
	cmd.Flags().StringVar(&gpuType, "gpu-type", "", "Filter by GPU type (see 'serverless gpus')")
	cmd.Flags().StringVar(&sort, "sort", "", "Sort order (createdAt (default), name, activity, or errorRate)")

	return cmd
}

func newAppsShowCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "show <appId>",
		Short: "Show details for a serverless application",
		Example: `  # show details for an application
  runware serverless apps show my-app`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching application %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			app, err := client.GetApp(cmd.Context(), id)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), appResult(*app))
		},
	}
}

func newAppsEndpointsCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		cursor string
	)

	cmd := &cobra.Command{
		Use:   "endpoints <appId>",
		Short: "List endpoints for a serverless application",
		Example: `  # list endpoints for an application
  runware serverless apps endpoints my-app

  # page through results
  runware serverless apps endpoints my-app --limit 20 --cursor <nextCursor>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListLimit(limit); err != nil {
				return err
			}
			id := args[0]
			var params *serverlessapi.ListEndpointsParams
			if limit > 0 || cursor != "" {
				params = &serverlessapi.ListEndpointsParams{}
				params.Limit, params.Cursor = listPageParams(limit, cursor)
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching endpoints for %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.ListEndpoints(cmd.Context(), id, params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printPage(cmdutil.FormatFor(cmd), page, endpointsResult(page.Data), cmd.ErrOrStderr(), "")
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of endpoints to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor")
	return cmd
}

func newAppsLogsCmd() *cobra.Command {
	cmd := stubLeaf(
		"logs <appId>",
		"Show logs for a serverless application",
		`  # show application logs (not available yet)
  runware serverless apps logs my-app`,
		cobra.ExactArgs(1),
	)
	cmd.Long = `Show application logs.

This command is not implemented yet. The log-query route exists but currently
answers 404 until a follow-up ADR; live tail is not supported.`
	return cmd
}

func newAppsWorkersCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		cursor string
		status string
	)

	cmd := &cobra.Command{
		Use:   "workers <appId>",
		Short: "List workers for a serverless application",
		Example: `  # list workers for an application
  runware serverless apps workers my-app

  # filter by status
  runware serverless apps workers my-app --status ready

  # page through results
  runware serverless apps workers my-app --limit 20 --cursor <nextCursor>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListLimit(limit); err != nil {
				return err
			}
			id := args[0]
			statusVal, err := parseWorkerStatus(status)
			if err != nil {
				return err
			}
			var params *serverlessapi.ListWorkersParams
			if limit > 0 || cursor != "" || status != "" {
				params = &serverlessapi.ListWorkersParams{}
				params.Limit, params.Cursor = listPageParams(limit, cursor)
				params.Status = statusVal
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching workers for %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.ListWorkers(cmd.Context(), id, params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printPage(cmdutil.FormatFor(cmd), page, workersResult(page.Data), cmd.ErrOrStderr(), extraStatusCursorFlag(status))
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of workers to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (ready, busy, pending, …)")
	return cmd
}

// validateListLimit checks --limit when set. Zero means unset (API default).
func validateListLimit(limit int) error {
	if limit == 0 {
		return nil
	}
	if limit < 1 || limit > 100 {
		return fmt.Errorf("--limit must be between 1 and 100")
	}
	return nil
}

// validListFlag is a generated enum used as a list-command filter.
type validListFlag interface {
	~string
	Valid() bool
}

func parseValidFlag[T validListFlag](flag, value, want string) (*T, error) {
	if value == "" {
		return nil, nil
	}
	v := T(value)
	if !v.Valid() {
		return nil, fmt.Errorf("invalid %s %q (want %s)", flag, value, want)
	}
	return &v, nil
}

func parseAppSort(sort string) (*serverlessapi.AppSort, error) {
	return parseValidFlag[serverlessapi.AppSort]("--sort", sort, "createdAt, name, activity, or errorRate")
}

func parseAppStatus(status string) (*serverlessapi.AppStatus, error) {
	return parseValidFlag[serverlessapi.AppStatus]("--status", status, "active, initializing, stopping, stopped, deleting, deleted, or failed")
}

func parseWorkerStatus(status string) (*serverlessapi.WorkerStatus, error) {
	return parseValidFlag[serverlessapi.WorkerStatus]("--status", status, "pending, pulling, loading, ready, busy, unhealthy, draining, stopping, or stopped")
}

// extraListCursorFlags repeats the apps-list filter flags a next-page --cursor is bound to.
func extraListCursorFlags(query, gpuType, sort, status string) string {
	parts := make([]string, 0, 4)
	parts = appendFlag(parts, "--query", query)
	parts = appendFlag(parts, "--gpu-type", gpuType)
	parts = appendFlag(parts, "--sort", sort)
	parts = appendFlag(parts, "--status", status)
	return strings.Join(parts, " ")
}

// extraStatusCursorFlag formats --status for a next-page --cursor hint.
func extraStatusCursorFlag(value string) string {
	return strings.Join(appendFlag(nil, "--status", value), " ")
}

func appendFlag(parts []string, name, value string) []string {
	if value == "" {
		return parts
	}
	if !isBareFlagValue(value) {
		return append(parts, name+" "+strconv.Quote(value))
	}
	return append(parts, name+" "+value)
}

func isBareFlagValue(value string) bool {
	for i := range len(value) {
		c := value[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == '.' || c == ':' || c == '/':
		default:
			return false
		}
	}
	return true
}

// listPageParams builds shared limit/cursor query values for cursor-paginated list commands.
func listPageParams(limit int, cursor string) (*serverlessapi.Limit, *serverlessapi.Cursor) {
	var (
		limitOut  *serverlessapi.Limit
		cursorOut *serverlessapi.Cursor
	)
	if limit > 0 {
		l := serverlessapi.Limit(limit)
		limitOut = &l
	}
	if cursor != "" {
		c := cursor
		cursorOut = &c
	}
	return limitOut, cursorOut
}

func newAppsUsageCmd() *cobra.Command {
	cmd := stubLeaf(
		"usage <appId>",
		"Show usage and cost for a serverless application",
		`  # show usage for an application (not available yet)
  runware serverless apps usage my-app`,
		cobra.ExactArgs(1),
	)
	cmd.Long = `Show an aggregated usage and cost report for one application.

This command is not implemented yet. Billing rollups are not in the API, so
there is no per-app report to list. When the report API exists, this will be
the account-wide usage command scoped to one appId.`
	return cmd
}
