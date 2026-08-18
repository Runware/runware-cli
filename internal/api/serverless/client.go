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

	"github.com/google/uuid"
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

// WorkerConfig is the live worker configuration on a deployment.
type WorkerConfig = gen.WorkerConfig

// WorkerConfigCreate is the worker configuration supplied at create time.
type WorkerConfigCreate = gen.WorkerConfigCreate

// DeploymentUpdate is the request body for updateDeployment.
type DeploymentUpdate = gen.DeploymentUpdate

// WorkerConfigPatch is a partial worker configuration for updateDeployment.
type WorkerConfigPatch = gen.WorkerConfigPatch

// ListDeploymentsParams are optional filters for ListDeployments.
type ListDeploymentsParams = gen.ListDeploymentsParams

// ListEndpointsParams are optional filters for ListEndpoints.
type ListEndpointsParams = gen.ListEndpointsParams

// ListVersionsParams are optional filters for ListVersions.
type ListVersionsParams = gen.ListVersionsParams

// ListBuildsParams are optional filters for ListBuilds.
type ListBuildsParams = gen.ListBuildsParams

// Build is a code build or container validation for a deployment.
type Build = gen.Build

// BuildStatus is a build lifecycle status.
type BuildStatus = gen.BuildStatus

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

// DeploymentSort is a listDeployments ordering.
type DeploymentSort = gen.DeploymentSort

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

	c.logResponse(ctx, "/v1/gpu-types", resp.StatusCode(), resp.Body)

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

	c.logResponse(ctx, "/v1/deployments", resp.StatusCode(), resp.Body)

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

	c.logResponse(ctx, "/v1/deployments", resp.StatusCode(), resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return pageOf[Deployment](nil, nil), nil
		}
		return pageOf(resp.JSON200.Data, resp.JSON200.NextCursor), nil
	case http.StatusBadRequest:
		return Page[Deployment]{}, problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
	case http.StatusUnauthorized:
		return Page[Deployment]{}, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return Page[Deployment]{}, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusUnprocessableEntity:
		return Page[Deployment]{}, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
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

	c.logResponse(ctx, "/v1/deployments/"+deploymentID, resp.StatusCode(), resp.Body)

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

// UpdateDeployment patches a deployment in place. Omitted fields are left
// unchanged. Currently persisted: deploymentName and configuration.
func (c *Client) UpdateDeployment(ctx context.Context, deploymentID string, body DeploymentUpdate) (*Deployment, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.UpdateDeploymentWithResponse(ctx, deploymentID, body)
	if err != nil {
		return nil, fmt.Errorf("update deployment: %w", err)
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
			return nil, fmt.Errorf("update deployment: empty 200 response")
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
	case http.StatusConflict:
		return nil, problemToError(resp.ApplicationproblemJSON409, http.StatusConflict)
	case http.StatusUnprocessableEntity:
		return nil, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
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

	c.logResponse(ctx, "/v1/deployments/"+deploymentID+"/endpoints", resp.StatusCode(), resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return pageOf[Endpoint](nil, nil), nil
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

	c.logResponse(ctx, "/v1/deployments/"+deploymentID+"/versions", resp.StatusCode(), resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return pageOf[Version](nil, nil), nil
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

// GetVersion returns a single version by number.
func (c *Client) GetVersion(ctx context.Context, deploymentID string, versionNumber int32) (*Version, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.GetVersionWithResponse(ctx, deploymentID, versionNumber)
	if err != nil {
		return nil, fmt.Errorf("get version: %w", err)
	}

	c.logResponse(ctx, fmt.Sprintf("/v1/deployments/%s/versions/%d", deploymentID, versionNumber), resp.StatusCode(), resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("get version: empty 200 response")
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

// ListBuilds returns a page of builds for a deployment.
func (c *Client) ListBuilds(ctx context.Context, deploymentID string, params *ListBuildsParams) (Page[Build], error) {
	if c.apiKey == "" {
		return Page[Build]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListBuildsWithResponse(ctx, deploymentID, params)
	if err != nil {
		return Page[Build]{}, fmt.Errorf("list builds: %w", err)
	}

	c.logResponse(ctx, "/v1/deployments/"+deploymentID+"/builds", resp.StatusCode(), resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return pageOf[Build](nil, nil), nil
		}
		return pageOf(resp.JSON200.Data, resp.JSON200.NextCursor), nil
	case http.StatusUnauthorized:
		return Page[Build]{}, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return Page[Build]{}, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return Page[Build]{}, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	default:
		return Page[Build]{}, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// GetBuild returns a single build by ID.
func (c *Client) GetBuild(ctx context.Context, deploymentID string, buildID uuid.UUID) (*Build, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.GetBuildWithResponse(ctx, deploymentID, buildID)
	if err != nil {
		return nil, fmt.Errorf("get build: %w", err)
	}

	c.logResponse(ctx, "/v1/deployments/"+deploymentID+"/builds/"+buildID.String(), resp.StatusCode(), resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("get build: empty 200 response")
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

// ListWorkers returns a page of workers for a deployment.
func (c *Client) ListWorkers(ctx context.Context, deploymentID string, params *ListWorkersParams) (Page[Worker], error) {
	if c.apiKey == "" {
		return Page[Worker]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListWorkersWithResponse(ctx, deploymentID, params)
	if err != nil {
		return Page[Worker]{}, fmt.Errorf("list workers: %w", err)
	}

	c.logResponse(ctx, "/v1/deployments/"+deploymentID+"/workers", resp.StatusCode(), resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return pageOf[Worker](nil, nil), nil
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
