// Package serverless is a thin wrapper around the OpenAPI-generated client for
// the Runware Serverless control-plane API (api.serverless.runware.ai).
//
// Regenerate the client after updating api/serverless/openapi.yaml:
//
//	make generate-serverless
package serverless

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/runware/runware-cli/internal/agents"
	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/buildinfo"
)

// defaultTimeout bounds a single REST request.
const defaultTimeout = 30 * time.Second

// GpuType is the public catalogue entry for a supported GPU type.
type GpuType = gen.GpuType

// Client talks to the Serverless control-plane REST API.
type Client struct {
	apiKey string
	inner  *gen.ClientWithResponses
	logger *slog.Logger
}

// NewClient creates a Serverless API client for the given API key and base URL.
func NewClient(apiKey, baseURL string, logger *slog.Logger) *Client {
	return newClient(apiKey, baseURL, logger, &http.Client{Timeout: defaultTimeout})
}

func newClient(apiKey, baseURL string, logger *slog.Logger, httpClient gen.HttpRequestDoer) *Client {
	ua := buildinfo.UserAgent()
	if agent := agents.Detect(); agent != "" {
		ua += " agent/" + string(agent)
	}

	inner, err := gen.NewClientWithResponses(
		strings.TrimSuffix(baseURL, "/")+"/",
		gen.WithHTTPClient(httpClient),
		gen.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+apiKey)
			req.Header.Set("User-Agent", ua)
			req.Header.Set("Accept", "application/json")
			return nil
		}),
	)
	if err != nil {
		// NewClientWithResponses only fails if an option returns an error;
		// our options do not, so treat this as a programmer error.
		panic(fmt.Sprintf("serverless client: %v", err))
	}

	return &Client{
		apiKey: apiKey,
		inner:  inner,
		logger: logger,
	}
}

// ListGpuTypes returns the catalogue of supported GPU types and their pricing.
func (c *Client) ListGpuTypes(ctx context.Context) ([]GpuType, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListGpuTypesWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("list GPU types: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/gpu-types",
			"status", resp.StatusCode(),
			"body", string(resp.Body),
		)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("list GPU types: empty 200 response")
		}
		return resp.JSON200.Data, nil
	case http.StatusUnauthorized:
		return nil, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return nil, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	default:
		return nil, problemToError(nil, resp.StatusCode())
	}
}
