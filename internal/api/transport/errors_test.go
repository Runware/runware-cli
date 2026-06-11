package transport

import (
	"encoding/json"
	"fmt"
	"reflect"
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

func TestRunwareErrorAPIFieldsPassthrough(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "minimal",
			raw:  `{"code":"serverError","message":"internal error"}`,
		},
		{
			name: "validation constraints",
			raw:  `{"code":"invalidCustomHeight","message":"Invalid height.","parameter":"height","type":"integer","min":128,"max":2048,"multiplier":64}`,
		},
		{
			name: "allowed values",
			raw:  `{"code":"invalidParameter","message":"bad","parameter":"model","allowedValues":["runware:101@1","runware:102@1"]}`,
		},
		{
			name: "with task context",
			raw:  `{"code":"taskFailed","message":"failed","taskUUID":"abc-123","taskType":"imageInference"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var re RunwareError
			if err := json.Unmarshal([]byte(tt.raw), &re); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			assertAPIFieldsEqual(t, tt.raw, &re)
		})
	}
}

func TestParseAPIErrorAPIFieldsPassthrough(t *testing.T) {
	const errObj = `{"code":"invalidCustomHeight","message":"Invalid height.","parameter":"height","min":128,"max":2048,"multiplier":64}`

	re := ParseAPIError([]byte(`{"errors":[`+errObj+`]}`), 400)
	assertAPIFieldsEqual(t, errObj, re)
	if re.Code != CodeValidation {
		t.Errorf("Code = %q, want %q", re.Code, CodeValidation)
	}
}

func assertAPIFieldsEqual(t *testing.T, raw string, re *RunwareError) {
	t.Helper()

	var want map[string]any
	if err := json.Unmarshal([]byte(raw), &want); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}

	got := re.APIFields()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("APIFields() = %#v, want %#v", got, want)
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
