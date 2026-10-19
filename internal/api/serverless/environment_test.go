package serverless

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/google/uuid"
)

const testEnvironment = "staging"

type environmentCall struct {
	name string
	call func(context.Context, *Client) error
}

func appScopedCalls() []environmentCall {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	return []environmentCall{
		{
			name: "create app",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.CreateApp(ctx, AppCreate{
					AppId: testAppID,
				})
				return err
			},
		},
		{
			name: "list apps",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListApps(ctx, ListAppsParams{})
				return err
			},
		},
		{
			name: "get app",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetApp(ctx, testAppID)
				return err
			},
		},
		{
			name: "update app",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.UpdateApp(ctx, testAppID, AppUpdate{})
				return err
			},
		},
		{
			name: "stop app",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.StopApp(ctx, testAppID)
				return err
			},
		},
		{
			name: "resume app",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ResumeApp(ctx, testAppID)
				return err
			},
		},
		{
			name: "delete app",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.DeleteApp(ctx, testAppID)
				return err
			},
		},
		{
			name: "deploy version",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.DeployVersion(ctx, testAppID, 2)
				return err
			},
		},
		{
			name: "list endpoints",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListEndpoints(ctx, testAppID, ListEndpointsParams{})
				return err
			},
		},
		{
			name: "get endpoint",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetEndpoint(ctx, testAppID, id)
				return err
			},
		},
		{
			name: "list versions",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListVersions(ctx, testAppID, ListVersionsParams{})
				return err
			},
		},
		{
			name: "get version",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetVersion(ctx, testAppID, 1)
				return err
			},
		},
		{
			name: "delete version",
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteVersion(ctx, testAppID, 1)
			},
		},
		{
			name: "list builds",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListBuilds(ctx, testAppID, ListBuildsParams{})
				return err
			},
		},
		{
			name: "get build",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetBuild(ctx, testAppID, id)
				return err
			},
		},
		{
			name: "delete build",
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteBuild(ctx, testAppID, id)
			},
		},
		{
			name: "list workers",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListWorkers(ctx, testAppID, ListWorkersParams{})
				return err
			},
		},
		{
			name: "get worker",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetWorker(ctx, testAppID, id)
				return err
			},
		},
		{
			name: "list events",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListAppEvents(ctx, testAppID, ListAppEventsParams{})
				return err
			},
		},
		{
			name: "list errors",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListAppErrors(ctx, testAppID, ListAppErrorsParams{})
				return err
			},
		},
		{
			name: "list env vars",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListAppEnvironmentVariables(ctx, testAppID, ListAppEnvironmentVariablesParams{})
				return err
			},
		},
		{
			name: "set env var",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.UpdateAppEnvironmentVariable(ctx, testAppID, "KEY", EnvironmentVariableUpdate{
					Value: "v",
				})
				return err
			},
		},
		{
			name: "unset env var",
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteAppEnvironmentVariable(ctx, testAppID, "KEY")
			},
		},
		{
			name: "list app secrets",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListAppSecrets(ctx, testAppID, ListAppSecretsParams{})
				return err
			},
		},
		{
			name: "attach secret",
			call: func(ctx context.Context, c *Client) error {
				return c.AttachAppSecret(ctx, testAppID, SecretAttach{
					SecretName: "TOKEN",
				})
			},
		},
		{
			name: "detach secret",
			call: func(ctx context.Context, c *Client) error {
				return c.DetachAppSecret(ctx, testAppID, "TOKEN")
			},
		},
		{
			name: "invoke async",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.InvokeAsync(ctx, testAppID, "infer", "", TaskPayload{})
				return err
			},
		},
		{
			name: "invoke sync",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.InvokeSync(ctx, testAppID, "infer", "", TaskPayload{})
				return err
			},
		},
		{
			name: "get task",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetTask(ctx, testAppID, id.String())
				return err
			},
		},
		{
			name: "list tasks",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListTasks(ctx, testAppID, ListTasksParams{})
				return err
			},
		},
		{
			name: "log entries",
			call: func(ctx context.Context, c *Client) error {
				app := testAppID
				_, err := c.GetLogEntries(ctx, LogQueryRuntime, GetLogEntriesParams{
					Window:     LogWindow("1h"),
					Deployment: &app,
				})
				return err
			},
		},
		{
			name: "tail logs",
			call: func(ctx context.Context, c *Client) error {
				return c.TailLogs(ctx, LogQueryRuntimeTail, testAppID, func(LogEntry) error { return nil })
			},
		},
		{
			name: "usage summary",
			call: func(ctx context.Context, c *Client) error {
				app := testAppID
				_, err := c.GetUsageSummary(ctx, GetUsageSummaryParams{
					AppId: &app,
				})
				return err
			},
		},
		{
			name: "usage events",
			call: func(ctx context.Context, c *Client) error {
				app := testAppID
				_, err := c.ListUsageEvents(ctx, ListUsageEventsParams{
					AppId: &app,
				})
				return err
			},
		},
	}
}

