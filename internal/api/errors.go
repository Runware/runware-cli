package api

import (
	"encoding/json"
	"errors"
)

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

// APIError represents an error returned by the API.
type APIError struct {
	Code          string          `json:"code"`
	Message       string          `json:"message"`
	RawParameter  json.RawMessage `json:"parameter,omitempty"`
	Type          string          `json:"type,omitempty"`
	Documentation string          `json:"documentation,omitempty"`
	TaskUUID      string          `json:"taskUUID,omitempty"`
	AllowedValues []any           `json:"allowedValues,omitempty"`
}

// Parameter returns the parameter field as a string, handling both string and array forms from the API.
func (e APIError) Parameter() string {
	if len(e.RawParameter) == 0 {
		return ""
	}
	// Try string first
	var s string
	if err := json.Unmarshal(e.RawParameter, &s); err == nil {
		return s
	}
	// Try array of strings
	var arr []string
	if err := json.Unmarshal(e.RawParameter, &arr); err == nil {
		if len(arr) > 0 {
			return arr[0]
		}
		return ""
	}
	return string(e.RawParameter)
}

func (e APIError) Error() string {
	return e.Message
}
