package api

import "errors"

var (
	// ErrUnauthorized is returned when the API rejects the request due to an invalid or missing API key.
	ErrUnauthorized = errors.New("unauthorized: invalid or missing API key")
	// ErrNoAPIKey is returned when no API key is present in the local configuration.
	ErrNoAPIKey = errors.New("no API key configured")
)

// IsAuthError checks if an API error is an authentication error.
func IsAuthError(err error) bool {
	if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrNoAPIKey) {
		return true
	}
	var apiErr APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == invalidAPIKeyCode
	}
	return false
}
