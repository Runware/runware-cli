package serverless

import (
	"context"
	"fmt"
	"net/http"

	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

// EnvironmentVariable is a plain-text deployment environment variable.
type EnvironmentVariable = gen.EnvironmentVariable

// EnvironmentVariableUpdate is the request body for updateDeploymentEnvironmentVariable.
type EnvironmentVariableUpdate = gen.EnvironmentVariableUpdate

// ListDeploymentEnvironmentVariablesParams are optional filters for
// ListDeploymentEnvironmentVariables.
type ListDeploymentEnvironmentVariablesParams = gen.ListDeploymentEnvironmentVariablesParams

// ListDeploymentEnvironmentVariables returns a page of plain-text environment
// variables for a deployment. Values are included in the response.
func (c *Client) ListDeploymentEnvironmentVariables(ctx context.Context, deploymentID string, params *ListDeploymentEnvironmentVariablesParams) (Page[EnvironmentVariable], error) {
	if c.apiKey == "" {
		return Page[EnvironmentVariable]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListDeploymentEnvironmentVariablesWithResponse(ctx, deploymentID, params)
	if err != nil {
		return Page[EnvironmentVariable]{}, fmt.Errorf("list environment variables: %w", err)
	}

	c.logResponse(ctx, "/v1/deployments/"+deploymentID+"/environment-variables", resp.StatusCode(), resp.Body)

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

// UpdateDeploymentEnvironmentVariable creates or replaces one plain-text
// environment variable on a deployment.
func (c *Client) UpdateDeploymentEnvironmentVariable(ctx context.Context, deploymentID, key string, body EnvironmentVariableUpdate) (*EnvironmentVariable, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.UpdateDeploymentEnvironmentVariableWithResponse(ctx, deploymentID, key, body)
	if err != nil {
		return nil, fmt.Errorf("update environment variable: %w", err)
	}

	c.logResponse(ctx, "/v1/deployments/"+deploymentID+"/environment-variables/"+key, resp.StatusCode(), resp.Body)

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

// DeleteDeploymentEnvironmentVariable removes one plain-text environment
// variable from a deployment.
func (c *Client) DeleteDeploymentEnvironmentVariable(ctx context.Context, deploymentID, key string) error {
	if c.apiKey == "" {
		return transport.ErrNoAPIKey
	}

	resp, err := c.inner.DeleteDeploymentEnvironmentVariableWithResponse(ctx, deploymentID, key)
	if err != nil {
		return fmt.Errorf("delete environment variable: %w", err)
	}

	c.logResponse(ctx, "/v1/deployments/"+deploymentID+"/environment-variables/"+key, resp.StatusCode(), resp.Body)

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