func orgScopedCalls() []environmentCall {
	return []environmentCall{
		{
			name: "list gpu types",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListGpuTypes(ctx)
				return err
			},
		},
		{
			name: "list environments",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListEnvironments(ctx)
				return err
			},
		},
		{
			name: "get environment",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetEnvironment(ctx, testEnvironment)
				return err
			},
		},
		{
			name: "list secrets",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListSecrets(ctx, ListSecretsParams{})
				return err
			},
		},
		{
			name: "delete secret",
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteSecret(ctx, "TOKEN")
			},
		},
		{
			name: "create source upload",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.CreateSourceUpload(ctx, SourceUploadCreate{})
				return err
			},
		},
	}
}

// queryRecorder answers every request with 404 and keeps each request's query.
type queryRecorder struct {
	mu      sync.Mutex
	queries []url.Values
}

func (r *queryRecorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.queries = append(r.queries, req.URL.Query())
	r.mu.Unlock()
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"title":"Not Found","status":404}`))
}

func recordQueries(t *testing.T, environment EnvironmentName, call func(context.Context, *Client) error) []url.Values {
	t.Helper()
	rec := &queryRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	c := newClient("test-key", srv.URL, slog.Default(), srv.Client()).WithEnvironment(environment)
	_ = call(context.Background(), c)
	if len(rec.queries) == 0 {
		t.Fatal("no request reached the server")
	}
	return rec.queries
}

func TestAppScopedRequestsCarryTheClientEnvironment(t *testing.T) {
	for _, tc := range appScopedCalls() {
		t.Run(tc.name, func(t *testing.T) {
			for _, q := range recordQueries(t, testEnvironment, tc.call) {
				if got := q.Get("environment"); got != testEnvironment {
					t.Fatalf("environment = %q, want %q (query %v)", got, testEnvironment, q)
				}
			}
		})
	}
}

func TestAppScopedRequestsOmitTheEnvironmentWhenUnset(t *testing.T) {
	for _, tc := range appScopedCalls() {
		t.Run(tc.name, func(t *testing.T) {
			for _, q := range recordQueries(t, "", tc.call) {
				if q.Has("environment") {
					t.Fatalf("environment sent without one set: %v", q)
				}
			}
		})
	}
}

type requestedEnvironmentCase struct {
	name      string
	client    EnvironmentName
	requested EnvironmentName
	want      string
}

func TestListRequestKeepsTheEnvironmentTheCallerSet(t *testing.T) {
	cases := []requestedEnvironmentCase{
		{
			name:      "unscoped client",
			requested: testEnvironment,
			want:      testEnvironment,
		},
		{
			name:      "client scoped elsewhere",
			client:    "production",
			requested: testEnvironment,
			want:      testEnvironment,
		},
		{
			name:   "caller leaves it unset",
			client: testEnvironment,
			want:   testEnvironment,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := ListAppsParams{}
			if tc.requested != "" {
				params.Environment = &tc.requested
			}

			queries := recordQueries(t, tc.client, func(ctx context.Context, c *Client) error {
				_, err := c.ListApps(ctx, params)
				return err
			})

			if got := queries[0].Get("environment"); got != tc.want {
				t.Fatalf("environment = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOrganizationRequestsNeverCarryAnEnvironment(t *testing.T) {
	for _, tc := range orgScopedCalls() {
		t.Run(tc.name, func(t *testing.T) {
			for _, q := range recordQueries(t, testEnvironment, tc.call) {
				if q.Has("environment") {
					t.Fatalf("organization request scoped to an environment: %v", q)
				}
			}
		})
	}
}

func TestWithEnvironmentLeavesTheOriginalClientUnscoped(t *testing.T) {
	rec := &queryRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	base := newClient("test-key", srv.URL, slog.Default(), srv.Client())

	_ = base.WithEnvironment(testEnvironment)
	_, _ = base.GetApp(context.Background(), testAppID)

	if len(rec.queries) != 1 {
		t.Fatalf("requests = %d, want 1", len(rec.queries))
	}
	if rec.queries[0].Has("environment") {
		t.Fatalf("base client sent environment: %v", rec.queries[0])
	}
}
