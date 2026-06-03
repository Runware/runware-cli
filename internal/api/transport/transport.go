package transport

import (
	"context"
	"encoding/json"
)

// APIResponse is the top-level envelope returned by the Runware API.
type APIResponse struct {
	Data   []json.RawMessage `json:"data"`
	Errors []RunwareError    `json:"errors,omitempty"`
}

// Transport handles the wire protocol for communicating with the Runware API.
// Implementations are responsible for connection lifecycle and request delivery.
type Transport interface {
	// Connect establishes a connection to the API.
	Connect(ctx context.Context) error

	// Disconnect closes the connection.
	Disconnect() error

	// Send transmits tasks to the API and returns the raw response data items,
	// with any API-level errors already converted to Go errors.
	Send(ctx context.Context, tasks []any) ([]json.RawMessage, error)
}

type contextKey int

const transportContextKey contextKey = iota

// WithTransport returns a new context carrying t.
func WithTransport(ctx context.Context, t Transport) context.Context {
	return context.WithValue(ctx, transportContextKey, t)
}

// TransportFromContext retrieves the Transport stored by WithTransport.
// Returns nil if no transport is present.
func TransportFromContext(ctx context.Context) Transport {
	t, _ := ctx.Value(transportContextKey).(Transport)
	return t
}
