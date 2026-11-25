package serverless

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

// OidcConnector is an organization-scoped OIDC connector. It holds no secret.
type OidcConnector = gen.OidcConnector

// AppOidcConnector is an OIDC connector attached to an app, with the subject
// its workers' tokens carry.
type AppOidcConnector = gen.AppOidcConnector

// OidcConnectorTest is one test of a connector's trust setup. It holds no
// token and no credential.
type OidcConnectorTest = gen.OidcConnectorTest

// OidcConnectorTestStatus is where a connector test is.
type OidcConnectorTestStatus = gen.OidcConnectorTestStatus

// The statuses of a connector test. Only pending is not final.
const (
	OidcConnectorTestStatusPending       = gen.OidcConnectorTestStatusPending
	OidcConnectorTestStatusOk            = gen.OidcConnectorTestStatusOk
	OidcConnectorTestStatusCloudError    = gen.OidcConnectorTestStatusCloudError
	OidcConnectorTestStatusPlatformError = gen.OidcConnectorTestStatusPlatformError
)

// defaultConnectorTestPollInterval is used when WaitAppOidcConnectorTest is
// called with a non-positive interval.
const defaultConnectorTestPollInterval = 2 * time.Second

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
	connector, _, err := c.GetOidcConnectorWithETag(ctx, name)
	return connector, err
}

