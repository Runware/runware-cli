// Package serverless is a thin wrapper around the OpenAPI-generated client for
// the Runware Serverless control-plane API (api.serverless.runware.ai).
//
// Regenerate the client after updating api/serverless/openapi.yaml:
//
//	make generate-serverless
package serverless

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/runware/runware-cli/internal/agents"
	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/buildinfo"
)

// defaultTimeout bounds a single REST request.
const defaultTimeout = 30 * time.Second

// createDeploymentTimeout bounds createDeployment, which uploads a base64 zip
// and can exceed the default request timeout on slow links or larger codebases.
const createDeploymentTimeout = 5 * time.Minute

// GpuType is the public catalogue entry for a supported GPU type.
type GpuType = gen.GpuType

// Deployment is a serverless application (API: deployment).
type Deployment = gen.Deployment

// DeploymentCreate is the request body for createDeployment.
type DeploymentCreate = gen.DeploymentCreate

// DeploymentSourceUpsert selects the initial version creation path.
type DeploymentSourceUpsert = gen.DeploymentSourceUpsert

// CodeSourceUpsert is a code-based deployment source.
type CodeSourceUpsert = gen.CodeSourceUpsert

// CodebaseSource is the zipped customer code payload.
type CodebaseSource = gen.CodebaseSource

// WorkerConfigCreate is the worker configuration supplied at create time.
type WorkerConfigCreate = gen.WorkerConfigCreate

// ListDeploymentsParams are optional filters for ListDeployments.
type ListDeploymentsParams = gen.ListDeploymentsParams

// ListEndpointsParams are optional filters for ListEndpoints.
type ListEndpointsParams = gen.ListEndpointsParams

// ListVersionsParams are optional filters for ListVersions.
type ListVersionsParams = gen.ListVersionsParams

// ListWorkersParams are optional filters for ListWorkers.
type ListWorkersParams = gen.ListWorkersParams

// Endpoint is a deployment HTTP endpoint.
type Endpoint = gen.Endpoint

// Version is a deployed application version.
type Version = gen.Version

// Worker is a runtime worker instance.
type Worker = gen.Worker

// DeploymentStatus is a deployment lifecycle status.
type DeploymentStatus = gen.DeploymentStatus

// WorkerStatus is a worker lifecycle status.
type WorkerStatus = gen.WorkerStatus

// Limit is a page size for cursor-paginated list endpoints.
type Limit = gen.Limit

// Cursor is an opaque pagination cursor.
type Cursor = gen.Cursor

// Page is one page of results from a cursor-paginated list endpoint.
type Page[T any] struct {
	Data       []T     `json:"data"`
	NextCursor *string `json:"nextCursor,omitempty"`
}

// DeploymentSourceTypeCode is deploymentSource.type = "code".
const DeploymentSourceTypeCode = gen.Code

func pageOf[T any](data *[]T, nextCursor *string) Page[T] {
	if data == nil || *data == nil {
		return Page[T]{
			Data:       []T{},
			NextCursor: nextCursor,
		}
	}
	return Page[T]{
		Data:       *data,
		NextCursor: nextCursor,
	}
}

// Client talks to the Serverless control-plane REST API.
type Client struct {
	apiKey  string
	baseURL string
	doer    gen.HttpRequestDoer
	inner   *gen.ClientWithResponses
	logger  *slog.Logger
}

// NewClient creates a Serverless API client for the given API key and base URL.
func NewClient(apiKey, baseURL string, logger *slog.Logger) *Client {
	return newClient(apiKey, baseURL, logger, &http.Client{Timeout: defaultTimeout})
}

func newClient(apiKey, baseURL string, logger *slog.Logger, httpClient gen.HttpRequestDoer) *Client {
	base := strings.TrimSuffix(baseURL, "/") + "/"
	return &Client{
		apiKey:  apiKey,
		baseURL: base,
		doer:    httpClient,
		inner:   newGeneratedClient(apiKey, base, httpClient),
		logger:  logger,
	}
}

func newGeneratedClient(apiKey, baseURL string, httpClient gen.HttpRequestDoer) *gen.ClientWithResponses {
	ua := buildinfo.UserAgent()
	if agent := agents.Detect(); agent != "" {
		ua += " agent/" + string(agent)
	}

	inner, err := gen.NewClientWithResponses(
		baseURL,
		gen.WithHTTPClient(httpClient),
		gen.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+apiKey)
			req.Header.Set("User-Agent", ua)
			req.Header.Set("Accept", "application/json")
			return nil
		}),
	)
	if err != nil {
		// NewClientWithResponses only fails if an option returns an error;
		// our options do not, so treat this as a programmer error.
		panic(fmt.Sprintf("serverless client: %v", err))
	}
	return inner
}

// createInner returns a generated client suitable for createDeployment.
// When the underlying doer is an *http.Client with a positive Timeout shorter
// than createDeploymentTimeout, a clone with the longer timeout is used so
// large uploads are not cut off. A zero Timeout (no deadline) is left unchanged.
func (c *Client) createInner() *gen.ClientWithResponses {
	hc, ok := c.doer.(*http.Client)
	if !ok {
		return c.inner
	}
	if hc.Timeout == 0 || hc.Timeout >= createDeploymentTimeout {
		return c.inner
	}
	cloned := *hc
	cloned.Timeout = createDeploymentTimeout
	return newGeneratedClient(c.apiKey, c.baseURL, &cloned)
}

