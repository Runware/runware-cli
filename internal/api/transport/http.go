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

	"github.com/runware/runware-cli/internal/agents"
	"github.com/runware/runware-cli/internal/buildinfo"
)

// HTTPTransport implements Transport using the Runware REST API.
type HTTPTransport struct {
	apiKey     string
	baseURL    string
	userAgent  string
	logger     *slog.Logger
	httpClient *http.Client
}

// httpTransportOptions holds the configurable settings for HTTPTransport.
type httpTransportOptions struct {
	timeout    time.Duration
	httpClient *http.Client
}

// HTTPTransportOption is a functional option for NewHTTPTransport.
type HTTPTransportOption func(*httpTransportOptions)

// WithTimeout sets the HTTP client timeout. Ignored if WithHTTPClient is also provided.
func WithTimeout(d time.Duration) HTTPTransportOption {
	return func(o *httpTransportOptions) {
		o.timeout = d
	}
}

// WithHTTPClient replaces the default HTTP client entirely. When provided,
// WithTimeout has no effect — the caller is responsible for the client's timeout.
func WithHTTPClient(c *http.Client) HTTPTransportOption {
	return func(o *httpTransportOptions) {
		o.httpClient = c
	}
}

// NewHTTPTransport creates an HTTP transport for the given API key and base URL.
// Default timeout is 120s. Use WithTimeout or WithHTTPClient to override.
func NewHTTPTransport(apiKey, baseURL string, logger *slog.Logger, opts ...HTTPTransportOption) *HTTPTransport {
	o := httpTransportOptions{
		timeout: 120 * time.Second,
	}
	for _, opt := range opts {
		opt(&o)
	}
	client := o.httpClient
	if client == nil {
		client = &http.Client{Timeout: o.timeout}
	}
	ua := buildinfo.UserAgent()
	if agent := agents.Detect(); agent != "" {
		ua += " agent/" + string(agent)
	}
	return &HTTPTransport{
		apiKey:     apiKey,
		baseURL:    baseURL,
		userAgent:  ua,
		logger:     logger,
		httpClient: client,
	}
}

// Connect is a no-op for HTTP; the transport is stateless.
func (t *HTTPTransport) Connect(_ context.Context) error {
	return nil
}

// Disconnect is a no-op for HTTP; the transport is stateless.
func (t *HTTPTransport) Disconnect() error {
	return nil
}

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
	req.Header.Set("User-Agent", t.userAgent)

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
		e := apiResp.Errors[0]
		e.StatusCode = resp.StatusCode
		return nil, &e
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, CreateRunwareError(
			"serverError",
			fmt.Sprintf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
			RunwareErrorDetails{StatusCode: resp.StatusCode},
		)
	}

	return apiResp.Data, nil
}
