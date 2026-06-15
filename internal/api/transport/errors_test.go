package transport

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"ErrNoAPIKey", ErrNoAPIKey, true},
		{"API auth error", CreateRunwareError("invalidApiKey", "bad key", RunwareErrorDetails{}), true},
		{"other API error", CreateRunwareError("invalidParameter", "bad param", RunwareErrorDetails{}), false},
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

func TestAPIErrorJSON(t *testing.T) {
	jsonData := `{
		"data": [],
		"errors": [{
			"code": "invalidApiKey",
			"message": "Invalid API key. Get one at https://my.runware.ai/signup",
			"parameter": "apiKey",
			"type": "string"
		}]
	}`

	var resp APIResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if len(resp.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(resp.Errors))
	}

	if resp.Errors[0].Code != CodeAuth {
		t.Errorf("error code = %q, want %q", resp.Errors[0].Code, CodeAuth)
	}
}

func TestAPIErrorParameterString(t *testing.T) {
	jsonData := `{
		"data": [],
		"errors": [{
			"code": "invalidParameter",
			"message": "bad param",
			"parameter": "model"
		}]
	}`

	var resp APIResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if len(resp.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(resp.Errors))
	}

	param := resp.Errors[0].Parameter
	if param != "model" {
		t.Errorf("Parameter = %q, want %q", param, "model")
	}
}

func TestAPIErrorParameterArray(t *testing.T) {
	jsonData := `{
		"data": [],
		"errors": [{
			"code": "invalidParameter",
			"message": "bad params",
			"parameter": ["positivePrompt", "model"]
		}]
	}`

	var resp APIResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if len(resp.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(resp.Errors))
	}

	param := resp.Errors[0].Parameter
	if param != "positivePrompt" {
		t.Errorf("Parameter = %q, want %q", param, "positivePrompt")
	}
}

// TestAPIErrorAllowedValuesObject guards against a regression where the API
// returns "allowedValues" as an object (e.g. an aspect-ratio→dimensions map)
// rather than an array. A too-strict []any field previously failed the whole
// envelope unmarshal, losing the real API message and hanging the ws poll loop.
func TestAPIErrorAllowedValuesObject(t *testing.T) {
	jsonData := `{
		"data": [],
		"errors": [{
			"code": "kontextMissingDimensionsOrReferenceImage",
			"message": "The width/height parameters are required.",
			"parameter": ["width", "height"],
			"type": "integer",
			"allowedValues": {"1:1": "1024x1024", "3:2": "1248x832"}
		}]
	}`

	var resp APIResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(resp.Errors))
	}
	if resp.Errors[0].Message != "The width/height parameters are required." {
		t.Errorf("Message = %q, want the API message preserved", resp.Errors[0].Message)
	}
}

// TestAPIErrorAllowedValuesArray confirms the array shape still parses.
func TestAPIErrorAllowedValuesArray(t *testing.T) {
	jsonData := `{
		"data": [],
		"errors": [{
			"code": "invalidParameter",
			"message": "bad value",
			"allowedValues": ["a", "b", "c"]
		}]
	}`

	var resp APIResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(resp.Errors))
	}
}

func TestAPIErrorParameterMissing(t *testing.T) {
	jsonData := `{
		"data": [],
		"errors": [{
			"code": "serverError",
			"message": "internal error"
		}]
	}`

	var resp APIResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	param := resp.Errors[0].Parameter
	if param != "" {
		t.Errorf("Parameter = %q, want empty string", param)
	}
}