// ListGpuTypes returns the catalogue of supported GPU types and their pricing.
func (c *Client) ListGpuTypes(ctx context.Context) ([]GpuType, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListGpuTypesWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("list GPU types: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/gpu-types",
			"status", resp.StatusCode(),
			"body", string(resp.Body),
		)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("list GPU types: empty 200 response")
		}
		return resp.JSON200.Data, nil
	case http.StatusUnauthorized:
		return nil, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return nil, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	default:
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// CreateDeployment creates a new deployment and starts its initial rollout.
func (c *Client) CreateDeployment(ctx context.Context, body DeploymentCreate) (*Deployment, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.createInner().CreateDeploymentWithResponse(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("create deployment: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/deployments",
			"status", resp.StatusCode(),
			"body", string(resp.Body),
		)
	}

	switch resp.StatusCode() {
	case http.StatusCreated:
		if resp.JSON201 == nil {
			return nil, fmt.Errorf("create deployment: empty 201 response")
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

// ListDeployments returns a page of deployments for the authenticated organisation.
func (c *Client) ListDeployments(ctx context.Context, params *ListDeploymentsParams) (Page[Deployment], error) {
	if c.apiKey == "" {
		return Page[Deployment]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListDeploymentsWithResponse(ctx, params)
	if err != nil {
		return Page[Deployment]{}, fmt.Errorf("list deployments: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/deployments",
			"status", resp.StatusCode(),
			"body", string(resp.Body),
		)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return Page[Deployment]{Data: []Deployment{}}, nil
		}
		return pageOf(resp.JSON200.Data, resp.JSON200.NextCursor), nil
	case http.StatusUnauthorized:
		return Page[Deployment]{}, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return Page[Deployment]{}, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	default:
		return Page[Deployment]{}, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// GetDeployment returns a single deployment by ID.
func (c *Client) GetDeployment(ctx context.Context, deploymentID string) (*Deployment, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.GetDeploymentWithResponse(ctx, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("get deployment: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/deployments/"+deploymentID,
			"status", resp.StatusCode(),
			"body", string(resp.Body),
		)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("get deployment: empty 200 response")
		}
		return resp.JSON200, nil
	case http.StatusUnauthorized:
		return nil, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return nil, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return nil, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	default:
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// ListEndpoints returns a page of endpoints for a deployment.
func (c *Client) ListEndpoints(ctx context.Context, deploymentID string, params *ListEndpointsParams) (Page[Endpoint], error) {
	if c.apiKey == "" {
		return Page[Endpoint]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListEndpointsWithResponse(ctx, deploymentID, params)
	if err != nil {
		return Page[Endpoint]{}, fmt.Errorf("list endpoints: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/deployments/"+deploymentID+"/endpoints",
			"status", resp.StatusCode(),
			"body", string(resp.Body),
		)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return Page[Endpoint]{Data: []Endpoint{}}, nil
		}
		return pageOf(resp.JSON200.Data, resp.JSON200.NextCursor), nil
	case http.StatusUnauthorized:
		return Page[Endpoint]{}, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return Page[Endpoint]{}, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return Page[Endpoint]{}, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	default:
		return Page[Endpoint]{}, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// ListVersions returns a page of versions for a deployment.
func (c *Client) ListVersions(ctx context.Context, deploymentID string, params *ListVersionsParams) (Page[Version], error) {
	if c.apiKey == "" {
		return Page[Version]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListVersionsWithResponse(ctx, deploymentID, params)
	if err != nil {
		return Page[Version]{}, fmt.Errorf("list versions: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/deployments/"+deploymentID+"/versions",
			"status", resp.StatusCode(),
			"body", string(resp.Body),
		)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return Page[Version]{Data: []Version{}}, nil
		}
		return pageOf(resp.JSON200.Data, resp.JSON200.NextCursor), nil
	case http.StatusUnauthorized:
		return Page[Version]{}, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return Page[Version]{}, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return Page[Version]{}, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	default:
		return Page[Version]{}, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// ListWorkers returns a page of workers for a deployment.
func (c *Client) ListWorkers(ctx context.Context, deploymentID string, params *ListWorkersParams) (Page[Worker], error) {
	if c.apiKey == "" {
		return Page[Worker]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListWorkersWithResponse(ctx, deploymentID, params)
	if err != nil {
		return Page[Worker]{}, fmt.Errorf("list workers: %w", err)
	}

	if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", "/v1/deployments/"+deploymentID+"/workers",
			"status", resp.StatusCode(),
			"body", string(resp.Body),
		)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return Page[Worker]{Data: []Worker{}}, nil
		}
		return pageOf(resp.JSON200.Data, resp.JSON200.NextCursor), nil
	case http.StatusUnauthorized:
		return Page[Worker]{}, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return Page[Worker]{}, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return Page[Worker]{}, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	default:
		return Page[Worker]{}, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// NewCodeDeploymentSource builds a deploymentSource for a code-based create.
func NewCodeDeploymentSource(src CodeSourceUpsert) (DeploymentSourceUpsert, error) {
	var source gen.DeploymentSourceUpsert_Source
	if err := source.FromCodeSourceUpsert(src); err != nil {
		return DeploymentSourceUpsert{}, err
	}
	return DeploymentSourceUpsert{
		Type:   DeploymentSourceTypeCode,
		Source: source,
	}, nil
}
