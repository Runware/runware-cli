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

// createAppTimeout bounds createApp, which uploads a base64 zip and can
// exceed the default request timeout on slow links or larger codebases.
const createAppTimeout = 5 * time.Minute

// invokeSyncTimeout bounds startSyncTask. It must exceed the platform wait
// window so a 202 with the accepted task is received; a client-side timeout
// would lose the response and force a resubmit (a second billable run unless
// the same client task id is reused).
const invokeSyncTimeout = 5 * time.Minute

// GpuType is the public catalogue entry for a supported GPU type.
type GpuType = gen.GpuType

// App is a serverless application.
type App = gen.App

// AppCreate is the request body for createApp.
type AppCreate = gen.AppCreate

// AppSourceType selects the version creation path (`code` or `container`).
type AppSourceType = gen.AppSourceType

// AppSourceUpsert selects the initial version creation path.
type AppSourceUpsert = gen.AppSourceUpsert

// CodeSourceUpsert is a code-based app source.
type CodeSourceUpsert = gen.CodeSourceUpsert

// CodebaseSource is the zipped customer code payload.
type CodebaseSource = gen.CodebaseSource

// ContainerSource is a Dockerfile + container.yaml archive identified by sourceId.
type ContainerSource = gen.ContainerSource

// AppVolume is a persistent node-local directory mounted into the application.
type AppVolume = gen.AppVolume

// WorkerConfig is the live worker configuration on an app.
type WorkerConfig = gen.WorkerConfig

// WorkerConfigCreate is the worker configuration supplied at create time.
type WorkerConfigCreate = gen.WorkerConfigCreate

// AppUpdate is the request body for updateApp.
type AppUpdate = gen.AppUpdate

// WorkerConfigPatch is a partial worker configuration for updateApp.
type WorkerConfigPatch = gen.WorkerConfigPatch

// DeployRequest is the request body for DeployVersion.
type DeployRequest = gen.DeployRequest

// ListAppsParams are optional filters for ListApps.
type ListAppsParams = gen.ListAppsParams

// ListEndpointsParams are optional filters for ListEndpoints.
type ListEndpointsParams = gen.ListEndpointsParams

// ListVersionsParams are optional filters for ListVersions.
type ListVersionsParams = gen.ListVersionsParams

// ListBuildsParams are optional filters for ListBuilds.
type ListBuildsParams = gen.ListBuildsParams

// Build is a code build or container validation for an app.
type Build = gen.Build

// BuildStatus is a build lifecycle status.
type BuildStatus = gen.BuildStatus

// ListWorkersParams are optional filters for ListWorkers.
type ListWorkersParams = gen.ListWorkersParams

// Task is a serverless invocation.
type Task = gen.Task

// TaskStatus is a task lifecycle status.
type TaskStatus = gen.TaskStatus

// TaskPayload is the JSON object forwarded to an endpoint handler.
// It is the TaskInvocation.payload member, not the request body itself.
type TaskPayload = map[string]interface{}

// ListTasksParams are optional filters for ListTasks.
type ListTasksParams = gen.ListTasksParams

const (
	TaskStatusPending   TaskStatus = gen.TaskStatusPending
	TaskStatusCompleted TaskStatus = gen.TaskStatusCompleted
	TaskStatusFailed    TaskStatus = gen.TaskStatusFailed
)

// Endpoint is an app HTTP endpoint.
type Endpoint = gen.Endpoint

// Version is a deployed application version.
type Version = gen.Version

// Worker is a runtime worker instance.
type Worker = gen.Worker

// AppStatus is an app lifecycle status.
type AppStatus = gen.AppStatus

// AppSort is a listApps ordering.
type AppSort = gen.AppSort

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

const (
	// AppSourceTypeCode is appSource.type = "code".
	AppSourceTypeCode = gen.Code
	// AppSourceTypeContainer is appSource.type = "container".
	AppSourceTypeContainer = gen.Container
)

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

// createInner returns a generated client suitable for createApp.
func (c *Client) createInner() *gen.ClientWithResponses {
	return c.innerWithMinTimeout(createAppTimeout)
}