// GetOidcConnectorWithETag returns one OIDC connector together with the ETag
// the API sent with it, to pass to UpdateOidcConnector as the If-Match
// precondition. The ETag is empty when the API sent none (an older API).
//
// A W/ prefix is dropped: the API only issues strong tags, and a CDN that
// compresses the response (Go's client asks for gzip) weakens the tag on the
// way. An API that predates accepting the weak form answers it with 400.
func (c *Client) GetOidcConnectorWithETag(ctx context.Context, name string) (*OidcConnector, string, error) {
	if c.apiKey == "" {
		return nil, "", transport.ErrNoAPIKey
	}

	resp, err := c.inner.GetOidcConnectorWithResponse(ctx, name)
	if err != nil {
		return nil, "", fmt.Errorf("get oidc connector: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusOK {
		return nil, "", problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON200 == nil {
		return nil, "", fmt.Errorf("get oidc connector: empty 200 response")
	}
	return resp.JSON200, strings.TrimPrefix(resp.HTTPResponse.Header.Get("ETag"), "W/"), nil
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
// rolls every live deployment that attaches it. A non-empty ifMatch is sent as
// the If-Match header, so the API applies the update only if the connector is
// unchanged since the ETag was read, and otherwise fails with a
// *transport.RunwareError whose StatusCode is 412. An empty ifMatch updates
// unconditionally.
func (c *Client) UpdateOidcConnector(ctx context.Context, name, ifMatch string, body OidcConnectorUpdate) (*OidcConnector, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	params := &gen.UpdateOidcConnectorParams{}
	if ifMatch != "" {
		params.IfMatch = &ifMatch
	}
	resp, err := c.inner.UpdateOidcConnectorWithResponse(ctx, name, params, body)
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

// StartAppOidcConnectorTest starts a test of the connector's trust setup for an
// app in the client's environment and returns it as the API accepted it: usually
// pending, or already platform_error when the platform cannot run it. A 429
// carries how long to wait before another test may start.
func (c *Client) StartAppOidcConnectorTest(ctx context.Context, appID, connectorName string) (*OidcConnectorTest, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.TestAppOidcConnectorWithResponse(ctx, appID, connectorName, &gen.TestAppOidcConnectorParams{
		Environment: c.environmentParam(),
	})
	if err != nil {
		return nil, fmt.Errorf("start oidc connector test: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusAccepted {
		return nil, withRetryAfter(problemFromBody(resp.Body, resp.StatusCode()), resp.HTTPResponse)
	}
	if resp.JSON202 == nil {
		return nil, fmt.Errorf("start oidc connector test: empty 202 response")
	}
	return resp.JSON202, nil
}

// GetAppOidcConnectorTest returns a connector test started in the client's
// environment.
func (c *Client) GetAppOidcConnectorTest(ctx context.Context, appID, connectorName, testID string) (*OidcConnectorTest, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}
	id, err := uuid.Parse(testID)
	if err != nil {
		return nil, fmt.Errorf("invalid test id %q: must be a UUID", testID)
	}

	resp, err := c.inner.GetAppOidcConnectorTestWithResponse(ctx, appID, connectorName, id, &gen.GetAppOidcConnectorTestParams{
		Environment: c.environmentParam(),
	})
	if err != nil {
		return nil, fmt.Errorf("get oidc connector test: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusOK {
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("get oidc connector test: empty 200 response")
	}
	return resp.JSON200, nil
}

// WaitAppOidcConnectorTest polls a connector test every interval until it is no
// longer pending, and returns it. A poll that fails on a server or network
// error is retried at the next interval, since the test goes on regardless;
// any other error ends the wait. It returns ctx's error when ctx ends first,
// as a *LastPollError when the poll before it had failed.
func (c *Client) WaitAppOidcConnectorTest(ctx context.Context, appID, connectorName string, test *OidcConnectorTest, interval time.Duration) (*OidcConnectorTest, error) {
	if interval <= 0 {
		interval = defaultConnectorTestPollInterval
	}

	var lastErr error
	ended := func() error {
		if lastErr != nil {
			return &LastPollError{Cause: ctx.Err(), Last: lastErr}
		}
		return ctx.Err()
	}
	for test.Status == OidcConnectorTestStatusPending {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ended()
		case <-timer.C:
		}

		next, err := c.GetAppOidcConnectorTest(ctx, appID, connectorName, test.Id.String())
		if err != nil {
			if ctx.Err() != nil {
				return nil, ended()
			}
			if retryablePoll(err) {
				lastErr = err
				continue
			}
			return nil, err
		}
		lastErr = nil
		test = next
	}
	return test, nil
}

// LastPollError is a wait that ended while its last poll had failed and was
// waiting to be retried. It unwraps to the context's error, so a timeout or an
// interrupt still reads as one.
type LastPollError struct {
	Cause error
	Last  error
}

func (e *LastPollError) Error() string {
	return fmt.Sprintf("%v (the last poll failed: %v)", e.Cause, e.Last)
}

func (e *LastPollError) Unwrap() error { return e.Cause }

// retryablePoll reports whether a failed poll may succeed at the next one: an
// answer with a transient status (408, 429, 500, 502, 503 or 504), a transport
// failure the client counts as transient, or a request whose connection failed
// before or while the answer was read. Any other status ends the wait, as does
// an answer the client could not decode: the next one would be the same.
func retryablePoll(err error) bool {
	if re, ok := errors.AsType[*transport.RunwareError](err); ok {
		if re.StatusCode == 0 {
			return re.Retryable
		}
		return transientPollStatus(re.StatusCode)
	}
	return transport.IsConnectionError(err)
}

// transientPollStatus reports whether a poll answered with status may succeed
// when repeated. The error code alone cannot tell: every status the client does
// not map, such as 405 or 410, counts as a server error.
func transientPollStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// withRetryAfter adds the response's Retry-After to a refusal that carries one,
// so a 429 says when to try again. The API sends whole seconds; a value that is
// not is shown as sent.
func withRetryAfter(err error, resp *http.Response) error {
	re, ok := errors.AsType[*transport.RunwareError](err)
	if !ok || resp == nil {
		return err
	}
	value := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if value == "" {
		return err
	}
	wait := value
	if secs, convErr := strconv.Atoi(value); convErr == nil && secs >= 0 {
		wait = (time.Duration(secs) * time.Second).String()
	}
	out := *re
	out.Message = fmt.Sprintf("%s\nRetry after %s", re.Message, wait)
	return &out
}
