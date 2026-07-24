// Package serverless is a REST client for the Runware Serverless control-plane
// API (api.serverless.runware.ai). It is separate from the inference task
// transport in internal/api/transport, which speaks the task-array protocol.
package serverless

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/runware/runware-cli/internal/agents"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/buildinfo"
)

// defaultTimeout bounds a single REST request.
const defaultTimeout = 30 * time.Second

// Client talks to the Serverless control-plane REST API.
type Client struct {
	apiKey     string
	baseURL    string
	userAgent  string
	logger     *slog.Logger
	httpClient *http.Client
}

// NewClient creates a Serverless API client for the given API key and base URL.
func NewClient(apiKey, baseURL string, logger *slog.Logger) *Client {
	ua := buildinfo.UserAgent()
	if agent := agents.Detect(); agent != "" {
		ua += " agent/" + string(agent)
	}
	return &Client{
		apiKey:     apiKey,
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		userAgent:  ua,
		logger:     logger,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// get performs an authenticated GET on path (e.g. "/v1/gpu-types") and decodes
// the JSON response body into out.
func (c *Client) get(ctx context.Context, path string, out any) error {
	if c.apiKey == "" {
		return transport.ErrNoAPIKey
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck,gosec

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"url", c.baseURL+path,
			"status", resp.StatusCode,
			"elapsed", time.Since(start).Round(time.Millisecond),
			"body", string(body),
		)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return parseError(body, resp.StatusCode)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to parse response (HTTP %d): %w", resp.StatusCode, err)
	}
	return nil
}
