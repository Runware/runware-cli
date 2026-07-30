package serverless

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runware/runware-cli/internal/api/transport"
)

const testDeploymentID = "my-app"

func TestListGpuTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/gpu-types" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("missing/incorrect auth header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"h100","name":"H100","memory":"80 GB HBM","availability":"available","pricing":{"perSecond":"0.000767"}}]}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	gpus, err := c.ListGpuTypes(context.Background())
	if err != nil {
		t.Fatalf("ListGpuTypes: %v", err)
	}
	if len(gpus) != 1 {
		t.Fatalf("expected 1 gpu, got %d", len(gpus))
	}
	if gpus[0].Id != "h100" || gpus[0].Pricing.PerSecond != "0.000767" {
		t.Errorf("unexpected gpu: %+v", gpus[0])
	}
	if gpus[0].Memory != "80 GB HBM" {
		t.Errorf("unexpected memory: %q", gpus[0].Memory)
	}
}

func TestListGpuTypes_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.ListGpuTypes(context.Background()); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestListGpuTypes_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Unauthorized","status":401,"detail":"Missing or invalid API key"}`))
	}))
	defer srv.Close()

	c := newClient("bad", srv.URL, slog.Default(), srv.Client())
	_, err := c.ListGpuTypes(context.Background())
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeAuth {
		t.Errorf("expected CodeAuth, got %v", re.Code)
	}
	if re.Message != "Missing or invalid API key" {
		t.Errorf("unexpected message: %q", re.Message)
	}
}

func TestListGpuTypes_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upstream boom"))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.ListGpuTypes(context.Background())
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeServerError {
		t.Errorf("expected CodeServerError, got %v", re.Code)
	}
	if re.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", re.StatusCode)
	}
}

func TestCreateDeployment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/deployments" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"deploymentId":"my-app",
			"deploymentName":"My App",
			"status":"initializing",
			"configuration":{"maxWorkers":1,"idleTtlSecs":60,"scalingDelaySecs":10},
			"environmentVariables":[],
			"secrets":[],
			"createdAt":"2026-07-30T12:00:00Z",
			"updatedAt":"2026-07-30T12:00:00Z"
		}`))
	}))
	defer srv.Close()

	source, err := NewCodeDeploymentSource(CodeSourceUpsert{
		BaseImage: "python:3.11-slim",
		Codebase: CodebaseSource{
			ModelFile: "app.py",
			ZipBase64: "Zm9v",
		},
	})
	if err != nil {
		t.Fatalf("NewCodeDeploymentSource: %v", err)
	}

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	dep, err := c.CreateDeployment(context.Background(), DeploymentCreate{
		DeploymentId:     testDeploymentID,
		DeploymentName:   "My App",
		DeploymentSource: source,
		Configuration: WorkerConfigCreate{
			MaxWorkers:       1,
			IdleTtlSecs:      60,
			ScalingDelaySecs: 10,
		},
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if dep.DeploymentId != testDeploymentID || string(dep.Status) != "initializing" {
		t.Errorf("unexpected deployment: %+v", dep)
	}
}

func TestCreateDeployment_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Conflict","status":409,"detail":"Deployment already exists"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.CreateDeployment(context.Background(), DeploymentCreate{
		DeploymentId:   testDeploymentID,
		DeploymentName: "My App",
		Configuration: WorkerConfigCreate{
			MaxWorkers:       1,
			IdleTtlSecs:      60,
			ScalingDelaySecs: 10,
		},
	})
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeValidation {
		t.Errorf("expected CodeValidation, got %v", re.Code)
	}
	if re.Message != "Deployment already exists" {
		t.Errorf("unexpected message: %q", re.Message)
	}
}

func TestCreateDeployment_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.CreateDeployment(context.Background(), DeploymentCreate{}); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestListDeployments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/deployments" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("status"); got != "active" {
			t.Errorf("status query = %q, want active", got)
		}
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Errorf("limit query = %q, want 10", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{
			"deploymentId":"my-app",
			"deploymentName":"My App",
			"status":"active",
			"configuration":{"maxWorkers":1,"idleTtlSecs":60,"scalingDelaySecs":10,"minWorkers":0,"gpusPerWorker":1,"concurrency":1,"gracefulStopTtlSecs":120,"computeType":"gpu"},
			"environmentVariables":[],
			"secrets":[],
			"createdAt":"2026-07-30T12:00:00Z",
			"updatedAt":"2026-07-30T12:00:00Z"
		}]}`))
	}))
	defer srv.Close()

	limit := Limit(10)
	status := DeploymentStatus("active")
	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	deps, err := c.ListDeployments(context.Background(), &ListDeploymentsParams{
		Limit:  &limit,
		Status: &status,
	})
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(deps) != 1 || deps[0].DeploymentId != testDeploymentID {
		t.Fatalf("unexpected deployments: %+v", deps)
	}
}

