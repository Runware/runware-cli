package api

import "errors"

var (
	ErrUnauthorized = errors.New("unauthorized: invalid or missing API key")
	ErrNoAPIKey     = errors.New("no API key configured")
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
