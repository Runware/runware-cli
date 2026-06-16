package transport

import (
	"encoding/json"
	"fmt"
	"slices"
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

func TestAPIErrorAllowedValuesArray(t *testing.T) {
	jsonData := `{
		"data": [],
		"errors": [{
			"code": "invalidCategory",
			"message": "bad category",
			"parameter": "category",
			"allowedValues": ["checkpoint", "lora"]
		}]
	}`

	var resp APIResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	got := resp.Errors[0].AllowedValues
	want := []string{"checkpoint", "lora"}
	if !slices.Equal(got, want) {
		t.Errorf("AllowedValues = %v, want %v", got, want)
	}
}

func TestAPIErrorAllowedValuesObject(t *testing.T) {
	// The API sometimes sends allowedValues as an object with numeric string
	// keys instead of an array; this must not fail the envelope unmarshal.
	jsonData := `{
		"data": [],
		"errors": [{
			"code": "invalidCategory",
			"message": "bad category",
			"parameter": "category",
			"allowedValues": {"0": "checkpoint", "1": "lora", "2": "controlnet", "10": "text"}
		}]
	}`

	var resp APIResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	got := resp.Errors[0].AllowedValues
	want := []string{"checkpoint", "lora", "controlnet", "text"}
	if !slices.Equal(got, want) {
		t.Errorf("AllowedValues = %v, want %v", got, want)
	}
}

func TestAPIErrorAllowedValuesUnrecognizedShape(t *testing.T) {
	jsonData := `{
		"data": [],
		"errors": [{
			"code": "invalidCategory",
			"message": "bad category",
			"allowedValues": "checkpoint"
		}]
	}`

	var resp APIResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got := resp.Errors[0].AllowedValues; got != nil {
		t.Errorf("AllowedValues = %v, want nil", got)
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
