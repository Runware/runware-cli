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
	transport             transport.Transport
	logger                *slog.Logger
	schemaBaseURLOverride string // non-empty overrides the package-level schemaBaseURL constant
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
// args is a slice of key=value strings (e.g. ["positivePrompt=hello", "width=1024"]).
// They are parsed against the model's fetched JSON Schema so that type coercion
// (string vs number vs bool) is schema-driven rather than best-effort.
// System fields (taskType, taskUUID, deliveryMethod) are injected automatically;
// callers must not include them in rawArgs.
//
// For async delivery Run polls until a success result is received or the context
// is cancelled. For sync delivery the submit response is returned directly.
func (c *Client) Run(ctx context.Context, model string, args []string, opts RunOptions) ([]json.RawMessage, error) {
	if model == "" {
		return nil, ErrModelRequired
	}

	baseURL := c.schemaBaseURLOverride
	if baseURL == "" {
		baseURL = schemaBaseURL
	}

	// Fetch the model schema; fail-open when the caller has supplied a task type.
	var modelSchema *ModelSchema
	ms, err := fetchModelSchema(ctx, model, schemaClient, baseURL)
	if err != nil {
		if opts.TaskType == "" {
			return nil, fmt.Errorf("could not fetch schema for %q: %w; set RunOptions.TaskType to skip validation", model, err)
		}
		c.logger.Warn("schema unavailable; skipping validation", "model", model, "err", err)
	} else {
		modelSchema = ms
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

	// modelUpload is a platform utility, not an inference task; it has its own
	// command with explicit flags.
	if taskType == string(taskTypeModelUpload) {
		return nil, ErrModelUploadViaRun
	}

	// Parse args against the real schema so type coercion is schema-driven.
	// Protected fields (taskType, taskUUID, model, deliveryMethod) are rejected here.
	payload := make(map[string]any, len(args)+4)
	payload[fieldModel] = model
	for _, a := range args {
		path, v, err := schema.ParseKV(a, reqSchema)
		if err != nil {
			return nil, fmt.Errorf("invalid argument %q: %w", a, err)
		}
		if hint, blocked := schema.IsProtected(path[0]); blocked {
			return nil, fmt.Errorf("argument %q: key %q is reserved — %s", a, path[0], hint)
		}
		schema.DeepSet(payload, path, v)
	}

	// Validate required fields and conditional constraints — opt-in via --validate.
	// Off by default so the API remains the source of truth for requirements; the
	// fetched schema can disagree with the live API (see RUN-10584).
	if modelSchema != nil && opts.Validate {
		if err := schema.ValidateRequired(reqSchema, payload); err != nil {
			return nil, err
		}
		if err := schema.ValidateAllOf(reqSchema, payload); err != nil {
			return nil, err
		}
	}

	// Resolve delivery method: payload value > opts override > schema default.
	deliveryMethod := schema.ResolveDeliveryMethod(opts.DeliveryMethod, payload, reqSchema)
	if deliveryMethod != "" {
		payload[fieldDeliveryMethod] = deliveryMethod
	}

	// Inject system fields.
	taskUUID := uuid.New()
	payload[fieldTaskType] = taskType
	payload[fieldTaskUUID] = taskUUID

	// Submit the request to the API.
	initialResults, err := c.submit(ctx, payload)
	if err != nil {
		return nil, err
	}

	// For async delivery, discard the submit acknowledgment and poll until done.
	if deliveryMethod == string(DeliveryMethodAsync) {
		if opts.OnSubmit != nil {
			opts.OnSubmit(taskUUID)
		}
		interval := opts.PollInterval
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

// modelUpload pipeline terminal statuses.
const (
	uploadStatusReady  = "ready"
	uploadStatusFailed = "failed"
)

// ModelUpload submits a modelUpload task and follows the streamed pipeline
// statuses (validated → downloaded → optimized → stored) until the model is
// ready or the upload fails.
//
// The pipeline statuses are only delivered as streamed frames over the
// WebSocket transport (the API does not expose them via getResponse polling),
// so the transport must implement [transport.StreamSender].
func (c *Client) ModelUpload(ctx context.Context, req ModelUploadRequest, opts ModelUploadOptions) (*ModelUploadResult, error) {
	ss, ok := c.transport.(transport.StreamSender)
	if !ok {
		return nil, ErrModelUploadTransport
	}

	req.TaskType = taskTypeModelUpload
	req.TaskUUID = uuid.New()

	var result *ModelUploadResult
	var lastStatus string
	err := ss.SendStream(ctx, req, func(raw json.RawMessage) (bool, error) {
		res, done, err := consumeUploadFrame(raw, opts.OnStatus, &lastStatus)
		if done {
			result = res
		}
		return done, err
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("model upload stream ended without a terminal status")
	}
	return result, nil
}

// consumeUploadFrame inspects a single modelUpload pipeline frame. done is
// true for a terminal frame ("ready" or "failed"). Intermediate statuses are
// reported through onStatus, deduplicated against *lastStatus so repeated
// frames for the same phase fire the callback once.
func consumeUploadFrame(raw json.RawMessage, onStatus func(status, message string), lastStatus *string) (*ModelUploadResult, bool, error) {
	var item ModelUploadResult
	if err := json.Unmarshal(raw, &item); err != nil {
		// Unparseable frames are skipped, not fatal: the stream continues
		// until a terminal frame arrives.
		return nil, false, nil //nolint:nilerr
	}
	switch item.Status {
	case uploadStatusReady:
		return &item, true, nil
	case uploadStatusFailed:
		msg := item.Message
		if msg == "" {
			msg = "no failure reason reported"
		}
		return nil, true, fmt.Errorf("model upload failed: %s", msg)
	case "":
		return nil, false, nil
	default:
		if item.Status != *lastStatus {
			*lastStatus = item.Status
			if onStatus != nil {
				onStatus(item.Status, item.Message)
			}
		}
		return nil, false, nil
	}
}

// Poll polls for async task results using the getResponse task type.
// It blocks until at least one result with status "success" is returned, a data
// item reports status "error", the context is cancelled, or a fatal API/auth
// error occurs.
//
// onProgress is called with the reported progress percentage (0–100) each time
// a "processing" status item is received. It may be nil.
func (c *Client) Poll(ctx context.Context, taskID uuid.UUID, interval time.Duration, onProgress func(int)) ([]json.RawMessage, error) {
	if interval == 0 {
		interval = 2 * time.Second
	}

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
				case "error":
					return nil, pollTerminalError(item)
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

// pollTerminalError formats a terminal poll failure from a getResponse data item.
func pollTerminalError(item pollResponseItem) error {
	if item.Message != "" {
		if item.Code != "" {
			return fmt.Errorf("task failed (%s): %s", item.Code, item.Message)
		}
		return fmt.Errorf("task failed: %s", item.Message)
	}
	if item.Code != "" {
		return fmt.Errorf("task failed (%s)", item.Code)
	}
	return fmt.Errorf("task failed with status %q", item.Status)
}
