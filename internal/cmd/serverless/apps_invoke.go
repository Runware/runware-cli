package serverless

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

func newAppsInvokeCmd(logger *log.Logger) *cobra.Command {
	var (
		sync         bool
		wait         bool
		timeout      time.Duration
		bodyFile     string
		taskID       string
		pollInterval time.Duration
	)

	cmd := &cobra.Command{
		Use:   "invoke <appId> <endpointPath>",
		Short: "Invoke an application endpoint",
		Long: `Submit a JSON payload to a named application endpoint.

endpointPath is the path apps endpoints reports, one or more lowercase
segments joined by slashes (e.g. infer, or v1/chat/completions for a server
that answers on a nested route). A leading slash is rejected.

The default is async: the command prints the accepted task id. Pass --wait
to poll until the task is completed or failed, and --timeout to bound that
wait.

--sync uses the sync invocation endpoint. If the platform wait window
expires, the command polls the returned task id; it never treats expiry as
a failure and never resubmits. --timeout bounds that sync call and the poll
together, and requires --wait or --sync.

A client-generated task id is sent with every invoke. Omit --task-id to
generate one. Resubmitting the same id returns the task it already names
instead of starting a second run.

When no response arrives (a dropped connection, a body that cannot be read,
or a server error from a proxy such as Cloudflare), the command fails with
code responseLost, the task id it sent in taskUUID, and how to read that
task. With --sync or --wait it polls that id first, so a run the worker
finished is neither lost nor run again.`,
		Example: `  # list endpoint paths, then invoke asynchronously
  runware serverless apps endpoints my-app
  runware serverless apps invoke my-app infer -f payload.json

  # wait for a completed task (sync, then poll if the wait window expires)
  runware serverless apps invoke my-app infer --sync -f payload.json

  # async invoke and poll
  runware serverless apps invoke my-app infer --wait --timeout 2m -f payload.json

  # retry a lost response without starting a second task
  runware serverless apps invoke my-app infer --task-id 7c9e6679-7425-40de-944b-e07fc1f90ae7 -f payload.json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID := args[0]
			endpointPath, payload, err := parseInvokeInput(args[1], bodyFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if err := validateWaitFlags(cmd, sync || wait, timeout); err != nil {
				return err
			}

			client := newServerlessClient(cmd, logger)
			spin := cmdutil.NewSpinner(fmt.Sprintf("Invoking %s on %s...", endpointPath, appID))
			spin.Start()

			waitCtx, cancel := waitContext(cmd.Context(), timeout)
			defer cancel()

			var task *serverlessapi.Task
			if sync {
				task, err = client.InvokeSync(waitCtx, appID, endpointPath, taskID, payload)
			} else {
				task, err = client.InvokeAsync(cmd.Context(), appID, endpointPath, taskID, payload)
			}
			if lost, ok := errors.AsType[*serverlessapi.ResponseLostError](err); ok {
				showCmd := taskShowCommand(cmd, appID, lost.TaskID)
				cause := waitTimeoutErr(waitCtx, lost.Err, "sync invocation", timeout)
				if polling := (sync || wait) && waitCtx.Err() == nil; !polling {
					spin.Stop()
					return newLostInvokeError(lost.TaskID, showCmd, cause, nil)
				}
				// A JSON or YAML error is the whole of stderr, so only table
				// output gets a note ahead of it.
				if cmdutil.FormatFor(cmd) == output.FormatTable {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Task %s may have started, but no response confirmed it. Polling it...\n", lost.TaskID)
				}
				spin.SetMessage(fmt.Sprintf("Waiting for task %s...", lost.TaskID))
				task, err = client.WaitTask(waitCtx, appID, lost.TaskID, pollInterval)
				if err != nil {
					spin.Stop()
					return newLostInvokeError(lost.TaskID, showCmd, cause, waitTimeoutErr(waitCtx, err, "task "+lost.TaskID, timeout))
				}
			}
			if err != nil {
				spin.Stop()
				return waitTimeoutErr(waitCtx, err, "sync invocation", timeout)
			}

			shouldWait := sync || wait
			if shouldWait && task.Status == serverlessapi.TaskStatusPending {
				waitedID := task.Id
				var depth *int64
				if app, appErr := client.GetApp(waitCtx, appID); appErr == nil {
					depth = app.Runtime.QueueDepth
				}
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), invokeWaitMessage(waitedID, depth))
				spin.SetMessage(fmt.Sprintf("Waiting for task %s...", waitedID))
				task, err = client.WaitTask(waitCtx, appID, waitedID, pollInterval)
				if err != nil {
					spin.Stop()
					return waitTimeoutErr(waitCtx, err, "task "+waitedID, timeout)
				}
			}
			spin.Stop()

			if err := output.Print(cmdutil.FormatFor(cmd), taskResult(*task)); err != nil {
				return err
			}
			return taskFailedErr(task)
		},
	}

	cmd.Flags().BoolVar(&sync, "sync", false, "Use sync invocation and wait for a terminal task")
	cmd.Flags().BoolVar(&wait, "wait", false, "Poll until the task is completed or failed")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Maximum time to wait (0 = no limit)")
	cmd.Flags().StringVarP(&bodyFile, "body", "f", "", "JSON payload file, or - for stdin (default {})")
	cmd.Flags().StringVar(&taskID, "task-id", "", "Client task id (UUID); generated if omitted")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 2*time.Second, "Polling interval when waiting for a task")
	return cmd
}

func parseInvokeInput(endpointPath, bodyFile string, stdin io.Reader) (string, serverlessapi.TaskPayload, error) {
	if err := serverlessapi.ValidateEndpointPath(endpointPath); err != nil {
		return "", nil, err
	}
	payload, err := readTaskPayload(bodyFile, stdin)
	if err != nil {
		return "", nil, err
	}
	return endpointPath, payload, nil
}

func readTaskPayload(path string, stdin io.Reader) (serverlessapi.TaskPayload, error) {
	if path == "" {
		return serverlessapi.TaskPayload{}, nil
	}
	raw, err := readValueFlag("", path, stdin)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return serverlessapi.TaskPayload{}, nil
	}
	var payload serverlessapi.TaskPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("parse body JSON: %w", err)
	}
	if payload == nil {
		return serverlessapi.TaskPayload{}, nil
	}
	return payload, nil
}

// invokeWaitMessage is the line printed when a waited invoke is still pending.
// depth is the app's queue depth when that read succeeded, and nil when it is
// unknown. This command polls only when --sync or --wait is set, so the hint
// names one of those flags together with --task-id.
func invokeWaitMessage(taskID string, depth *int64) string {
	depthText := ""
	if depth != nil {
		depthText = fmt.Sprintf(" App queue depth: %d.", *depth)
	}
	return fmt.Sprintf(
		"Task %s is pending (queued or running). Polling it.%s To wait on it again, run the same command with --sync or --wait and --task-id %s.",
		taskID, depthText, taskID,
	)
}

// taskShowCommand is the command line that reads taskID on appID in the
// environment cmd addresses.
func taskShowCommand(cmd *cobra.Command, appID, taskID string) string {
	choice := environmentFor(cmd)
	return serverlessCommand(choice.name, choice.defaultEnvironment, "apps", "tasks", "show", appID, taskID)
}

// lostResponseCode is the error code of an invoke no response confirmed.
// Scripts read the task id beside it in taskUUID.
const lostResponseCode = "responseLost"

// lostInvokeError is how an invoke fails when no response confirmed its task
// and polling did not recover it. Its *transport.RunwareError gives JSON and
// YAML output the code and task id as fields, and it unwraps to its causes
// too, so an interrupt still reads as one. It is a cmdutil.ResumableError, so
// an interrupted run still prints the task id it needs to read that task.
type lostInvokeError struct {
	api    *transport.RunwareError
	causes []error
}

var _ cmdutil.ResumableError = (*lostInvokeError)(nil)

func (e *lostInvokeError) Error() string { return e.api.Message }

// Resumable marks the error as one the CLI prints even when interrupted.
func (*lostInvokeError) Resumable() {}

func (e *lostInvokeError) Unwrap() []error {
	return append([]error{e.api}, e.causes...)
}

// newLostInvokeError reports that task taskID may have started. cause is why
// no response confirmed it, and pollErr, when not nil, why polling it failed.
// The message ends with how to reach the task without a second run; an API
// error in cause lends its status and problem type.
func newLostInvokeError(taskID, showCmd string, cause, pollErr error) error {
	msg := fmt.Sprintf("task %s may have started, but no response confirmed it: %v", taskID, cause)
	causes := []error{cause}
	if pollErr != nil {
		msg += fmt.Sprintf("\npolling it failed: %v", pollErr)
		causes = append(causes, pollErr)
	}
	msg += fmt.Sprintf("\nRead it with: %s\nOr run the same invoke with --task-id %s: it returns that task instead of starting a second run.", showCmd, taskID)

	details := transport.RunwareErrorDetails{
		TaskUUID: taskID,
	}
	var problemType string
	if re, ok := errors.AsType[*transport.RunwareError](cause); ok {
		details.StatusCode = re.StatusCode
		problemType = re.ProblemType
	}
	api := transport.CreateRunwareError(lostResponseCode, msg, details)
	api.ProblemType = problemType
	return &lostInvokeError{
		api:    api,
		causes: causes,
	}
}

func taskFailedErr(task *serverlessapi.Task) error {
	if task == nil || task.Status != serverlessapi.TaskStatusFailed {
		return nil
	}
	if task.Error != nil && *task.Error != "" {
		return fmt.Errorf("%s", *task.Error)
	}
	return fmt.Errorf("task failed")
}
