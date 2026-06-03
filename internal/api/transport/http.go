package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// HTTPTransport implements Transport using the Runware REST API.
type HTTPTransport struct {
	apiKey     string
	baseURL    string
	logger     *slog.Logger
	httpClient *http.Client
}

// NewHTTPTransport creates an HTTP transport for the given API key and base URL.
func NewHTTPTransport(apiKey, baseURL string, logger *slog.Logger) *HTTPTransport {
	return &HTTPTransport{
		apiKey:  apiKey,
		baseURL: baseURL,
		logger:  logger,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Connect is a no-op for HTTP; the transport is stateless.
func (t *HTTPTransport) Connect(_ context.Context) error { return nil }

// Disconnect is a no-op for HTTP; the transport is stateless.
func (t *HTTPTransport) Disconnect() error { return nil }

// Send marshals tasks, POSTs to the API, and returns the response data items.
// API-level errors are converted to Go errors before returning.
func (t *HTTPTransport) Send(ctx context.Context, tasks []any) ([]json.RawMessage, error) {
	if t.apiKey == "" {
		return nil, ErrNoAPIKey
	}

	body, err := json.Marshal(tasks)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if t.logger != nil && t.logger.Enabled(ctx, slog.LevelDebug) {
		var pretty bytes.Buffer
		json.Indent(&pretty, body, "", "  ")                                 //nolint:errcheck,gosec
		t.logger.Debug("request", "url", t.baseURL, "body", pretty.String()) //nolint:errcheck,gosec
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.apiKey)

	start := time.Now()
	resp, err := t.httpClient.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck,gosec

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if t.logger != nil && t.logger.Enabled(ctx, slog.LevelDebug) {
		t.logger.Debug("response", "status", resp.StatusCode, "elapsed", elapsed.Round(time.Millisecond), "body", string(respBody)) //nolint:errcheck,gosec
	}

	var apiResp APIResponse
	if err = json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response (HTTP %d): %w", resp.StatusCode, err)
	}

	if len(apiResp.Errors) > 0 {
		if IsAuthError(apiResp.Errors[0]) {
			return nil, ErrUnauthorized
		}
		return nil, apiResp.Errors[0]
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("API returned HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 500))
	}

	return apiResp.Data, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
