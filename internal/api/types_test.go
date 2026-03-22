package api

import (
	"encoding/json"
	"testing"
)

func TestImageInferenceRequestJSON(t *testing.T) {
	req := &ImageInferenceRequest{
		TaskType:       "imageInference",
		TaskUUID:       "test-uuid-1234",
		PositivePrompt: "a cat",
		Model:          "runware:100@1",
		Width:          1024,
		Height:         1024,
		Steps:          28,
		NumberResults:  1,
		CFGScale:       3.5,
		Scheduler:      "euler",
		OutputFormat:   "png",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Verify key field names match the Runware API
	expectedFields := []string{"taskType", "taskUUID", "positivePrompt", "model", "width", "height", "steps", "numberResults", "CFGScale", "scheduler", "outputFormat"}
	for _, field := range expectedFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("missing expected field %q in JSON output", field)
		}
	}

	// Verify omitempty works
	if _, ok := parsed["negativePrompt"]; ok {
		t.Error("negativePrompt should be omitted when empty")
	}
	if _, ok := parsed["inputImage"]; ok {
		t.Error("inputImage should be omitted when empty")
	}
	if _, ok := parsed["seed"]; ok {
		t.Error("seed should be omitted when zero")
	}
}

func TestImageInferenceResultJSON(t *testing.T) {
	jsonData := `{
		"taskType": "imageInference",
		"taskUUID": "edcb45e8-055f-4581-aca9-099e657e70c8",
		"imageUUID": "cb5fd51a-e193-4eb5-a6ec-500fe0ddc197",
		"imageURL": "https://im.runware.ai/image/os/a08d21/ws/2/ii/cb5fd51a.jpg",
		"seed": 126128181
	}`

	var result ImageInferenceResult
	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if result.TaskType != "imageInference" {
		t.Errorf("taskType = %q, want %q", result.TaskType, "imageInference")
	}
	if result.ImageURL == "" {
		t.Error("imageURL is empty")
	}
	if result.Seed != 126128181 {
		t.Errorf("seed = %d, want %d", result.Seed, 126128181)
	}
}

func TestPingResultJSON(t *testing.T) {
	jsonData := `{"taskType": "ping", "pong": true}`

	var result PingResult
	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if result.TaskType != "ping" {
		t.Errorf("taskType = %q, want %q", result.TaskType, "ping")
	}
	if !result.Pong {
		t.Error("pong = false, want true")
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

	if resp.Errors[0].Code != "invalidApiKey" {
		t.Errorf("error code = %q, want %q", resp.Errors[0].Code, "invalidApiKey")
	}
}

func TestAccountResultJSON(t *testing.T) {
	jsonData := `{
		"taskType": "accountManagement",
		"taskUUID": "test-uuid",
		"organizationUUID": "org-uuid",
		"organizationName": "Test Org",
		"balance": 126.95,
		"usage": {
			"total": {"credits": 194.36, "requests": 14301},
			"today": {"credits": 0.001, "requests": 1},
			"last7Days": {"credits": 0.15, "requests": 18},
			"last30Days": {"credits": 3.24, "requests": 511}
		}
	}`

	var result AccountResult
	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if result.Balance != 126.95 {
		t.Errorf("balance = %f, want %f", result.Balance, 126.95)
	}
	if result.Usage.Total.Requests != 14301 {
		t.Errorf("total requests = %d, want %d", result.Usage.Total.Requests, 14301)
	}
}

func TestNewUUID(t *testing.T) {
	id1 := NewUUID()
	id2 := NewUUID()

	if id1 == "" {
		t.Error("NewUUID() returned empty string")
	}
	if id1 == id2 {
		t.Error("NewUUID() returned same value twice")
	}
	if len(id1) != 36 {
		t.Errorf("UUID length = %d, want 36", len(id1))
	}
}
