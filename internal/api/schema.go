package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const schemaBaseURL = "https://schemas.runware.ai/resolve/"

// schemaClient is used exclusively for schema endpoint fetches.
// A dedicated client with an explicit timeout avoids blocking indefinitely
// on a stalled connection, unlike http.DefaultClient which has no timeout.
var schemaClient = &http.Client{Timeout: 30 * time.Second}

// FetchModelSchema retrieves the request and response JSON Schema for the given
// AIR identifier from the Runware schema service. This endpoint is unauthenticated
// and separate from the inference API transport.
func FetchModelSchema(ctx context.Context, air string) (*ModelSchema, error) {
	return fetchModelSchema(ctx, air, schemaClient, schemaBaseURL)
}

// fetchModelSchema is the testable core of FetchModelSchema. Callers can inject
// a custom http.Client and base URL to point at an httptest server.
func fetchModelSchema(ctx context.Context, air string, client *http.Client, baseURL string) (*ModelSchema, error) {
	u := baseURL + url.PathEscape(air)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build schema request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("schema request failed: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck,gosec
		resp.Body.Close()              //nolint:errcheck,gosec
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		// handled below
	case http.StatusNotFound:
		return nil, fmt.Errorf("schema not found for: %s", air)
	default:
		return nil, fmt.Errorf("schema request returned status %d", resp.StatusCode)
	}

	var schema ModelSchema
	if err := json.NewDecoder(resp.Body).Decode(&schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema response: %w", err)
	}

	return &schema, nil
}

// MarshalYAML converts ModelSchema to a form the YAML encoder can handle.
// json.RawMessage fields are []byte, which would otherwise be base64-encoded
// by the YAML encoder. Unmarshaling them to any first produces native Go
// maps/slices that YAML can render as structured output.
func (s ModelSchema) MarshalYAML() (any, error) {
	var req, resp any
	if err := json.Unmarshal(s.RequestSchema, &req); err != nil {
		return nil, fmt.Errorf("failed to decode requestSchema for YAML: %w", err)
	}
	if err := json.Unmarshal(s.ResponseSchema, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode responseSchema for YAML: %w", err)
	}
	return map[string]any{
		"requestSchema":  req,
		"responseSchema": resp,
		"documentation":  s.Documentation,
	}, nil
}
