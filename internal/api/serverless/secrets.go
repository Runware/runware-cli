package serverless

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

// Secret is organisation-scoped secret metadata. The encrypted value is never returned.
type Secret = gen.Secret

// SecretAttachment is a secret attached to a deployment. The encrypted value is never returned.
type SecretAttachment = gen.SecretAttachment

// SecretAttach is the request body for attachDeploymentSecret.
type SecretAttach = gen.SecretAttach

// SecretCreate is the request body for createSecret.
type SecretCreate = gen.SecretCreate

// SecretUpdate is the request body for updateSecret.
type SecretUpdate = gen.SecretUpdate

// SecretType is the kind of organisation secret.
type SecretType = gen.SecretType

// SecretTypeGeneric is the only supported secret type (environment-variable secrets).
const SecretTypeGeneric SecretType = gen.Generic

// ListSecretsParams are optional filters for ListSecrets.
type ListSecretsParams = gen.ListSecretsParams

// ListDeploymentSecretsParams are optional filters for ListDeploymentSecrets.
type ListDeploymentSecretsParams = gen.ListDeploymentSecretsParams

// ListSecrets returns a page of organisation secret metadata.
func (c *Client) ListSecrets(ctx context.Context, params *ListSecretsParams) (Page[Secret], error) {
	if c.apiKey == "" {
		return Page[Secret]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListSecretsWithResponse(ctx, params)
	if err != nil {
		return Page[Secret]{}, fmt.Errorf("list secrets: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/secrets",
			"status", resp.StatusCode(),
		)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return pageOf[Secret](nil, nil), nil
		}
		return pageOf(resp.JSON200.Data, resp.JSON200.NextCursor), nil
	case http.StatusBadRequest:
		return Page[Secret]{}, problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
	case http.StatusUnauthorized:
		return Page[Secret]{}, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return Page[Secret]{}, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusUnprocessableEntity:
		return Page[Secret]{}, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	default:
		return Page[Secret]{}, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// CreateSecret creates an organisation-scoped secret. The value is encrypted at
// rest; the response is metadata only.
func (c *Client) CreateSecret(ctx context.Context, body SecretCreate) (*Secret, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.CreateSecretWithResponse(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("create secret: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/secrets",
			"status", resp.StatusCode(),
		)
	}

	switch resp.StatusCode() {
	case http.StatusCreated:
		if resp.JSON201 == nil {
			return nil, fmt.Errorf("create secret: empty 201 response")
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

// UpdateSecret replaces the value of an existing organisation secret.
func (c *Client) UpdateSecret(ctx context.Context, name string, body SecretUpdate) (*Secret, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.UpdateSecretWithResponse(ctx, name, body)
	if err != nil {
		return nil, fmt.Errorf("update secret: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/secrets/"+name,
			"status", resp.StatusCode(),
		)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("update secret: empty 200 response")
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

// DeleteSecret soft-deletes an organisation secret. Returns 409 while any
// deployment still attaches it.
func (c *Client) DeleteSecret(ctx context.Context, name string) error {
	if c.apiKey == "" {
		return transport.ErrNoAPIKey
	}

	resp, err := c.inner.DeleteSecretWithResponse(ctx, name)
	if err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/secrets/"+name,
			"status", resp.StatusCode(),
		)
	}

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

// ListDeploymentSecrets returns a page of secrets attached to a deployment.
func (c *Client) ListDeploymentSecrets(ctx context.Context, deploymentID string, params *ListDeploymentSecretsParams) (Page[SecretAttachment], error) {
	if c.apiKey == "" {
		return Page[SecretAttachment]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListDeploymentSecretsWithResponse(ctx, deploymentID, params)
	if err != nil {
		return Page[SecretAttachment]{}, fmt.Errorf("list deployment secrets: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/deployments/"+deploymentID+"/secrets",
			"status", resp.StatusCode(),
		)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return pageOf[SecretAttachment](nil, nil), nil
		}
		return pageOf(resp.JSON200.Data, resp.JSON200.NextCursor), nil
	case http.StatusBadRequest:
		return Page[SecretAttachment]{}, problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
	case http.StatusUnauthorized:
		return Page[SecretAttachment]{}, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return Page[SecretAttachment]{}, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return Page[SecretAttachment]{}, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	case http.StatusUnprocessableEntity:
		return Page[SecretAttachment]{}, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	default:
		return Page[SecretAttachment]{}, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// AttachDeploymentSecret records that an organisation secret is attached to a
// deployment. This is a control-plane association only in this API release.
func (c *Client) AttachDeploymentSecret(ctx context.Context, deploymentID string, body SecretAttach) error {
	if c.apiKey == "" {
		return transport.ErrNoAPIKey
	}

	resp, err := c.inner.AttachDeploymentSecretWithResponse(ctx, deploymentID, body)
	if err != nil {
		return fmt.Errorf("attach secret: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/deployments/"+deploymentID+"/secrets",
			"status", resp.StatusCode(),
		)
	}

	switch resp.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusBadRequest:
		return problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
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

// DetachDeploymentSecret removes a secret attachment from a deployment. It does
// not delete the organisation secret.
func (c *Client) DetachDeploymentSecret(ctx context.Context, deploymentID, secretName string) error {
	if c.apiKey == "" {
		return transport.ErrNoAPIKey
	}

	resp, err := c.inner.DetachDeploymentSecretWithResponse(ctx, deploymentID, secretName)
	if err != nil {
		return fmt.Errorf("detach secret: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/deployments/"+deploymentID+"/secrets/"+secretName,
			"status", resp.StatusCode(),
		)
	}

	switch resp.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusUnauthorized:
		return problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	case http.StatusUnprocessableEntity:
		return problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	default:
		return problemFromBody(resp.Body, resp.StatusCode())
	}
}
