package api

import "github.com/runware/runware-cli/internal/api/transport"

// APIError is an error returned by the Runware API.
// Defined in the transport package; re-exported here for convenience.
type APIError = transport.APIError

var (
	// ErrUnauthorized is returned when the API rejects the request due to an invalid or missing API key.
	ErrUnauthorized = transport.ErrUnauthorized
	// ErrNoAPIKey is returned when no API key is present in the local configuration.
	ErrNoAPIKey = transport.ErrNoAPIKey
)

// IsAuthError reports whether err is an authentication error.
func IsAuthError(err error) bool { return transport.IsAuthError(err) }
