package serverless

import (
	"context"
	"fmt"
	"net/http"

	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

// OidcConnector is an organization-scoped OIDC connector. It holds no secret.
type OidcConnector = gen.OidcConnector

// AppOidcConnector is an OIDC connector attached to an app, with the subject
// its workers' tokens carry.
type AppOidcConnector = gen.AppOidcConnector

// OidcConnectorCreate is the request body for createOidcConnector.
type OidcConnectorCreate = gen.OidcConnectorCreate

// OidcConnectorUpdate is the request body for updateOidcConnector.
type OidcConnectorUpdate = gen.OidcConnectorUpdate

// OidcConnectorAws is the provider block of an aws connector.
type OidcConnectorAws = gen.OidcConnectorAws

// OidcConnectorGcp is the provider block of a gcp connector.
type OidcConnectorGcp = gen.OidcConnectorGcp

// OidcConnectorKind is the provider an OIDC connector authenticates to.
type OidcConnectorKind = gen.OidcConnectorKind

// The kinds of OIDC connector.
const (
	OidcConnectorKindAws     = gen.OidcConnectorKindAws
	OidcConnectorKindGcp     = gen.OidcConnectorKindGcp
	OidcConnectorKindGeneric = gen.OidcConnectorKindGeneric
)

// ListOidcConnectorsParams are optional paging parameters for ListOidcConnectors.
type ListOidcConnectorsParams = gen.ListOidcConnectorsParams

// ListOidcConnectors returns a page of the organization's OIDC connectors.
// Connectors belong to the organization, so the request carries no environment.
func (c *Client) ListOidcConnectors(ctx context.Context, params ListOidcConnectorsParams) (Page[OidcConnector], error) {
	if c.apiKey == "" {
		return Page[OidcConnector]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListOidcConnectorsWithResponse(ctx, &params)
	if err != nil {
		return Page[OidcConnector]{}, fmt.Errorf("list oidc connectors: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusOK {
		return Page[OidcConnector]{}, problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON200 == nil {
		return pageOf[OidcConnector](nil, nil), nil
	}
	return pageOf(resp.JSON200.Data, resp.JSON200.NextCursor), nil
}

// GetOidcConnector returns one OIDC connector of the organization.
func (c *Client) GetOidcConnector(ctx context.Context, name string) (*OidcConnector, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.GetOidcConnectorWithResponse(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("get oidc connector: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusOK {
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("get oidc connector: empty 200 response")
	}
	return resp.JSON200, nil
}

// CreateOidcConnector creates an OIDC connector in the organization.
func (c *Client) CreateOidcConnector(ctx context.Context, body OidcConnectorCreate) (*OidcConnector, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.CreateOidcConnectorWithResponse(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("create oidc connector: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusCreated {
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON201 == nil {
		return nil, fmt.Errorf("create oidc connector: empty 201 response")
	}
	return resp.JSON201, nil
}

// UpdateOidcConnector replaces a connector's audience and provider block and
// rolls every live deployment that attaches it.
func (c *Client) UpdateOidcConnector(ctx context.Context, name string, body OidcConnectorUpdate) (*OidcConnector, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.UpdateOidcConnectorWithResponse(ctx, name, body)
	if err != nil {
		return nil, fmt.Errorf("update oidc connector: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusOK {
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("update oidc connector: empty 200 response")
	}
	return resp.JSON200, nil
}

// DeleteOidcConnector deletes a connector. The API answers 409 while any app
// still attaches it.
func (c *Client) DeleteOidcConnector(ctx context.Context, name string) error {
	if c.apiKey == "" {
		return transport.ErrNoAPIKey
	}

	resp, err := c.inner.DeleteOidcConnectorWithResponse(ctx, name)
	if err != nil {
		return fmt.Errorf("delete oidc connector: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusNoContent {
		return problemFromBody(resp.Body, resp.StatusCode())
	}
	return nil
}

// ListAppOidcConnectors returns the connectors attached to an app in the
// client's environment, each with the subject its tokens carry.
func (c *Client) ListAppOidcConnectors(ctx context.Context, appID string) ([]AppOidcConnector, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListAppOidcConnectorsWithResponse(ctx, appID, &gen.ListAppOidcConnectorsParams{
		Environment: c.environmentParam(),
	})
	if err != nil {
		return nil, fmt.Errorf("list app oidc connectors: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusOK {
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON200 == nil || resp.JSON200.Data == nil {
		return []AppOidcConnector{}, nil
	}
	return resp.JSON200.Data, nil
}

// AttachAppOidcConnector attaches a connector to an app in the client's
// environment and returns it as the app lists it, with its subject.
func (c *Client) AttachAppOidcConnector(ctx context.Context, appID, connectorName string) (*AppOidcConnector, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.AttachAppOidcConnectorWithResponse(ctx, appID, &gen.AttachAppOidcConnectorParams{
		Environment: c.environmentParam(),
	}, gen.OidcConnectorAttach{ConnectorName: connectorName})
	if err != nil {
		return nil, fmt.Errorf("attach oidc connector: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusCreated {
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON201 == nil {
		return nil, fmt.Errorf("attach oidc connector: empty 201 response")
	}
	return resp.JSON201, nil
}

// DetachAppOidcConnector removes a connector from an app in the client's
// environment. It does not delete the connector.
func (c *Client) DetachAppOidcConnector(ctx context.Context, appID, connectorName string) error {
	if c.apiKey == "" {
		return transport.ErrNoAPIKey
	}

	resp, err := c.inner.DetachAppOidcConnectorWithResponse(ctx, appID, connectorName, &gen.DetachAppOidcConnectorParams{
		Environment: c.environmentParam(),
	})
	if err != nil {
		return fmt.Errorf("detach oidc connector: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusNoContent {
		return problemFromBody(resp.Body, resp.StatusCode())
	}
	return nil
}
