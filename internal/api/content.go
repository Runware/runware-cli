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

const contentBaseURL = "https://content.runware.ai/models/"

// contentClient fetches from the public content metadata service. Like the
// schema endpoint it is unauthenticated and separate from the inference API.
var contentClient = &http.Client{Timeout: 30 * time.Second}

// PricingExample is one configuration/price pair in a model's pricing.
type PricingExample struct {
	Configuration string `json:"configuration"`
	Price         string `json:"price"`
}

// ModelPricing is the pricing summary for a model from the content service.
type ModelPricing struct {
	Model           string           `json:"model"`
	AIR             string           `json:"air"`
	Name            string           `json:"name"`
	Status          string           `json:"status"`
	ReleasedAt      string           `json:"releasedAt,omitempty"`
	PricingOverview string           `json:"pricingOverview,omitempty"`
	PricingExamples []PricingExample `json:"pricingExamples,omitempty"`
	Category        []string         `json:"category,omitempty"`
}

// ModelExample is one example request/response pair for a model.
type ModelExample struct {
	ID         string         `json:"id"`
	Title      string         `json:"title,omitempty"`
	Model      string         `json:"model"`
	Capability string         `json:"capability"`
	Asset      string         `json:"asset,omitempty"`
	Request    map[string]any `json:"request,omitempty"`
	Response   map[string]any `json:"response,omitempty"`
}

// FetchModelPricing retrieves the pricing summary for an identifier (AIR or
// model id) from the content service. The endpoint accepts either form.
func FetchModelPricing(ctx context.Context, air string) (*ModelPricing, error) {
	return fetchModelPricing(ctx, air, contentClient, contentBaseURL)
}

func fetchModelPricing(ctx context.Context, air string, client *http.Client, baseURL string) (*ModelPricing, error) {
	var pricing ModelPricing
	if err := fetchContentJSON(ctx, client, baseURL+url.PathEscape(air)+"/pricing", air, &pricing); err != nil {
		return nil, err
	}
	return &pricing, nil
}

// FetchModelExamples retrieves the example requests for an identifier (AIR or
// model id) from the content service.
func FetchModelExamples(ctx context.Context, air string) ([]ModelExample, error) {
	return fetchModelExamples(ctx, air, contentClient, contentBaseURL)
}

func fetchModelExamples(ctx context.Context, air string, client *http.Client, baseURL string) ([]ModelExample, error) {
	var examples []ModelExample
	if err := fetchContentJSON(ctx, client, baseURL+url.PathEscape(air)+"/examples", air, &examples); err != nil {
		return nil, err
	}
	return examples, nil
}

// fetchContentJSON does an unauthenticated GET and decodes the JSON body into
// out. A 404 is reported as a not-found error that names the identifier.
func fetchContentJSON(ctx context.Context, client *http.Client, u, air string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("failed to build content request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("content request failed: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck,gosec
		resp.Body.Close()              //nolint:errcheck,gosec
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		// handled below
	case http.StatusNotFound:
		return fmt.Errorf("model not found: %s", air)
	default:
		return fmt.Errorf("content request returned status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to parse content response: %w", err)
	}

	return nil
}
