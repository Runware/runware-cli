package serverless

import (
	"context"
	"fmt"
	"net/http"

	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

// UsageSummary is GPU time and spend aggregated over one half-open window.
type UsageSummary = gen.UsageSummary

// UsageBucket is the usage that fell into one grouping key.
type UsageBucket = gen.UsageBucket

// UsageDimension is one grouping a usage summary can be aggregated by.
type UsageDimension = gen.UsageDimension

// UsageCoverage is which commercial capacity paid for a span of GPU time.
type UsageCoverage = gen.UsageCoverage

// MoneyAmount is an exact decimal amount in a currency's major unit.
type MoneyAmount = gen.MoneyAmount

// GetUsageSummaryParams are the query parameters of GET /v1/usage/summary.
type GetUsageSummaryParams = gen.GetUsageSummaryParams

// Usage summary grouping dimensions.
const (
	UsageDimensionApp      = gen.UsageDimensionApp
	UsageDimensionGpuType  = gen.UsageDimensionGpuType
	UsageDimensionDay      = gen.UsageDimensionDay
	UsageDimensionCoverage = gen.UsageDimensionCoverage
)

// GetUsageSummary returns GPU time and spend for the authenticated organization
// over a half-open window, grouped by the requested dimensions. The API derives
// every figure on read; usage it cannot price fails the whole request rather
// than being left out of the totals.
func (c *Client) GetUsageSummary(ctx context.Context, params *GetUsageSummaryParams) (*UsageSummary, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.GetUsageSummaryWithResponse(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("get usage summary: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("get usage summary: empty 200 response")
		}
		return resp.JSON200, nil
	case http.StatusBadRequest:
		return nil, problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
	case http.StatusUnauthorized:
		return nil, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return nil, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusUnprocessableEntity:
		return nil, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	case http.StatusInternalServerError:
		return nil, problemToError(resp.ApplicationproblemJSON500, http.StatusInternalServerError)
	case http.StatusServiceUnavailable:
		return nil, problemToError(resp.ApplicationproblemJSON503, http.StatusServiceUnavailable)
	default:
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
}
