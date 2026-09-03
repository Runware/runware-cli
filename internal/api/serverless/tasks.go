package serverless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

// endpointPathPattern is ADR-034: a bare lowercase segment, no leading slash.
var endpointPathPattern = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,62}[a-z0-9])?$`)

// taskNotFoundRetry is how long WaitTask retries getTask 404s. A freshly
// accepted id can temporarily miss the result store.
const taskNotFoundRetry = 30 * time.Second

// defaultTaskPollInterval is used when WaitTask is called with a non-positive interval.
const defaultTaskPollInterval = 2 * time.Second

// ValidateEndpointPath checks ADR-034. A leading slash is rejected by the API
// with 422; fail locally with a hint naming the bare segment.
func ValidateEndpointPath(path string) error {
	if path == "" {
		return fmt.Errorf("endpoint path is required")
	}
	if strings.HasPrefix(path, "/") {
		bare := strings.TrimLeft(path, "/")
		if endpointPathPattern.MatchString(bare) {
			return fmt.Errorf("endpoint path %q must be a bare segment without a leading slash (e.g. %q)", path, bare)
		}
		return fmt.Errorf("endpoint path %q must be a bare lowercase segment without a leading slash", path)
	}
	if !endpointPathPattern.MatchString(path) {
		return fmt.Errorf("endpoint path %q is invalid: use a lowercase segment of 1-64 characters (letters, digits, hyphens)", path)
	}
	return nil
}

// InvokeAsync starts a task and returns the accepted (typically pending) task.
func (c *Client) InvokeAsync(ctx context.Context, appID, endpointPath string, body TaskPayload) (*Task, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}
	if err := ValidateEndpointPath(endpointPath); err != nil {
		return nil, err
	}
	if body == nil {
		body = TaskPayload{}
	}

	resp, err := c.inner.StartAsyncTaskWithResponse(ctx, appID, endpointPath, gen.StartAsyncTaskJSONRequestBody(body))
	if err != nil {
		return nil, fmt.Errorf("invoke async: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusAccepted:
		if resp.JSON202 == nil {
			return nil, fmt.Errorf("invoke async: empty 202 response")
		}
		return resp.JSON202, nil
	case http.StatusBadRequest:
		return nil, problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
	case http.StatusUnauthorized:
		return nil, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return nil, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return nil, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	case http.StatusConflict:
		return nil, problemToError(resp.ApplicationproblemJSON409, http.StatusConflict)
	case http.StatusUnprocessableEntity:
		return nil, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	default:
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// InvokeSync starts a task and waits up to the platform wait window.
// A wait-window expiry (504 today, 202 after RUNSERV-547) is not a failure:
// the accepted task is returned so the caller can poll. Never resubmit.
func (c *Client) InvokeSync(ctx context.Context, appID, endpointPath string, body TaskPayload) (*Task, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}
	if err := ValidateEndpointPath(endpointPath); err != nil {
		return nil, err
	}
	if body == nil {
		body = TaskPayload{}
	}

	resp, err := c.innerWithMinTimeout(invokeSyncTimeout).StartSyncTaskWithResponse(ctx, appID, endpointPath, gen.StartSyncTaskJSONRequestBody(body))
	if err != nil {
		return nil, fmt.Errorf("invoke sync: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("invoke sync: empty 200 response")
		}
		return resp.JSON200, nil
	case http.StatusAccepted:
		// Settled shape (RUNSERV-547): wait expiry returns 202 + Task.
		task, err := taskFromBody(resp.Body)
		if err != nil {
			if id := taskIDFromProblem(nil, resp.Body); id != "" {
				return pendingTask(appID, id), nil
			}
			return nil, fmt.Errorf("invoke sync: wait window expired without a task id")
		}
		return task, nil
	case http.StatusGatewayTimeout:
		if id := taskIDFromProblem(resp.ApplicationproblemJSON504, resp.Body); id != "" {
			return pendingTask(appID, id), nil
		}
		return nil, fmt.Errorf("invoke sync: wait window expired without a task id")
	case http.StatusBadRequest:
		return nil, problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
	case http.StatusUnauthorized:
		return nil, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return nil, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return nil, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	case http.StatusConflict:
		return nil, problemToError(resp.ApplicationproblemJSON409, http.StatusConflict)
	case http.StatusUnprocessableEntity:
		return nil, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	default:
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// GetTask returns a task by id.
func (c *Client) GetTask(ctx context.Context, appID, taskID string) (*Task, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.GetTaskWithResponse(ctx, appID, taskID)
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("get task: empty 200 response")
		}
		return resp.JSON200, nil
	case http.StatusUnauthorized:
		return nil, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return nil, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return nil, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	default:
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// ListTasks returns a page of tasks for an app.
func (c *Client) ListTasks(ctx context.Context, appID string, params *ListTasksParams) (Page[Task], error) {
	if c.apiKey == "" {
		return Page[Task]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListTasksWithResponse(ctx, appID, params)
	if err != nil {
		return Page[Task]{}, fmt.Errorf("list tasks: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return pageOf[Task](nil, nil), nil
		}
		return pageOf(resp.JSON200.Data, resp.JSON200.NextCursor), nil
	case http.StatusBadRequest:
		return Page[Task]{}, problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
	case http.StatusUnauthorized:
		return Page[Task]{}, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return Page[Task]{}, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return Page[Task]{}, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	case http.StatusUnprocessableEntity:
		return Page[Task]{}, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	default:
		return Page[Task]{}, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// WaitTask polls getTask until the task is completed or failed.
// Transient 404s are retried for taskNotFoundRetry; the invocation is never
// resubmitted.
func (c *Client) WaitTask(ctx context.Context, appID, taskID string, interval time.Duration) (*Task, error) {
	if interval <= 0 {
		interval = defaultTaskPollInterval
	}

	var notFoundSince time.Time
	for {
		task, err := c.GetTask(ctx, appID, taskID)
		if err != nil {
			if isNotFound(err) {
				if notFoundSince.IsZero() {
					notFoundSince = time.Now()
				}
				if time.Since(notFoundSince) > taskNotFoundRetry {
					return nil, err
				}
			} else {
				return nil, err
			}
		} else {
			notFoundSince = time.Time{}
			if task.Status != TaskStatusPending {
				return task, nil
			}
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func pendingTask(appID, taskID string) *Task {
	return &Task{
		Id:     taskID,
		AppId:  appID,
		Status: TaskStatusPending,
	}
}

func taskFromBody(body []byte) (*Task, error) {
	var task Task
	if err := json.Unmarshal(body, &task); err != nil {
		return nil, err
	}
	if task.Id == "" {
		return nil, fmt.Errorf("empty task id")
	}
	return &task, nil
}

func taskIDFromProblem(p *gen.ProblemDetails, body []byte) string {
	if p != nil && p.TaskId != nil && *p.TaskId != "" {
		return *p.TaskId
	}
	if len(body) == 0 {
		return ""
	}
	var parsed gen.ProblemDetails
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ""
	}
	if parsed.TaskId == nil {
		return ""
	}
	return *parsed.TaskId
}

func isNotFound(err error) bool {
	var re *transport.RunwareError
	return errors.As(err, &re) && re.Code == transport.CodeNotFound
}
