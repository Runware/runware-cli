package transport

import (
	"context"
	"encoding/json"
)

// APIResponse is the top-level envelope returned by the Runware API.
type APIResponse struct {
	Data   []json.RawMessage `json:"data"`
	Errors []APIError        `json:"errors,omitempty"`
}

// Transport handles the wire protocol for communicating with the Runware API.
// Implementations are responsible for connection lifecycle and request delivery.
// The concrete instance is created in the root command and injected via context.
type Transport interface {
	// Connect establishes a connection to the API.
	// For stateless transports (HTTP) this is a no-op.
	Connect(ctx context.Context) error

	// Disconnect closes the connection.
	// For stateless transports (HTTP) this is a no-op.
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
