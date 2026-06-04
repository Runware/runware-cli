package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

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
