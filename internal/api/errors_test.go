package api

import (
	"fmt"
	"testing"
)

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"ErrUnauthorized", ErrUnauthorized, true},
		{"ErrNoAPIKey", ErrNoAPIKey, true},
		{"API auth error", APIError{Code: "invalidApiKey", Message: "bad key"}, true},
		{"other API error", APIError{Code: "invalidParameter", Message: "bad param"}, false},
		{"generic error", fmt.Errorf("network timeout"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAuthError(tt.err)
			if got != tt.expected {
				t.Errorf("IsAuthError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}
