package serverless

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

func newAppsInvokeCmd(logger *log.Logger) *cobra.Command {
	var (
		sync         bool
		wait         bool
		bodyFile     string
		pollInterval time.Duration
	)

	cmd := &cobra.Command{
		Use:   "invoke <appId> <endpointPath>",
		Short: "Invoke an application endpoint",
		Long: `Submit a JSON payload to a named application endpoint.

endpointPath is a bare lowercase segment as returned by apps endpoints
(e.g. infer). A leading slash is rejected.

The default is async: the command prints the accepted task id. Pass --wait
to poll until the task is completed or failed.

--sync uses the sync invocation endpoint. If the platform wait window
expires, the command polls the returned task id; it never treats expiry as
a failure and never resubmits.`,
		Example: `  # list endpoint paths, then invoke asynchronously
  runware serverless apps endpoints my-app
  runware serverless apps invoke my-app infer -f payload.json

  # wait for a completed task (sync, then poll if the wait window expires)
  runware serverless apps invoke my-app infer --sync -f payload.json

  # async invoke and poll
  runware serverless apps invoke my-app infer --wait -f payload.json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID := args[0]
			endpointPath, payload, err := parseInvokeInput(args[1], bodyFile, cmd.InOrStdin())
			if err != nil {
				return err
			}

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			spin := cmdutil.NewSpinner(fmt.Sprintf("Invoking %s on %s...", endpointPath, appID))
			spin.Start()

			var task *serverlessapi.Task
			if sync {
				task, err = client.InvokeSync(cmd.Context(), appID, endpointPath, payload)
			} else {
				task, err = client.InvokeAsync(cmd.Context(), appID, endpointPath, payload)
			}
			if err != nil {
				spin.Stop()
				return err
			}

			shouldWait := sync || wait
			if shouldWait && task.Status == serverlessapi.TaskStatusPending {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Task %s accepted; waiting...\n", task.Id)
				spin.SetMessage(fmt.Sprintf("Waiting for task %s...", task.Id))
				task, err = client.WaitTask(cmd.Context(), appID, task.Id, pollInterval)
				if err != nil {
					spin.Stop()
					return err
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
	cmd.Flags().StringVarP(&bodyFile, "body", "f", "", "JSON payload file, or - for stdin (default {})")
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

func taskFailedErr(task *serverlessapi.Task) error {
	if task == nil || task.Status != serverlessapi.TaskStatusFailed {
		return nil
	}
	if task.Error != nil && *task.Error != "" {
		return fmt.Errorf("%s", *task.Error)
	}
	return fmt.Errorf("task failed")
}