// innerWithMinTimeout returns the generated client, cloning the HTTP client
// when its Timeout is shorter than minTimeout. A zero Timeout (no deadline) is left
// unchanged.
func (c *Client) innerWithMinTimeout(minTimeout time.Duration) *gen.ClientWithResponses {
	hc, ok := c.doer.(*http.Client)
	if !ok {
		return c.inner
	}
	if hc.Timeout == 0 || hc.Timeout >= minTimeout {
		return c.inner
	}
	cloned := *hc
	cloned.Timeout = minTimeout
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

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

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

// CreateApp creates a new app and starts its initial rollout.
func (c *Client) CreateApp(ctx context.Context, body AppCreate) (*App, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.createInner().CreateAppWithResponse(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("create app: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusCreated:
		if resp.JSON201 == nil {
			return nil, fmt.Errorf("create app: empty 201 response")
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

// ListApps returns a page of apps for the authenticated organisation.
func (c *Client) ListApps(ctx context.Context, params *ListAppsParams) (Page[App], error) {
	if c.apiKey == "" {
		return Page[App]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListAppsWithResponse(ctx, params)
	if err != nil {
		return Page[App]{}, fmt.Errorf("list apps: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return pageOf[App](nil, nil), nil
		}
		return pageOf(resp.JSON200.Data, resp.JSON200.NextCursor), nil
	case http.StatusBadRequest:
		return Page[App]{}, problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
	case http.StatusUnauthorized:
		return Page[App]{}, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return Page[App]{}, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusUnprocessableEntity:
		return Page[App]{}, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	default:
		return Page[App]{}, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// GetApp returns a single app by ID.
func (c *Client) GetApp(ctx context.Context, appID string) (*App, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.GetAppWithResponse(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("get app: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("get app: empty 200 response")
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

// UpdateApp patches an app in place. Omitted fields are left unchanged.
// Currently persisted: appName and configuration.
func (c *Client) UpdateApp(ctx context.Context, appID string, body AppUpdate) (*App, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.UpdateAppWithResponse(ctx, appID, body)
	if err != nil {
		return nil, fmt.Errorf("update app: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("update app: empty 200 response")
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

// StopApp accepts a stop and returns the app with status stopping. Worker
// drain is asynchronous.
func (c *Client) StopApp(ctx context.Context, appID string) (*App, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.StopAppWithResponse(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("stop app: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	return acceptedApp("stop app", resp.StatusCode(), resp.JSON202, resp.Body, lifecycleProblems{
		Unauthorized: resp.ApplicationproblemJSON401,
		Forbidden:    resp.ApplicationproblemJSON403,
		NotFound:     resp.ApplicationproblemJSON404,
		Conflict:     resp.ApplicationproblemJSON409,
	})
}

// ResumeApp accepts a resume and returns the app with status initializing.
// Worker start is asynchronous.
func (c *Client) ResumeApp(ctx context.Context, appID string) (*App, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ResumeAppWithResponse(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("resume app: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	return acceptedApp("resume app", resp.StatusCode(), resp.JSON202, resp.Body, lifecycleProblems{
		Unauthorized: resp.ApplicationproblemJSON401,
		Forbidden:    resp.ApplicationproblemJSON403,
		NotFound:     resp.ApplicationproblemJSON404,
		Conflict:     resp.ApplicationproblemJSON409,
	})
}

// DeleteApp accepts a soft delete and returns the app with status deleting.
// Router removal and worker drain are asynchronous.
func (c *Client) DeleteApp(ctx context.Context, appID string) (*App, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.DeleteAppWithResponse(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("delete app: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	return acceptedApp("delete app", resp.StatusCode(), resp.JSON202, resp.Body, lifecycleProblems{
		Unauthorized: resp.ApplicationproblemJSON401,
		Forbidden:    resp.ApplicationproblemJSON403,
		NotFound:     resp.ApplicationproblemJSON404,
	})
}

// DeployVersion activates a ready version by number. Worker rollout is
// asynchronous; the 202 App reflects the persisted intent (activeVersionId
// and status), not healthy workers. An older ready number is a rollback.
func (c *Client) DeployVersion(ctx context.Context, appID string, versionNumber int32) (*App, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.DeployVersionWithResponse(ctx, appID, DeployRequest{
		VersionNumber: versionNumber,
	})
	if err != nil {
		return nil, fmt.Errorf("deploy version: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	return acceptedApp("deploy version", resp.StatusCode(), resp.JSON202, resp.Body, lifecycleProblems{
		Unauthorized: resp.ApplicationproblemJSON401,
		Forbidden:    resp.ApplicationproblemJSON403,
		NotFound:     resp.ApplicationproblemJSON404,
		Conflict:     resp.ApplicationproblemJSON409,
	})
}

// lifecycleProblems are typed RFC 9457 bodies bound by the generated client.
type lifecycleProblems struct {
	Unauthorized *gen.ProblemDetails
	Forbidden    *gen.ProblemDetails
	NotFound     *gen.ProblemDetails
	Conflict     *gen.ProblemDetails
}

func acceptedApp(op string, status int, app *App, body []byte, problems lifecycleProblems) (*App, error) {
	switch status {
	case http.StatusAccepted:
		if app == nil {
			return nil, fmt.Errorf("%s: empty 202 response", op)
		}
		return app, nil
	case http.StatusUnauthorized:
		return nil, problemToError(problems.Unauthorized, http.StatusUnauthorized)
	case http.StatusForbidden:
		return nil, problemToError(problems.Forbidden, http.StatusForbidden)
	case http.StatusNotFound:
		return nil, problemToError(problems.NotFound, http.StatusNotFound)
	case http.StatusConflict:
		if problems.Conflict != nil {
			return nil, problemToError(problems.Conflict, http.StatusConflict)
		}
		return nil, problemFromBody(body, status)
	default:
		return nil, problemFromBody(body, status)
	}
}

// ListEndpoints returns a page of endpoints for an app.
func (c *Client) ListEndpoints(ctx context.Context, appID string, params *ListEndpointsParams) (Page[Endpoint], error) {
	if c.apiKey == "" {
		return Page[Endpoint]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListEndpointsWithResponse(ctx, appID, params)
	if err != nil {
		return Page[Endpoint]{}, fmt.Errorf("list endpoints: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

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

// ListVersions returns a page of versions for an app.
func (c *Client) ListVersions(ctx context.Context, appID string, params *ListVersionsParams) (Page[Version], error) {
	if c.apiKey == "" {
		return Page[Version]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListVersionsWithResponse(ctx, appID, params)
	if err != nil {
		return Page[Version]{}, fmt.Errorf("list versions: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return pageOf[Version](nil, nil), nil
		}
		return pageOf(&resp.JSON200.Data, resp.JSON200.NextCursor), nil
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
func (c *Client) GetVersion(ctx context.Context, appID string, versionNumber int32) (*Version, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.GetVersionWithResponse(ctx, appID, versionNumber)
	if err != nil {
		return nil, fmt.Errorf("get version: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

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

// ListBuilds returns a page of builds for an app.
func (c *Client) ListBuilds(ctx context.Context, appID string, params *ListBuildsParams) (Page[Build], error) {
	if c.apiKey == "" {
		return Page[Build]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListBuildsWithResponse(ctx, appID, params)
	if err != nil {
		return Page[Build]{}, fmt.Errorf("list builds: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return pageOf[Build](nil, nil), nil
		}
		return pageOf(&resp.JSON200.Data, resp.JSON200.NextCursor), nil
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
func (c *Client) GetBuild(ctx context.Context, appID string, buildID uuid.UUID) (*Build, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.GetBuildWithResponse(ctx, appID, buildID)
	if err != nil {
		return nil, fmt.Errorf("get build: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

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

// ListWorkers returns a page of workers for an app.
func (c *Client) ListWorkers(ctx context.Context, appID string, params *ListWorkersParams) (Page[Worker], error) {
	if c.apiKey == "" {
		return Page[Worker]{}, transport.ErrNoAPIKey
	}

	resp, err := c.inner.ListWorkersWithResponse(ctx, appID, params)
	if err != nil {
		return Page[Worker]{}, fmt.Errorf("list workers: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

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

// NewCodeAppSource builds an appSource for a code-based create.
func NewCodeAppSource(src CodeSourceUpsert) (AppSourceUpsert, error) {
	var source gen.AppSourceUpsert_Source
	if err := source.FromCodeSourceUpsert(src); err != nil {
		return AppSourceUpsert{}, err
	}
	return AppSourceUpsert{
		Type:   AppSourceTypeCode,
		Source: source,
	}, nil
}

// NewContainerAppSource builds an appSource for a container-based create.
func NewContainerAppSource(src ContainerSource) (AppSourceUpsert, error) {
	var source gen.AppSourceUpsert_Source
	if err := source.FromContainerSource(src); err != nil {
		return AppSourceUpsert{}, err
	}
	return AppSourceUpsert{
		Type:   AppSourceTypeContainer,
		Source: source,
	}, nil
}
