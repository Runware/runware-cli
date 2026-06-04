package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const schemaBaseURL = "https://schemas.runware.ai/resolve/"

// FetchModelSchema retrieves the request and response JSON Schema for the given
// AIR identifier from the Runware schema service. This endpoint is unauthenticated
// and separate from the inference API transport.
func FetchModelSchema(ctx context.Context, air string) (*ModelSchema, error) {
	u := schemaBaseURL + url.PathEscape(air)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build schema request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
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
