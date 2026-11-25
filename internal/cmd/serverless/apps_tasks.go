package serverless

import (
	"fmt"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

func newAppsTasksCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		cursor string
		status string
	)

	cmd := &cobra.Command{
		Use:   "tasks <appId>",
		Short: "List and inspect application tasks",
		Long: `List TTL-bounded task metadata for an application.

This is a recovery window, not persisted history. Pending includes queued,
running, and retrying work. A page can be empty and still have nextCursor.`,
		Example: `  # list recent tasks
  runware serverless apps tasks my-app --limit 10

  # filter by status
  runware serverless apps tasks my-app --status pending

  # page through results
  runware serverless apps tasks my-app --limit 10 --cursor <nextCursor>`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if err := validateListLimit(limit); err != nil {
				return err
			}
			id := args[0]
			statusVal, err := parseTaskStatus(status)
			if err != nil {
				return err
			}
			var params serverlessapi.ListTasksParams
			params.Limit, params.Cursor = listPageParams(limit, cursor)
			params.Status = statusVal

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching tasks for %s...", id))
			spin.Start()

			client := newServerlessClient(cmd, logger)
			page, err := client.ListTasks(cmd.Context(), id, params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printPage(cmdutil.FormatFor(cmd), page, tasksResult(page.Data), cmd.ErrOrStderr(), cursorFlags(cmd, extraStatusCursorFlag(status)))
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of tasks to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (pending, completed, failed, or canceled)")
	cmd.AddCommand(newAppsTasksShowCmd(logger))
	cmd.AddCommand(newAppsTasksCancelCmd(logger))
	return cmd
}

func newAppsTasksShowCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "show <appId> <taskId>",
		Short: "Show a single application task",
		Example: `  # show a task
  runware serverless apps tasks show my-app 7c9e6679-7425-40de-944b-e07fc1f90ae7`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID := args[0]
			taskID := args[1]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching task %s...", taskID))
			spin.Start()

			client := newServerlessClient(cmd, logger)
			task, err := client.GetTask(cmd.Context(), appID, taskID)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			if err := output.Print(cmdutil.FormatFor(cmd), taskResult(*task)); err != nil {
				return err
			}
			return taskFailedErr(task)
		},
	}
}

func parseTaskStatus(status string) (*serverlessapi.TaskStatus, error) {
	return parseValidFlag[serverlessapi.TaskStatus]("--status", status, "pending, completed, failed, or canceled")
}

func newAppsTasksCancelCmd(logger *log.Logger) *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "cancel <appId> [taskId]",
		Short: "Cancel a queued task, or all queued tasks of an application",
		Long: `Cancel a task that no worker has started. Workers that check for a canceled task skip it.

A task that is already running, completed, or failed cannot be canceled.

With --all, the application's queue is emptied and every queued task is
canceled, so resuming a stopped application starts no worker for them.
Canceling a single task leaves its message queued until a worker skips it.`,
		Example: `  # cancel one queued task
  runware serverless apps tasks cancel my-app 7c9e6679-7425-40de-944b-e07fc1f90ae7

  # cancel every queued task of an app
  runware serverless apps tasks cancel my-app --all`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID := args[0]
			if all == (len(args) == 2) {
				return fmt.Errorf("give either a task id or --all")
			}

			client := newServerlessClient(cmd, logger)
			if all {
				spin := cmdutil.NewSpinner(fmt.Sprintf("Canceling queued tasks of %s...", appID))
				spin.Start()
				canceled, err := client.CancelQueuedTasks(cmd.Context(), appID)
				spin.Stop()
				if err != nil {
					return err
				}
				return output.Print(cmdutil.FormatFor(cmd), tasksCanceledResult{
					AppID:    appID,
					Canceled: canceled,
				})
			}

			taskID := args[1]
			spin := cmdutil.NewSpinner(fmt.Sprintf("Canceling task %s...", taskID))
			spin.Start()
			task, err := client.CancelTask(cmd.Context(), appID, taskID)
			spin.Stop()
			if err != nil {
				return err
			}
			return output.Print(cmdutil.FormatFor(cmd), taskResult(*task))
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Cancel every queued task of the application")
	return cmd
}
