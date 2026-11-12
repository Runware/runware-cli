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

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

// endpointPathPattern and endpointPathMaxLen mirror the EndpointPath schema in
// api/serverless/openapi.yaml. A path is one or more lowercase segments joined
// by slashes, so an endpoint that answers on a nested route, as an
// OpenAI-compatible server does, is addressable here too.
var endpointPathPattern = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,62}[a-z0-9])?(/[a-z]([a-z0-9-]{0,62}[a-z0-9])?)*$`)

const endpointPathMaxLen = 255

// endpointPathEditor restores the slashes of a multi-segment endpoint path in
// the request URL. The generated client styles a path parameter with simple
// escaping, which turns those slashes into %2F, and the API rejects an
// endpointPath that arrives escaped.
//
// The editor is handed to the two operations that carry an endpointPath, so it
// knows the value to look for. A path it is not given is left alone: `invoke-sync`
// is itself a valid app id, so a route matched by its text would rewrite requests
// that carry no endpoint path at all.
func endpointPathEditor(endpointPath string) gen.RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		if req.URL == nil || req.URL.RawPath == "" {
			return nil
		}
		// Path is already decoded, so the parameter we were handed is its last
		// segments. Anything else on the URL is not ours to touch.
		if !strings.HasSuffix(req.URL.Path, "/"+endpointPath) {
			return nil
		}
		// The escaped parameter carries no slash of its own, so it is whatever
		// follows the last one, and the base URL keeps the encoding it came with.
		raw := req.URL.RawPath
		cut := strings.LastIndex(raw, "/") + 1
		req.URL.RawPath = raw[:cut] + endpointPath
		return nil
	}
}

// ResponseLostError reports an invocation the API may have accepted although
// no response said so: the connection dropped or timed out, the body could not
// be read, or a proxy or the API answered with a server error rather than a
// refusal. TaskID is the id the request carried, so the caller can read that
// task, or send the invocation again under the same id, without a second run.
type ResponseLostError struct {
	TaskID string
	Err    error
}

func (e *ResponseLostError) Error() string {
	return fmt.Sprintf("%v (task %s)", e.Err, e.TaskID)
}

func (e *ResponseLostError) Unwrap() error { return e.Err }

// taskNotFoundRetry is how long WaitTask retries getTask 404s. A freshly
// accepted id can temporarily miss the result store.
const taskNotFoundRetry = 30 * time.Second

// defaultTaskPollInterval is used when WaitTask is called with a non-positive interval.
const defaultTaskPollInterval = 2 * time.Second

// ValidateEndpointPath checks a path against the API's own EndpointPath schema.
// A leading slash is rejected by the API with 422; fail locally with a hint
// naming the path without it.
func ValidateEndpointPath(path string) error {
	if path == "" {
		return fmt.Errorf("endpoint path is required")
	}
	if strings.HasPrefix(path, "/") {
		bare := strings.TrimLeft(path, "/")
		if endpointPathPattern.MatchString(bare) {
			return fmt.Errorf("endpoint path %q must carry no leading slash (e.g. %q)", path, bare)
		}
		return fmt.Errorf("endpoint path %q must carry no leading slash", path)
	}
	if len(path) > endpointPathMaxLen {
		return fmt.Errorf("endpoint path is %d characters, over the %d the API accepts", len(path), endpointPathMaxLen)
	}
	if !endpointPathPattern.MatchString(path) {
		return fmt.Errorf("endpoint path %q is invalid: use the path apps endpoints reports, one or more lowercase segments joined by slashes, such as generate or v1/chat/completions", path)
	}
	return nil
}

// InvokeAsync starts a task and returns the accepted (typically pending) task.
// taskID is the client-owned identifier; an empty value is replaced with a new UUID.
func (c *Client) InvokeAsync(ctx context.Context, appID, endpointPath, taskID string, body TaskPayload) (*Task, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}
	if err := ValidateEndpointPath(endpointPath); err != nil {
		return nil, err
	}
	invocation, err := newTaskInvocation(taskID, body)
	if err != nil {
		return nil, err
	}

	// Send and parse in two steps: a parse failure drops the response, and
	// its status still decides whether the task may have been accepted.
	rsp, err := c.inner.StartAsyncTask(ctx, appID, endpointPath, &gen.StartAsyncTaskParams{
		Environment: c.environmentParam(),
	}, invocation, endpointPathEditor(endpointPath))
	if err != nil {
		return nil, responseLost(invocation.TaskId, fmt.Errorf("invoke async: %w", err))
	}
	defer func() { _ = rsp.Body.Close() }()
	resp, err := gen.ParseStartAsyncTaskResponse(rsp)
	if err != nil {
		c.logResponse(ctx, rsp, nil)
		return nil, unreadableInvokeError(invocation.TaskId, "invoke async", rsp.StatusCode, err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusAccepted:
		if resp.JSON202 == nil {
			return nil, responseLost(invocation.TaskId, fmt.Errorf("invoke async: empty 202 response"))
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
	case http.StatusServiceUnavailable:
		return nil, invokeUnavailableError(invocation.TaskId, resp.ApplicationproblemJSON503, resp.Body)
	default:
		return nil, invokeStatusError(invocation.TaskId, resp.Body, resp.StatusCode())
	}
}

// InvokeSync starts a task and waits up to the platform wait window.
// A wait-window expiry is not a failure: the accepted task is returned so
// the caller can poll. Never resubmit. A response that never arrives, or
// arrives unreadable, returns a *ResponseLostError naming the task id sent,
// which the caller polls in the same way.
func (c *Client) InvokeSync(ctx context.Context, appID, endpointPath, taskID string, body TaskPayload) (*Task, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}
	if err := ValidateEndpointPath(endpointPath); err != nil {
		return nil, err
	}
	invocation, err := newTaskInvocation(taskID, body)
	if err != nil {
		return nil, err
	}

	// Send and parse in two steps, as InvokeAsync does.
	rsp, err := c.innerWithMinTimeout(invokeSyncTimeout).StartSyncTask(ctx, appID, endpointPath, &gen.StartSyncTaskParams{
		Environment: c.environmentParam(),
	}, invocation, endpointPathEditor(endpointPath))
	if err != nil {
		return nil, responseLost(invocation.TaskId, fmt.Errorf("invoke sync: %w", err))
	}
	defer func() { _ = rsp.Body.Close() }()
	resp, err := gen.ParseStartSyncTaskResponse(rsp)
	if err != nil {
		c.logResponse(ctx, rsp, nil)
		return nil, unreadableInvokeError(invocation.TaskId, "invoke sync", rsp.StatusCode, err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, responseLost(invocation.TaskId, fmt.Errorf("invoke sync: empty 200 response"))
		}
		return resp.JSON200, nil
	case http.StatusAccepted:
		if resp.JSON202 != nil {
			return resp.JSON202, nil
		}
		task, err := taskFromBody(resp.Body)
		if err != nil {
			return nil, responseLost(invocation.TaskId, fmt.Errorf("invoke sync: wait window expired with an unreadable 202 response"))
		}
		return task, nil
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
	case http.StatusServiceUnavailable:
		return nil, invokeUnavailableError(invocation.TaskId, resp.ApplicationproblemJSON503, resp.Body)
	default:
		return nil, invokeStatusError(invocation.TaskId, resp.Body, resp.StatusCode())
	}
}

// responseLost wraps err, an invocation failure that leaves unknown whether
// the API accepted the task, with the task id the request carried.
func responseLost(taskID uuid.UUID, err error) error {
	return &ResponseLostError{
		TaskID: taskID.String(),
		Err:    err,
	}
}

// invokeUnavailableError is the error for a 503 to an invocation. The API's
// own 503 carries a problem document and mints no task, so it is a refusal.
// A 503 without one, whether it carries a page or JSON of another shape,
// came from a proxy in front of the API, which says nothing about whether
// the task was accepted.
func invokeUnavailableError(taskID uuid.UUID, problem *gen.ProblemDetails, body []byte) error {
	if problem != nil && isProblemDetails(*problem) {
		return problemToError(problem, http.StatusServiceUnavailable)
	}
	return responseLost(taskID, problemFromBody(body, http.StatusServiceUnavailable))
}

// unreadableInvokeError is the error for an invocation response whose body
// could not be read or decoded. Its status still arrived and decides: a
// client error is the API's refusal whatever the body holds, while a success
// or a server error leaves unknown whether the task was accepted. A server
// error keeps its status, as invokeStatusError's does.
func unreadableInvokeError(taskID uuid.UUID, op string, statusCode int, err error) error {
	if statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError {
		return problemToError(nil, statusCode)
	}
	if statusCode >= http.StatusInternalServerError {
		err = fmt.Errorf("%w: %w", problemToError(nil, statusCode), err)
	}
	return responseLost(taskID, fmt.Errorf("%s: unreadable response: %w", op, err))
}

// invokeStatusError is the error for an invocation status the operation does
// not bind. A server error may follow an accepted task: the API answers 502
// when its task service is unreachable after accepting, and a proxy answers
// 520 or 524 when the connection to the API drops mid-response.
func invokeStatusError(taskID uuid.UUID, body []byte, statusCode int) error {
	err := problemFromBody(body, statusCode)
	if statusCode >= http.StatusInternalServerError {
		return responseLost(taskID, err)
	}
	return err
}

// GetTask returns a task by id.
func (c *Client) GetTask(ctx context.Context, appID, taskID string) (*Task, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	id, err := parseTaskID(taskID)
	if err != nil {
		return nil, err
	}

	resp, err := c.inner.GetTaskWithResponse(ctx, appID, id, &gen.GetTaskParams{
		Environment: c.environmentParam(),
	})
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
	case http.StatusNotFound:
		return nil, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	case http.StatusServiceUnavailable:
		return nil, problemToError(resp.ApplicationproblemJSON503, http.StatusServiceUnavailable)
	default:
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// ListTasks returns a page of tasks for an app.
func (c *Client) ListTasks(ctx context.Context, appID string, params ListTasksParams) (Page[Task], error) {
	if c.apiKey == "" {
		return Page[Task]{}, transport.ErrNoAPIKey
	}

	params.Environment = c.environmentParam()
	resp, err := c.inner.ListTasksWithResponse(ctx, appID, &params)
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
	case http.StatusNotFound:
		return Page[Task]{}, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	case http.StatusUnprocessableEntity:
		return Page[Task]{}, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	case http.StatusServiceUnavailable:
		return Page[Task]{}, problemToError(resp.ApplicationproblemJSON503, http.StatusServiceUnavailable)
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

func newTaskInvocation(taskID string, body TaskPayload) (gen.TaskInvocation, error) {
	id, err := resolveTaskID(taskID)
	if err != nil {
		return gen.TaskInvocation{}, err
	}
	if body == nil {
		body = TaskPayload{}
	}
	return gen.TaskInvocation{
		Payload: body,
		TaskId:  id,
	}, nil
}

func resolveTaskID(taskID string) (uuid.UUID, error) {
	if taskID == "" {
		return uuid.New(), nil
	}
	return parseTaskID(taskID)
}

func parseTaskID(taskID string) (uuid.UUID, error) {
	id, err := uuid.Parse(taskID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid task id %q: must be a lowercase UUID", taskID)
	}
	return id, nil
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

func isNotFound(err error) bool {
	var re *transport.RunwareError
	return errors.As(err, &re) && re.Code == transport.CodeNotFound
}
