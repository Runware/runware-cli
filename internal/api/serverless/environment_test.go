package serverless

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

const testEnvironment = "staging"

// environmentCall is one client call, named by the generated operation it sends.
type environmentCall struct {
	operation string
	call      func(context.Context, *Client) error
}

func environmentScopedCalls() []environmentCall {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	return []environmentCall{
		{
			operation: "CreateApp",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.CreateApp(ctx, AppCreate{
					AppId: testAppID,
				})
				return err
			},
		},
		{
			operation: "ListApps",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListApps(ctx, ListAppsParams{})
				return err
			},
		},
		{
			operation: "GetApp",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetApp(ctx, testAppID)
				return err
			},
		},
		{
			operation: "UpdateApp",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.UpdateApp(ctx, testAppID, AppUpdate{})
				return err
			},
		},
		{
			operation: "StopApp",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.StopApp(ctx, testAppID)
				return err
			},
		},
		{
			operation: "ResumeApp",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ResumeApp(ctx, testAppID)
				return err
			},
		},
		{
			operation: "DeleteApp",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.DeleteApp(ctx, testAppID)
				return err
			},
		},
		{
			operation: "DeployVersion",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.DeployVersion(ctx, testAppID, 2)
				return err
			},
		},
		{
			operation: "ListEndpoints",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListEndpoints(ctx, testAppID, ListEndpointsParams{})
				return err
			},
		},
		{
			operation: "GetEndpoint",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetEndpoint(ctx, testAppID, id)
				return err
			},
		},
		{
			operation: "ListVersions",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListVersions(ctx, testAppID, ListVersionsParams{})
				return err
			},
		},
		{
			operation: "GetVersion",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetVersion(ctx, testAppID, 1)
				return err
			},
		},
		{
			operation: "DeleteVersion",
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteVersion(ctx, testAppID, 1)
			},
		},
		{
			operation: "ListBuilds",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListBuilds(ctx, testAppID, ListBuildsParams{})
				return err
			},
		},
		{
			operation: "GetBuild",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetBuild(ctx, testAppID, id)
				return err
			},
		},
		{
			operation: "DeleteBuild",
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteBuild(ctx, testAppID, id)
			},
		},
		{
			operation: "ListWorkers",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListWorkers(ctx, testAppID, ListWorkersParams{})
				return err
			},
		},
		{
			operation: "GetWorker",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetWorker(ctx, testAppID, id)
				return err
			},
		},
		{
			operation: "ListAppEvents",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListAppEvents(ctx, testAppID, ListAppEventsParams{})
				return err
			},
		},
		{
			operation: "ListAppErrors",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListAppErrors(ctx, testAppID, ListAppErrorsParams{})
				return err
			},
		},
		{
			operation: "ListAppEnvironmentVariables",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListAppEnvironmentVariables(ctx, testAppID, ListAppEnvironmentVariablesParams{})
				return err
			},
		},
		{
			operation: "UpdateAppEnvironmentVariable",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.UpdateAppEnvironmentVariable(ctx, testAppID, "KEY", EnvironmentVariableUpdate{
					Value: "v",
				})
				return err
			},
		},
		{
			operation: "DeleteAppEnvironmentVariable",
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteAppEnvironmentVariable(ctx, testAppID, "KEY")
			},
		},
		{
			operation: "ListSecrets",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListSecrets(ctx, ListSecretsParams{})
				return err
			},
		},
		{
			operation: "CreateSecret",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.CreateSecret(ctx, SecretCreate{
					Name:  "TOKEN",
					Type:  SecretTypeGeneric,
					Value: "v",
				})
				return err
			},
		},
		{
			operation: "UpdateSecret",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.UpdateSecret(ctx, "TOKEN", SecretUpdate{
					Value: "v",
				})
				return err
			},
		},
		{
			operation: "DeleteSecret",
			call: func(ctx context.Context, c *Client) error {
				return c.DeleteSecret(ctx, "TOKEN")
			},
		},
		{
			operation: "ListAppSecrets",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListAppSecrets(ctx, testAppID, ListAppSecretsParams{})
				return err
			},
		},
		{
			operation: "AttachAppSecret",
			call: func(ctx context.Context, c *Client) error {
				return c.AttachAppSecret(ctx, testAppID, SecretAttach{
					SecretName: "TOKEN",
				})
			},
		},
		{
			operation: "DetachAppSecret",
			call: func(ctx context.Context, c *Client) error {
				return c.DetachAppSecret(ctx, testAppID, "TOKEN")
			},
		},
		{
			operation: "StartAsyncTask",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.InvokeAsync(ctx, testAppID, "infer", "", TaskPayload{})
				return err
			},
		},
		{
			operation: "StartSyncTask",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.InvokeSync(ctx, testAppID, "infer", "", TaskPayload{})
				return err
			},
		},
		{
			operation: "GetTask",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetTask(ctx, testAppID, id.String())
				return err
			},
		},
		{
			operation: "ListTasks",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListTasks(ctx, testAppID, ListTasksParams{})
				return err
			},
		},
		{
			operation: "GetLogEntries",
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
			operation: "TailLogEntries",
			call: func(ctx context.Context, c *Client) error {
				return c.TailLogs(ctx, LogQueryRuntimeTail, testAppID, func(LogEntry) error { return nil })
			},
		},
		{
			operation: "GetUsageSummary",
			call: func(ctx context.Context, c *Client) error {
				app := testAppID
				_, err := c.GetUsageSummary(ctx, GetUsageSummaryParams{
					AppId: &app,
				})
				return err
			},
		},
		{
			operation: "ListUsageEvents",
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
			operation: "ListGpuTypes",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListGpuTypes(ctx)
				return err
			},
		},
		{
			operation: "ListEnvironments",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.ListEnvironments(ctx)
				return err
			},
		},
		{
			operation: "GetEnvironment",
			call: func(ctx context.Context, c *Client) error {
				_, err := c.GetEnvironment(ctx, testEnvironment)
				return err
			},
		},
		{
			operation: "CreateSourceUpload",
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

func TestEnvironmentScopedRequestsCarryTheClientEnvironment(t *testing.T) {
	for _, tc := range environmentScopedCalls() {
		t.Run(tc.operation, func(t *testing.T) {
			for _, q := range recordQueries(t, testEnvironment, tc.call) {
				if got := q.Get("environment"); got != testEnvironment {
					t.Fatalf("environment = %q, want %q (query %v)", got, testEnvironment, q)
				}
			}
		})
	}
}

func TestEnvironmentScopedRequestsOmitTheEnvironmentWhenUnset(t *testing.T) {
	for _, tc := range environmentScopedCalls() {
		t.Run(tc.operation, func(t *testing.T) {
			for _, q := range recordQueries(t, "", tc.call) {
				if q.Has("environment") {
					t.Fatalf("environment sent without one set: %v", q)
				}
			}
		})
	}
}

func TestOrganizationRequestsNeverCarryAnEnvironment(t *testing.T) {
	for _, tc := range orgScopedCalls() {
		t.Run(tc.operation, func(t *testing.T) {
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

// unwrappedOperations take an environment in the spec but have no client
// wrapper, so no command can send them.
var unwrappedOperations = []string{
	"CreateSecretBatch",
	"FavoriteApp",
	"GetAppSummary",
	"GetMetricSeries",
	"StartDefaultAsyncTask",
	"StartDefaultSyncTask",
	"UnfavoriteApp",
}

// environmentOperations are the generated operations whose params carry an
// environment field.
func environmentOperations(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "gen/client.gen.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var operations []string
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts := spec.(*ast.TypeSpec)
			st, ok := ts.Type.(*ast.StructType)
			if !ok || !strings.HasSuffix(ts.Name.Name, "Params") {
				continue
			}
			for _, field := range st.Fields.List {
				if len(field.Names) == 1 && field.Names[0].Name == "Environment" {
					operations = append(operations, strings.TrimSuffix(ts.Name.Name, "Params"))
				}
			}
		}
	}
	slices.Sort(operations)
	return operations
}

func TestEveryOperationWithAnEnvironmentIsCovered(t *testing.T) {
	var covered []string
	for _, c := range environmentScopedCalls() {
		covered = append(covered, c.operation)
	}
	covered = append(covered, unwrappedOperations...)
	slices.Sort(covered)

	want := environmentOperations(t)
	if !slices.Equal(covered, want) {
		t.Fatalf("operations with an environment = %v\ncovered by environmentScopedCalls or unwrappedOperations = %v", want, covered)
	}
	for _, c := range orgScopedCalls() {
		if slices.Contains(want, c.operation) {
			t.Errorf("%s takes an environment in the spec but is tested as an organization call", c.operation)
		}
	}
}
