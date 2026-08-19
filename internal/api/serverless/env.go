package serverless

import (
	"context"
	"fmt"
	"net/http"

	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

// EnvironmentVariable is a plain-text app environment variable.
type EnvironmentVariable = gen.EnvironmentVariable

// EnvironmentVariableUpdate is the request body for updateAppEnvironmentVariable.
type EnvironmentVariableUpdate = gen.EnvironmentVariableUpdate

// ListAppEnvironmentVariablesParams are optional filters for
// ListAppEnvironmentVariables.
type ListAppEnvironmentVariablesParams = gen.ListAppEnvironmentVariablesParams

// ListAppEnvironmentVariables returns a page of plain-text environment
// variables for an app. Values are included in the response.
func (c *Client) ListAppEnvironmentVariables(ctx context.Context, appID string, params *ListAppEnvironmentVariablesParams) (Page[EnvironmentVariable], error) {
	if c.apiKey == "" {
		return Page[EnvironmentVariable]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListAppEnvironmentVariablesWithResponse(ctx, appID, params)
	if err != nil {
		return Page[EnvironmentVariable]{}, fmt.Errorf("list environment variables: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return pageOf[EnvironmentVariable](nil, nil), nil
		}
		return pageOf(resp.JSON200.Data, resp.JSON200.NextCursor), nil
	case http.StatusUnauthorized:
		return Page[EnvironmentVariable]{}, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return Page[EnvironmentVariable]{}, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return Page[EnvironmentVariable]{}, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	default:
		return Page[EnvironmentVariable]{}, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// UpdateAppEnvironmentVariable creates or replaces one plain-text environment
// variable on an app.
func (c *Client) UpdateAppEnvironmentVariable(ctx context.Context, appID, key string, body EnvironmentVariableUpdate) (*EnvironmentVariable, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.UpdateAppEnvironmentVariableWithResponse(ctx, appID, key, body)
	if err != nil {
		return nil, fmt.Errorf("update environment variable: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("update environment variable: empty 200 response")
		}
		return resp.JSON200, nil
	case http.StatusBadRequest:
		return nil, problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
	case http.StatusUnauthorized:
		return nil, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return nil, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return nil, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	case http.StatusUnprocessableEntity:
		return nil, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	default:
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// DeleteAppEnvironmentVariable removes one plain-text environment variable
// from an app.
func (c *Client) DeleteAppEnvironmentVariable(ctx context.Context, appID, key string) error {
	if c.apiKey == "" {
		return transport.ErrNoAPIKey
	}

	resp, err := c.inner.DeleteAppEnvironmentVariableWithResponse(ctx, appID, key)
	if err != nil {
		return fmt.Errorf("delete environment variable: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	default:
		return problemFromBody(resp.Body, resp.StatusCode())
	}
}
