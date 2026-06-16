package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// Transport schemes supported by the CLI.
const (
	SchemeWS   = "ws"
	SchemeHTTP = "http"
)

// ValidTransports returns the list of supported transport schemes.
func ValidTransports() []string {
	return []string{
		SchemeWS,
		SchemeHTTP,
	}
}

// ValidTransport reports whether s names a supported transport scheme.
// Matching is case-insensitive, consistent with output format validation.
func ValidTransport(s string) bool {
	switch strings.ToLower(s) {
	case SchemeWS, SchemeHTTP:
		return true
	default:
		return false
	}
}

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

// StreamSender is implemented by transports that can deliver multiple result
// frames for a single task over a persistent connection (e.g. WebSocket).
// Tasks like modelUpload stream pipeline status frames that are not available
// via getResponse polling.
type StreamSender interface {
	// SendStream transmits a single task and invokes onFrame for every result
	// frame delivered for its taskUUID, until onFrame reports done or returns
	// an error. API error frames for the task are returned as Go errors.
	SendStream(ctx context.Context, task any, onFrame func(frame json.RawMessage) (done bool, err error)) error
}

// DialContext dials a transport by scheme. scheme must be "ws" or "http"
// (case-insensitive). Returns an error if the scheme is not recognised.
func DialContext(ctx context.Context, scheme, apiKey, url string, logger *slog.Logger) (Transport, error) {
	switch strings.ToLower(scheme) {
	case SchemeWS:
		return DialWS(ctx, apiKey, url, logger)
	case SchemeHTTP:
		return DialHTTP(ctx, apiKey, url, logger)
	default:
		return nil, fmt.Errorf("unknown transport scheme %q: must be one of: %s", scheme, strings.Join(ValidTransports(), ", "))
	}
}
