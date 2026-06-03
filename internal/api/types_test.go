package api

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

var testUUID = uuid.MustParse("a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11")

const (
	jsonKeyModel     = "model"
	jsonKeyOutputFmt = "outputFormat"
	jsonKeyPosPrompt = "positivePrompt"
)

func TestImageInferenceRequestJSON(t *testing.T) {
	req := &ImageInferenceRequest{
		TaskType:       taskTypeImageInference,
		TaskUUID:       testUUID,
		PositivePrompt: "a cat",
		Model:          "runware:100@1",
		Width:          1024,
		Height:         1024,
		Steps:          28,
		NumberResults:  1,
		CFGScale:       3.5,
		Scheduler:      "euler",
		OutputFormat:   OutputFormatPNG,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Verify key field names match the Runware API
	expectedFields := []string{jsonKeyTaskType, jsonKeyTaskUUID, jsonKeyPosPrompt, jsonKeyModel, "width", "height", "steps", "numberResults", "CFGScale", "scheduler", jsonKeyOutputFmt}
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

	if result.TaskType != taskTypeImageInference {
		t.Errorf("taskType = %q, want %q", result.TaskType, taskTypeImageInference)
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

	if result.TaskType != taskTypePing {
		t.Errorf("taskType = %q, want %q", result.TaskType, taskTypePing)
	}
	if !result.Pong {
		t.Error("pong = false, want true")
	}
}

func TestAccountResultJSON(t *testing.T) {
	jsonData := `{
		"taskType": "accountManagement",
		"taskUUID": "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"organizationUUID": "b0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
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

func TestVideoInferenceRequestJSON(t *testing.T) {
	req := &VideoInferenceRequest{
		TaskType:       taskTypeVideoInference,
		TaskUUID:       testUUID,
		Model:          "klingai:5@3",
		PositivePrompt: "a cat on the moon",
		Width:          1280,
		Height:         720,
		Duration:       5.0,
		DeliveryMethod: DeliveryMethodAsync,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Verify key field names match the Runware API
	expectedFields := []string{jsonKeyTaskType, jsonKeyTaskUUID, jsonKeyModel, jsonKeyPosPrompt, "width", "height", "duration", "deliveryMethod"}
	for _, field := range expectedFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("missing expected field %q in JSON output", field)
		}
	}

	// Verify omitempty works for optional fields
	omittedFields := []string{"negativePrompt", "seed", "steps", "CFGScale", "frameImages", jsonKeyOutputFmt}
	for _, field := range omittedFields {
		if _, ok := parsed[field]; ok {
			t.Errorf("%s should be omitted when zero/empty", field)
		}
	}
}

func TestVideoInferenceRequestWithFrameImages(t *testing.T) {
	req := &VideoInferenceRequest{
		TaskType:       taskTypeVideoInference,
		TaskUUID:       testUUID,
		Model:          "klingai:5@3",
		PositivePrompt: "animate this",
		DeliveryMethod: DeliveryMethodAsync,
		FrameImages: []FrameImage{
			{InputImage: "data:image/png;base64,abc123", Frame: "first"},
			{InputImage: "data:image/png;base64,def456", Frame: "last"},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	frames, ok := parsed["frameImages"].([]any)
	if !ok {
		t.Fatal("frameImages is not an array")
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 frameImages, got %d", len(frames))
	}

	first := frames[0].(map[string]any)
	if first["frame"] != "first" {
		t.Errorf("first frame = %q, want %q", first["frame"], "first")
	}
}

func TestVideoInferenceResultJSON(t *testing.T) {
	vidUUID := uuid.MustParse("c0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11")
	jsonData := `{
		"taskType": "videoInference",
		"taskUUID": "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"videoUUID": "c0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"videoURL": "https://cdn.runware.ai/video/c0eebc99.mp4",
		"seed": 98765,
		"cost": 0.1234
	}`

	var result VideoInferenceResult
	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if result.TaskType != taskTypeVideoInference {
		t.Errorf("taskType = %q, want %q", result.TaskType, taskTypeVideoInference)
	}
	if result.VideoURL == "" {
		t.Error("videoURL is empty")
	}
	if result.VideoUUID != vidUUID {
		t.Errorf("videoUUID = %q, want %q", result.VideoUUID, vidUUID)
	}
	if result.Seed != 98765 {
		t.Errorf("seed = %d, want %d", result.Seed, 98765)
	}
	if result.Cost != 0.1234 {
		t.Errorf("cost = %f, want %f", result.Cost, 0.1234)
	}
}

func TestVideoInferenceResultWithMediaFields(t *testing.T) {
	mediaUUID := uuid.MustParse("d0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11")
	jsonData := `{
		"taskType": "videoInference",
		"taskUUID": "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"mediaUUID": "d0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"mediaURL": "https://cdn.runware.ai/media/d0eebc99.mp4"
	}`

	var result VideoInferenceResult
	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if result.MediaURL == "" {
		t.Error("mediaURL is empty")
	}
	if result.MediaUUID != mediaUUID {
		t.Errorf("mediaUUID = %q, want %q", result.MediaUUID, mediaUUID)
	}
	// videoURL should be empty when media fields are used
	if result.VideoURL != "" {
		t.Errorf("videoURL should be empty, got %q", result.VideoURL)
	}
}

func TestGetResponseRequestJSON(t *testing.T) {
	pollID := uuid.MustParse("e0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11")
	req := &GetResponseRequest{
		TaskType: taskTypeGetResponse,
		TaskUUID: pollID,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if parsed[jsonKeyTaskType] != "getResponse" {
		t.Errorf("taskType = %q, want %q", parsed[jsonKeyTaskType], "getResponse")
	}
	if parsed[jsonKeyTaskUUID] != pollID.String() {
		t.Errorf("taskUUID = %q, want %q", parsed[jsonKeyTaskUUID], pollID.String())
	}
}

func TestAudioInferenceRequestJSON(t *testing.T) {
	req := &AudioInferenceRequest{
		TaskType:       taskTypeAudioInference,
		TaskUUID:       testUUID,
		Model:          "elevenlabs:1@1",
		PositivePrompt: "jazz piano solo",
		Duration:       30,
		DeliveryMethod: DeliveryMethodAsync,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	expectedFields := []string{jsonKeyTaskType, jsonKeyTaskUUID, jsonKeyModel, jsonKeyPosPrompt, "duration", "deliveryMethod"}
	for _, field := range expectedFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("missing expected field %q in JSON output", field)
		}
	}

	omittedFields := []string{jsonKeyOutputFmt, "audioSettings"}
	for _, field := range omittedFields {
		if _, ok := parsed[field]; ok {
			t.Errorf("%s should be omitted when zero/empty", field)
		}
	}
}

func TestAudioInferenceRequestWithSettings(t *testing.T) {
	req := &AudioInferenceRequest{
		TaskType:       taskTypeAudioInference,
		TaskUUID:       testUUID,
		Model:          "elevenlabs:1@1",
		PositivePrompt: "ocean waves",
		Duration:       60,
		DeliveryMethod: DeliveryMethodAsync,
		AudioSettings: &AudioSettings{
			SampleRate: 48000,
			Bitrate:    320,
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	settings, ok := parsed["audioSettings"].(map[string]any)
	if !ok {
		t.Fatal("audioSettings is missing or not an object")
	}
	if settings["sampleRate"] != float64(48000) {
		t.Errorf("sampleRate = %v, want 48000", settings["sampleRate"])
	}
	if settings["bitrate"] != float64(320) {
		t.Errorf("bitrate = %v, want 320", settings["bitrate"])
	}
}

func TestAudioInferenceResultJSON(t *testing.T) {
	audUUID := uuid.MustParse("f0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11")
	jsonData := `{
		"taskType": "audioInference",
		"taskUUID": "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"audioUUID": "f0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"audioURL": "https://am.runware.ai/audio/ws/0.5/ai/f0eebc99.mp3",
		"cost": 0.045
	}`

	var result AudioInferenceResult
	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if result.TaskType != taskTypeAudioInference {
		t.Errorf("taskType = %q, want %q", result.TaskType, taskTypeAudioInference)
	}
	if result.AudioUUID != audUUID {
		t.Errorf("audioUUID = %q, want %q", result.AudioUUID, audUUID)
	}
	if result.AudioURL == "" {
		t.Error("audioURL is empty")
	}
	if result.Cost != 0.045 {
		t.Errorf("cost = %f, want %f", result.Cost, 0.045)
	}
}

func TestAudioInferenceResultProcessing(t *testing.T) {
	jsonData := `{
		"taskType": "audioInference",
		"taskUUID": "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"status": "processing"
	}`

	var result AudioInferenceResult
	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if result.Status != "processing" {
		t.Errorf("status = %q, want %q", result.Status, "processing")
	}
	if result.AudioURL != "" {
		t.Errorf("audioURL should be empty during processing, got %q", result.AudioURL)
	}
}

func TestModelSearchRequestJSON(t *testing.T) {
	req := &ModelSearchRequest{
		TaskType:     taskTypeModelSearch,
		TaskUUID:     testUUID,
		Search:       "flux",
		Category:     "checkpoint",
		Architecture: "flux1d",
		Limit:        10,
		Offset:       20,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	expectedFields := []string{jsonKeyTaskType, jsonKeyTaskUUID, "search", "category", "architecture", "limit", "offset"}
	for _, field := range expectedFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("missing expected field %q in JSON output", field)
		}
	}
}

func TestModelSearchRequestOmitempty(t *testing.T) {
	req := &ModelSearchRequest{
		TaskType: taskTypeModelSearch,
		TaskUUID: testUUID,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	omittedFields := []string{"search", "category", "architecture", "limit", "offset"}
	for _, field := range omittedFields {
		if _, ok := parsed[field]; ok {
			t.Errorf("%s should be omitted when zero/empty", field)
		}
	}
}

func TestModelSearchResponseJSON(t *testing.T) {
	jsonData := `{
		"taskType": "modelSearch",
		"taskUUID": "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"totalResults": 42,
		"results": [
			{
				"name": "FLUX.1 [dev]",
				"air": "civitai:618692@691639",
				"tags": ["flux", "base model"],
				"heroImage": "https://example.com/hero.jpg",
				"category": "checkpoint",
				"private": false,
				"version": "v1.0",
				"architecture": "flux1d",
				"nsfwLevel": 0,
				"defaultWidth": 1024,
				"defaultHeight": 1024,
				"defaultSteps": 28,
				"defaultScheduler": "euler",
				"defaultCFG": 3.5
			},
			{
				"name": "Some LoRA",
				"air": "civitai:12345@67890",
				"tags": ["lora"],
				"heroImage": "",
				"category": "lora",
				"private": true,
				"version": "v2.0",
				"architecture": "sdxl",
				"nsfwLevel": 1,
				"type": "lora",
				"defaultWeight": 0.8
			}
		]
	}`

	var result ModelSearchResponse
	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if result.TaskType != taskTypeModelSearch {
		t.Errorf("taskType = %q, want %q", result.TaskType, taskTypeModelSearch)
	}
	if result.TotalResults != 42 {
		t.Errorf("totalResults = %d, want %d", result.TotalResults, 42)
	}
	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}

	first := result.Results[0]
	if first.Name != "FLUX.1 [dev]" {
		t.Errorf("name = %q, want %q", first.Name, "FLUX.1 [dev]")
	}
	if first.AIR != "civitai:618692@691639" {
		t.Errorf("air = %q, want %q", first.AIR, "civitai:618692@691639")
	}
	if first.Category != "checkpoint" {
		t.Errorf("category = %q, want %q", first.Category, "checkpoint")
	}
	if first.Architecture != "flux1d" {
		t.Errorf("architecture = %q, want %q", first.Architecture, "flux1d")
	}
	if first.DefaultWidth != 1024 {
		t.Errorf("defaultWidth = %d, want %d", first.DefaultWidth, 1024)
	}
	if first.DefaultCFG != 3.5 {
		t.Errorf("defaultCFG = %f, want %f", first.DefaultCFG, 3.5)
	}
	if len(first.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(first.Tags))
	}

	second := result.Results[1]
	if !second.Private {
		t.Error("second result should be private")
	}
	if second.DefaultWeight != 0.8 {
		t.Errorf("defaultWeight = %f, want %f", second.DefaultWeight, 0.8)
	}
	if second.Type != "lora" {
		t.Errorf("type = %q, want %q", second.Type, "lora")
	}
}

func TestModelSearchResponseEmpty(t *testing.T) {
	jsonData := `{
		"taskType": "modelSearch",
		"taskUUID": "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"totalResults": 0,
		"results": []
	}`

	var result ModelSearchResponse
	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if result.TotalResults != 0 {
		t.Errorf("totalResults = %d, want 0", result.TotalResults)
	}
	if len(result.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(result.Results))
	}
}

func TestTextInferenceRequestJSON(t *testing.T) {
	req := &TextInferenceRequest{
		TaskType: taskTypeTextInference,
		TaskUUID: testUUID,
		Model:    "runware:qwen3-thinking@1",
		Messages: []Message{
			{Role: "user", Content: "What is Go?"},
		},
		MaxTokens:    500,
		Temperature:  0.8,
		SystemPrompt: "You are helpful",
		IncludeCost:  true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	expectedFields := []string{jsonKeyTaskType, jsonKeyTaskUUID, jsonKeyModel, "messages", "maxTokens", "temperature", "systemPrompt", "includeCost"}
	for _, field := range expectedFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("missing expected field %q in JSON output", field)
		}
	}

	// Verify omitempty works for optional fields
	omittedFields := []string{"topP", "topK", "seed", "stopSequences", "numberResults", jsonKeyOutputFmt}
	for _, field := range omittedFields {
		if _, ok := parsed[field]; ok {
			t.Errorf("%s should be omitted when zero/empty", field)
		}
	}

	// Verify messages structure
	msgs, ok := parsed["messages"].([]any)
	if !ok {
		t.Fatal("messages is not an array")
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]any)
	if msg["role"] != "user" {
		t.Errorf("message role = %q, want %q", msg["role"], "user")
	}
}

func TestTextInferenceResultJSON(t *testing.T) {
	jsonData := `{
		"taskType": "textInference",
		"taskUUID": "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"text": "Go is a programming language designed at Google.",
		"finishReason": "stop",
		"usage": {
			"inputTokens": 10,
			"outputTokens": 25,
			"totalTokens": 35
		},
		"cost": 0.000123
	}`

	var result TextInferenceResult
	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if result.TaskType != taskTypeTextInference {
		t.Errorf("taskType = %q, want %q", result.TaskType, taskTypeTextInference)
	}
	if result.Text == "" {
		t.Error("text is empty")
	}
	if result.FinishReason != "stop" {
		t.Errorf("finishReason = %q, want %q", result.FinishReason, "stop")
	}
	if result.Usage.TotalTokens != 35 {
		t.Errorf("totalTokens = %d, want %d", result.Usage.TotalTokens, 35)
	}
	if result.Cost != 0.000123 {
		t.Errorf("cost = %f, want %f", result.Cost, 0.000123)
	}
}
