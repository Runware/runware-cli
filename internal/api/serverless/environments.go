package serverless

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

// EnvironmentCreate is the request body for createEnvironment.
type EnvironmentCreate = gen.EnvironmentCreate

// EnvironmentUpdate is the request body for updateEnvironment. An unspecified
// field is left unchanged; a null one is cleared.
type EnvironmentUpdate = gen.EnvironmentUpdate

// EnvironmentDefaults are the organization defaults an environment inherits
// for each cap and the access setting it leaves empty.
type EnvironmentDefaults = gen.EnvironmentDefaults

// EnvironmentDefaultsFields are the organization defaults keyed by JSON field.
// Unlike EnvironmentDefaults they keep a field this client does not know, so
// writing them back with PutEnvironmentDefaults loses nothing.
type EnvironmentDefaultsFields map[string]json.RawMessage

// The EnvironmentDefaultsFields keys of the default caps.
const (
	EnvironmentDefaultsMaxWorkers = "maxConcurrentWorkers"
	EnvironmentDefaultsMaxGPUs    = "maxConcurrentGpus"
)

// CurrencyUSD is the only currency the platform bills in.
const CurrencyUSD = gen.USD

// CreateEnvironment creates one of the organization's environments.
func (c *Client) CreateEnvironment(ctx context.Context, body EnvironmentCreate) (*Environment, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.CreateEnvironmentWithResponse(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("create environment: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusCreated:
		if resp.JSON201 == nil {
			return nil, fmt.Errorf("create environment: empty 201 response")
		}
		return resp.JSON201, nil
	case http.StatusBadRequest:
		return nil, problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
	case http.StatusUnauthorized:
		return nil, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return nil, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusConflict:
		return nil, problemToError(resp.ApplicationproblemJSON409, http.StatusConflict)
	case http.StatusUnprocessableEntity:
		return nil, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	default:
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// UpdateEnvironment changes the fields body specifies on one of the
// organization's environments and leaves the rest as they are.
func (c *Client) UpdateEnvironment(ctx context.Context, name EnvironmentName, body EnvironmentUpdate) (*Environment, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.UpdateEnvironmentWithResponse(ctx, name, body)
	if err != nil {
		return nil, fmt.Errorf("update environment: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("update environment: empty 200 response")
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

// DeleteEnvironment deletes one of the organization's environments. Returns
// 409 while it still has an app or a secret, and for production.
func (c *Client) DeleteEnvironment(ctx context.Context, name EnvironmentName) error {
	if c.apiKey == "" {
		return transport.ErrNoAPIKey
	}

	resp, err := c.inner.DeleteEnvironmentWithResponse(ctx, name)
	if err != nil {
		return fmt.Errorf("delete environment: %w", err)
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
	case http.StatusConflict:
		return problemToError(resp.ApplicationproblemJSON409, http.StatusConflict)
	case http.StatusUnprocessableEntity:
		return problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	default:
		return problemFromBody(resp.Body, resp.StatusCode())
	}
}

// GetEnvironmentDefaults returns the organization defaults.
func (c *Client) GetEnvironmentDefaults(ctx context.Context) (*EnvironmentDefaults, error) {
	resp, err := c.getEnvironmentDefaults(ctx)
	if err != nil {
		return nil, err
	}
	return resp.JSON200, nil
}

// GetEnvironmentDefaultsFields returns the organization defaults field by
// field, to change and write back with PutEnvironmentDefaults.
func (c *Client) GetEnvironmentDefaultsFields(ctx context.Context) (EnvironmentDefaultsFields, error) {
	resp, err := c.getEnvironmentDefaults(ctx)
	if err != nil {
		return nil, err
	}
	var fields EnvironmentDefaultsFields
	if err := json.Unmarshal(resp.Body, &fields); err != nil {
		return nil, fmt.Errorf("get environment defaults: %w", err)
	}
	return fields, nil
}

func (c *Client) getEnvironmentDefaults(ctx context.Context) (*gen.GetEnvironmentDefaultsResponse, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.GetEnvironmentDefaultsWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("get environment defaults: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("get environment defaults: empty 200 response")
		}
		return resp, nil
	case http.StatusUnauthorized:
		return nil, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	default:
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// PutEnvironmentDefaults replaces the organization defaults as a whole: a cap
// body leaves out means no cap.
func (c *Client) PutEnvironmentDefaults(ctx context.Context, body EnvironmentDefaultsFields) (*EnvironmentDefaults, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("put environment defaults: %w", err)
	}
	resp, err := c.inner.PutEnvironmentDefaultsWithBodyWithResponse(ctx, "application/json", bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("put environment defaults: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("put environment defaults: empty 200 response")
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
	default:
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
}