func TestGetDeployment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/deployments/"+testDeploymentID {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"deploymentId":"my-app",
			"deploymentName":"My App",
			"status":"initializing",
			"configuration":{"maxWorkers":1,"idleTtlSecs":60,"scalingDelaySecs":10,"minWorkers":0,"gpusPerWorker":1,"concurrency":1,"gracefulStopTtlSecs":120,"computeType":"gpu"},
			"environmentVariables":[],
			"secrets":[],
			"createdAt":"2026-07-30T12:00:00Z",
			"updatedAt":"2026-07-30T12:00:00Z"
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	dep, err := c.GetDeployment(context.Background(), testDeploymentID)
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if dep.DeploymentId != testDeploymentID || string(dep.Status) != "initializing" {
		t.Errorf("unexpected deployment: %+v", dep)
	}
}

func TestGetDeployment_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"No deployment 'missing' exists"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.GetDeployment(context.Background(), "missing")
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", re.Code)
	}
}

func TestListEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/deployments/" + testDeploymentID + "/endpoints"
		if r.Method != http.MethodGet || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{
			"id":"11111111-1111-1111-1111-111111111111",
			"deploymentId":"my-app",
			"path":"/infer",
			"createdAt":"2026-07-30T12:00:00Z"
		}]}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	items, err := c.ListEndpoints(context.Background(), testDeploymentID, nil)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(items) != 1 || items[0].Path != "/infer" {
		t.Fatalf("unexpected endpoints: %+v", items)
	}
}

func TestListVersions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/deployments/" + testDeploymentID + "/versions"
		if r.Method != http.MethodGet || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{
			"id":"22222222-2222-2222-2222-222222222222",
			"deploymentId":"my-app",
			"buildId":"33333333-3333-3333-3333-333333333333",
			"versionNumber":1,
			"createdAt":"2026-07-30T12:00:00Z"
		}]}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	items, err := c.ListVersions(context.Background(), testDeploymentID, nil)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(items) != 1 || items[0].VersionNumber != 1 {
		t.Fatalf("unexpected versions: %+v", items)
	}
}

func TestListWorkers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/deployments/" + testDeploymentID + "/workers"
		if r.Method != http.MethodGet || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("status"); got != "ready" {
			t.Errorf("status query = %q, want ready", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{
			"id":"44444444-4444-4444-4444-444444444444",
			"deploymentId":"my-app",
			"versionId":"22222222-2222-2222-2222-222222222222",
			"status":"ready",
			"podName":"worker-0",
			"nodeName":"node-a",
			"lastSeenAt":"2026-07-30T12:01:00Z"
		}]}`))
	}))
	defer srv.Close()

	status := WorkerStatus("ready")
	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	items, err := c.ListWorkers(context.Background(), testDeploymentID, &ListWorkersParams{Status: &status})
	if err != nil {
		t.Fatalf("ListWorkers: %v", err)
	}
	if len(items) != 1 || string(items[0].Status) != "ready" || items[0].PodName != "worker-0" {
		t.Fatalf("unexpected workers: %+v", items)
	}
}
