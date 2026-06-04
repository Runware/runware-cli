package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// APIResponse is the top-level envelope returned by the Runware API.
type APIResponse struct {
	Data   []json.RawMessage `json:"data"`
	Errors []RunwareError    `json:"errors,omitempty"`
}

// Transport handles the wire protocol for communicating with the Runware API.
// Implementations are responsible for connection lifecycle and request delivery.
type Transport interface {
	// Close tears down the connection and releases any associated resources.
	Close() error

	// Send transmits tasks to the API and returns the raw response data items,
	// with any API-level errors already converted to Go errors.
	Send(ctx context.Context, tasks []any) ([]json.RawMessage, error)
}

// DialContext dials a transport by scheme. scheme must be "ws" or "http".
// Returns an error if the scheme is not recognised.
func DialContext(ctx context.Context, scheme, apiKey, url string, logger *slog.Logger) (Transport, error) {
	switch scheme {
	case "ws":
		return DialWS(ctx, apiKey, url, logger)
	case "http":
		return DialHTTP(ctx, apiKey, url, logger)
	default:
		return nil, fmt.Errorf("unknown transport scheme %q: must be \"ws\" or \"http\"", scheme)
	}
}
