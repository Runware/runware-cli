package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/schema"
)

// Client provides business-logic methods for the Runware API.
// It delegates wire-level communication to the injected Transport.
type Client struct {
	transport     transport.Transport
	logger        *slog.Logger
	schemaBaseURL string // overrides schemaBaseURL constant; empty means use package default
}

// NewClient creates a Client backed by the given transport.
func NewClient(t transport.Transport, logger *slog.Logger) *Client {
	return &Client{
		transport: t,
		logger:    logger,
	}
}

// Ping checks API connectivity.
func (c *Client) Ping(ctx context.Context) (*PingResult, error) {
	tasks := []any{
		&PingRequest{
			TaskType: taskTypePing,
			TaskUUID: uuid.New(),
		},
	}

	data, err := c.transport.Send(ctx, tasks)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("empty response from ping")
	}

	var result PingResult
	if err := json.Unmarshal(data[0], &result); err != nil {
		return nil, fmt.Errorf("failed to parse ping response: %w", err)
	}

	return &result, nil
}

// AccountDetails fetches account information.
func (c *Client) AccountDetails(ctx context.Context) (*AccountResult, error) {
	tasks := []any{
		&AccountManagementRequest{
			TaskType:  taskTypeAccountManagement,
			TaskUUID:  uuid.New(),
			Operation: "getDetails",
		},
	}

	data, err := c.transport.Send(ctx, tasks)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("empty response from account details")
	}

	var result AccountResult
	if err := json.Unmarshal(data[0], &result); err != nil {
		return nil, fmt.Errorf("failed to parse account response: %w", err)
	}

	return &result, nil
}

// submit sends a single arbitrary task payload and returns all raw JSON responses.
// It is the low-level wire call used by Run after system fields have been injected.
func (c *Client) submit(ctx context.Context, payload map[string]any) ([]json.RawMessage, error) {
	data, err := c.transport.Send(ctx, []any{payload})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Run executes the full inference lifecycle for the given model.
//
// model is the AIR identifier of the model to run (e.g. "runware:101@1").
// It fetches the model's JSON Schema to auto-detect the task type and validate
// user-supplied parameters. If schema fetching fails and opts.TaskType is
// non-empty, validation is skipped and the request is submitted with best-effort
// type coercion. System fields (taskType, taskUUID, deliveryMethod) are injected
// automatically; callers must not set them.
//
// For async delivery Run polls until a success result is received or the context
// is cancelled. For sync delivery the submit response is returned directly.
func (c *Client) Run(ctx context.Context, model string, params map[string]any, opts RunOptions) ([]json.RawMessage, error) {
	baseURL := c.schemaBaseURL
	if baseURL == "" {
		baseURL = schemaBaseURL
	}

	// Inject the model into params before submission.
	params[fieldModel] = model

	// Fetch the model schema; fail-open when the caller has supplied a task type.
	var modelSchema *ModelSchema
	if model != "" {
		ms, err := fetchModelSchema(ctx, model, schemaClient, baseURL)
		if err != nil {
			if opts.TaskType == "" {
				return nil, fmt.Errorf("could not fetch schema for %q: %w; set RunOptions.TaskType to skip validation", model, err)
			}
			c.logger.Warn("schema unavailable; skipping validation", "model", model, "err", err)
		} else {
			modelSchema = ms
		}
	}

	// Unmarshal the request schema node for validation and field extraction.
	var reqSchema schema.Node
	if modelSchema != nil {
		if err := json.Unmarshal(modelSchema.RequestSchema, &reqSchema); err != nil {
			return nil, fmt.Errorf("failed to parse request schema: %w", err)
		}
	}

	// Determine the task type: caller override > schema detection.
	taskType := opts.TaskType
	if taskType == "" {
		detected, ok := schema.ExtractTaskType(reqSchema)
		if !ok {
			return nil, fmt.Errorf("could not detect task type for model %q; set RunOptions.TaskType", model)
		}
		taskType = detected
	}

	// Validate required fields and conditional constraints against the schema.
	if modelSchema != nil {
		if err := schema.ValidateRequired(reqSchema, params); err != nil {
			return nil, err
		}
		if err := schema.ValidateAllOf(reqSchema, params); err != nil {
			return nil, err
		}
	}

	// Resolve delivery method: payload value > opts override > schema default.
	deliveryMethod := schema.ResolveDeliveryMethod(opts.DeliveryMethod, params, reqSchema)
	if deliveryMethod != "" {
		params[fieldDeliveryMethod] = deliveryMethod
	}

	// Inject system fields.
	taskUUID := uuid.New()
	params[fieldTaskType] = taskType
	params[fieldTaskUUID] = taskUUID

	// Submit the request to the API.
	initialResults, err := c.submit(ctx, params)
	if err != nil {
		return nil, err
	}

	// For async delivery, discard the submit acknowledgment and poll until done.
	if deliveryMethod == string(DeliveryMethodAsync) {
		interval := opts.PollInterval
		if interval == 0 {
			interval = 2 * time.Second
		}
		return c.Poll(ctx, taskUUID, interval, opts.OnProgress)
	}

	return initialResults, nil
}

// ModelSearch searches for models on the Runware platform.
func (c *Client) ModelSearch(ctx context.Context, req ModelSearchRequest) (*ModelSearchResponse, error) {
	req.TaskType = taskTypeModelSearch
	req.TaskUUID = uuid.New()

	data, err := c.transport.Send(ctx, []any{req})
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("empty response from modelSearch")
	}

	var result ModelSearchResponse
	if err := json.Unmarshal(data[0], &result); err != nil {
		return nil, fmt.Errorf("failed to parse modelSearch response: %w", err)
	}

	return &result, nil
}

// Poll polls for async task results using the getResponse task type.
// It blocks until at least one result with status "success" is returned, the
// context is cancelled, or a fatal API/auth error occurs.
//
// onProgress is called with the reported progress percentage (0–100) each time
// a "processing" status item is received. It may be nil.
func (c *Client) Poll(ctx context.Context, taskID uuid.UUID, interval time.Duration, onProgress func(int)) ([]json.RawMessage, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var results []json.RawMessage
	for {
		tasks := []any{
			&GetResponseRequest{
				TaskType: taskTypeGetResponse,
				TaskUUID: taskID,
			},
		}

		data, err := c.transport.Send(ctx, tasks)
		if err != nil {
			var re *transport.RunwareError
			if errors.As(err, &re) || transport.IsAuthError(err) {
				return nil, err
			}
			if c.logger.Enabled(ctx, slog.LevelDebug) {
				c.logger.Debug("poll error", "err", err)
			}
		} else {
			for _, raw := range data {
				var item pollResponseItem
				if err := json.Unmarshal(raw, &item); err != nil {
					continue
				}
				switch item.Status {
				case "success":
					results = append(results, raw)
				case "processing":
					if onProgress != nil {
						onProgress(item.Progress)
					}
				}
			}
			if len(results) > 0 {
				return results, nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
