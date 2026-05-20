package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Client is the interface for interacting with the Runware API.
type Client interface {
	Ping(ctx context.Context) (*PingResult, error)
	ImageInference(ctx context.Context, req *ImageInferenceRequest) ([]ImageInferenceResult, error)
	VideoInference(ctx context.Context, req *VideoInferenceRequest) ([]VideoInferenceResult, error)
	AudioInference(ctx context.Context, req *AudioInferenceRequest) ([]AudioInferenceResult, error)
	TextInference(ctx context.Context, req *TextInferenceRequest) ([]TextInferenceResult, error)
	GetResponse(ctx context.Context, taskUUID string) ([]json.RawMessage, error)
	AccountDetails(ctx context.Context) (*AccountResult, error)
	ModelSearch(ctx context.Context, req *ModelSearchRequest) (*ModelSearchResponse, error)
	// Raw sends arbitrary tasks and returns the raw response. Useful for --dry-run previewing.
	Raw(ctx context.Context, tasks []any) (*APIResponse, error)
}

// RestClient implements Client using the Runware REST API.
type RestClient struct {
	apiKey     string
	baseURL    string
	verbose    bool
	httpClient *http.Client
}

// NewClient creates a new REST API client.
func NewClient(apiKey, baseURL string, verbose bool) *RestClient {
	return &RestClient{
		apiKey:  apiKey,
		baseURL: baseURL,
		verbose: verbose,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// NewUUID generates a new UUIDv4 for task requests.
func NewUUID() string {
	return uuid.New().String()
}

// do sends the raw request to the API and returns the parsed response.
func (c *RestClient) do(ctx context.Context, tasks []any) (*APIResponse, error) {
	body, err := json.Marshal(tasks)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if c.verbose {
		var pretty bytes.Buffer
		_ = json.Indent(&pretty, body, "", "  ")
		_, _ = fmt.Fprintf(getStderr(), "→ POST %s\n%s\n", c.baseURL, pretty.String())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if c.verbose {
		_, _ = fmt.Fprintf(getStderr(), "← %d (%s)\n%s\n", resp.StatusCode, elapsed.Round(time.Millisecond), string(respBody))
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(apiResp.Errors) > 0 {
		first := apiResp.Errors[0]
		if first.Code == "invalidApiKey" {
			return &apiResp, ErrUnauthorized
		}
		return &apiResp, first
	}

	return &apiResp, nil
}

// Raw sends arbitrary tasks and returns the raw response.
func (c *RestClient) Raw(ctx context.Context, tasks []any) (*APIResponse, error) {
	return c.do(ctx, tasks)
}

// Ping checks API connectivity.
func (c *RestClient) Ping(ctx context.Context) (*PingResult, error) {
	tasks := []any{
		map[string]string{
			"taskType": "ping",
			"taskUUID": NewUUID(),
		},
	}

	resp, err := c.do(ctx, tasks)
	if err != nil {
		return nil, err
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("empty response from ping")
	}

	var result PingResult
	if err := json.Unmarshal(resp.Data[0], &result); err != nil {
		return nil, fmt.Errorf("failed to parse ping response: %w", err)
	}

	return &result, nil
}

// ImageInference runs an image inference task.
func (c *RestClient) ImageInference(ctx context.Context, req *ImageInferenceRequest) ([]ImageInferenceResult, error) {
	req.TaskType = "imageInference"
	if req.TaskUUID == "" {
		req.TaskUUID = NewUUID()
	}

	tasks := []any{req}

	resp, err := c.do(ctx, tasks)
	if err != nil {
		return nil, err
	}

	var results []ImageInferenceResult
	for _, raw := range resp.Data {
		var r ImageInferenceResult
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("failed to parse image result: %w", err)
		}
		results = append(results, r)
	}

	return results, nil
}

// AccountDetails fetches account information.
func (c *RestClient) AccountDetails(ctx context.Context) (*AccountResult, error) {
	tasks := []any{
		map[string]string{
			"taskType":  "accountManagement",
			"taskUUID":  NewUUID(),
			"operation": "getDetails",
		},
	}

	resp, err := c.do(ctx, tasks)
	if err != nil {
		return nil, err
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("empty response from account details")
	}

	var result AccountResult
	if err := json.Unmarshal(resp.Data[0], &result); err != nil {
		return nil, fmt.Errorf("failed to parse account response: %w", err)
	}

	return &result, nil
}

// VideoInference submits a video inference task.
// Video generation is async: the API returns immediately and results are polled via GetResponse.
func (c *RestClient) VideoInference(ctx context.Context, req *VideoInferenceRequest) ([]VideoInferenceResult, error) {
	req.TaskType = "videoInference"
	if req.TaskUUID == "" {
		req.TaskUUID = NewUUID()
	}
	if req.DeliveryMethod == "" {
		req.DeliveryMethod = "async"
	}

	tasks := []any{req}

	resp, err := c.do(ctx, tasks)
	if err != nil {
		return nil, err
	}

	var results []VideoInferenceResult
	for _, raw := range resp.Data {
		var r VideoInferenceResult
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("failed to parse video result: %w", err)
		}
		results = append(results, r)
	}

	return results, nil
}

// AudioInference submits an audio inference task.
func (c *RestClient) AudioInference(ctx context.Context, req *AudioInferenceRequest) ([]AudioInferenceResult, error) {
	req.TaskType = "audioInference"
	if req.TaskUUID == "" {
		req.TaskUUID = NewUUID()
	}

	tasks := []any{req}

	resp, err := c.do(ctx, tasks)
	if err != nil {
		return nil, err
	}

	var results []AudioInferenceResult
	for _, raw := range resp.Data {
		var r AudioInferenceResult
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("failed to parse audio result: %w", err)
		}
		results = append(results, r)
	}

	return results, nil
}

// TextInference runs a text inference task.
func (c *RestClient) TextInference(ctx context.Context, req *TextInferenceRequest) ([]TextInferenceResult, error) {
	req.TaskType = "textInference"
	if req.TaskUUID == "" {
		req.TaskUUID = NewUUID()
	}

	tasks := []any{req}

	resp, err := c.do(ctx, tasks)
	if err != nil {
		return nil, err
	}

	var results []TextInferenceResult
	for _, raw := range resp.Data {
		var r TextInferenceResult
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("failed to parse text result: %w", err)
		}
		results = append(results, r)
	}

	return results, nil
}

// GetResponse polls for async task results.
func (c *RestClient) GetResponse(ctx context.Context, taskUUID string) ([]json.RawMessage, error) {
	tasks := []any{
		&GetResponseRequest{
			TaskType: "getResponse",
			TaskUUID: taskUUID,
		},
	}

	resp, err := c.do(ctx, tasks)
	if err != nil {
		return nil, err
	}

	return resp.Data, nil
}

// ModelSearch searches for available models.
func (c *RestClient) ModelSearch(ctx context.Context, req *ModelSearchRequest) (*ModelSearchResponse, error) {
	req.TaskType = "modelSearch"
	if req.TaskUUID == "" {
		req.TaskUUID = NewUUID()
	}

	tasks := []any{req}

	resp, err := c.do(ctx, tasks)
	if err != nil {
		return nil, err
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("empty response from model search")
	}

	var result ModelSearchResponse
	if err := json.Unmarshal(resp.Data[0], &result); err != nil {
		return nil, fmt.Errorf("failed to parse model search response: %w", err)
	}

	return &result, nil
}
