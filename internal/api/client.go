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
)

// Client provides business-logic methods for the Runware API.
// It delegates wire-level communication to the injected Transport.
type Client struct {
	transport transport.Transport
	logger    *slog.Logger
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

// Run sends a single arbitrary task payload and returns all raw JSON responses.
// The caller is responsible for setting taskType, taskUUID, model, and any required fields.
// This is used by the dynamic run command where the exact request shape is not known at
// compile time — it is derived from the model's JSON Schema at runtime.
func (c *Client) Run(ctx context.Context, payload map[string]any) ([]json.RawMessage, error) {
	data, err := c.transport.Send(ctx, []any{payload})
	if err != nil {
		return nil, err
	}
	return data, nil
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
