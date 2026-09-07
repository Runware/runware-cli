package serverless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/api/transport"
)

const (
	testAppID         = "my-app"
	testGPUType       = "h100"
	testBuildID       = "33333333-3333-3333-3333-333333333333"
	testVersionID     = "22222222-2222-2222-2222-222222222222"
	testEndpointID    = "11111111-1111-1111-1111-111111111111"
	testWorkerID      = "44444444-4444-4444-4444-444444444444"
	testVersionNumber = int32(1)
	testCursorPage2   = "page-2"
	testCursorPage3   = "page-3"
	testStatusReady   = "ready"
	testModelFile     = "model.py"
)

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
	if gpus[0].Id != testGPUType || gpus[0].Pricing.PerSecond != "0.000767" {
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

func TestListGpuTypes_UnhandledStatusKeepsProblemDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Service Unavailable","status":503,"detail":"Control plane is draining"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.ListGpuTypes(context.Background())
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Message != "Control plane is draining" {
		t.Errorf("unexpected message: %q", re.Message)
	}
	if re.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", re.StatusCode)
	}
}

func TestCreateApp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/apps" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var body AppCreate
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.AppId != testAppID {
			t.Errorf("appId = %q, want %s", body.AppId, testAppID)
		}
		if body.Configuration.GpuType != testGPUType {
			t.Errorf("gpuType = %q, want %s", body.Configuration.GpuType, testGPUType)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"appId":"my-app",
			"appName":"My App",
			"status":"initializing",
			"configuration":{"maxWorkers":1,"idleTtlSecs":60,"scalingDelaySecs":10},
			"environmentVariables":[],
			"secrets":[],
			"createdAt":"2026-07-30T12:00:00Z",
			"updatedAt":"2026-07-30T12:00:00Z"
		}`))
	}))
	defer srv.Close()

	source, err := NewCodeAppSource(CodeSourceUpsert{
		BaseImage: "python:3.11-slim",
		Codebase: CodebaseSource{
			SourceId:  uuid.MustParse("019c7654-8b21-7abc-9123-abcdef123456"),
			ModelFile: testModelFile,
		},
	})
	if err != nil {
		t.Fatalf("NewCodeAppSource: %v", err)
	}

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	app, err := c.CreateApp(context.Background(), AppCreate{
		AppId:     testAppID,
		AppName:   "My App",
		AppSource: source,
		Configuration: WorkerConfigCreate{
			GpuType:          testGPUType,
			MaxWorkers:       1,
			IdleTtlSecs:      60,
			ScalingDelaySecs: 10,
		},
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if app.AppId != testAppID || app.Status != AppStatusInitializing {
		t.Errorf("unexpected app: %+v", app)
	}
}

func TestCreateApp_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Conflict","status":409,"detail":"App already exists"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.CreateApp(context.Background(), AppCreate{
		AppId:   testAppID,
		AppName: "My App",
		Configuration: WorkerConfigCreate{
			GpuType:          testGPUType,
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
	if re.Message != "App already exists" {
		t.Errorf("unexpected message: %q", re.Message)
	}
}

func TestNewContainerAppSource(t *testing.T) {
	id := uuid.MustParse("019c7654-8b21-7abc-9123-abcdef123456")
	source, err := NewContainerAppSource(ContainerSource{
		SourceId: id,
	})
	if err != nil {
		t.Fatalf("NewContainerAppSource: %v", err)
	}
	if source.Type != AppSourceTypeContainer {
		t.Errorf("type = %q, want container", source.Type)
	}
	inner, err := source.Source.AsContainerSource()
	if err != nil {
		t.Fatalf("AsContainerSource: %v", err)
	}
	if inner.SourceId != id {
		t.Errorf("sourceId = %s, want %s", inner.SourceId, id)
	}

	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire AppSourceUpsert
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if wire.Type != AppSourceTypeContainer {
		t.Errorf("wire type = %q, want container", wire.Type)
	}
	got, err := wire.Source.AsContainerSource()
	if err != nil {
		t.Fatalf("wire AsContainerSource: %v", err)
	}
	if got.SourceId != id {
		t.Errorf("wire sourceId = %s, want %s", got.SourceId, id)
	}
}

func TestNewCodeAppSource(t *testing.T) {
	id := uuid.MustParse("019c7654-8b21-7abc-9123-abcdef123456")
	source, err := NewCodeAppSource(CodeSourceUpsert{
		BaseImage: "python:3.11-slim",
		Codebase: CodebaseSource{
			SourceId:  id,
			ModelFile: testModelFile,
		},
	})
	if err != nil {
		t.Fatalf("NewCodeAppSource: %v", err)
	}
	if source.Type != AppSourceTypeCode {
		t.Errorf("type = %q, want code", source.Type)
	}
	inner, err := source.Source.AsCodeSourceUpsert()
	if err != nil {
		t.Fatalf("AsCodeSourceUpsert: %v", err)
	}
	if inner.Codebase.SourceId != id || inner.Codebase.ModelFile != testModelFile {
		t.Errorf("codebase = %+v", inner.Codebase)
	}
}

func TestCreateApp_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.CreateApp(context.Background(), AppCreate{}); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestPageOf_PreservesNextCursorWhenDataNil(t *testing.T) {
	next := testCursorPage2
	page := pageOf[App](nil, &next)
	if len(page.Data) != 0 {
		t.Fatalf("expected empty data, got %+v", page.Data)
	}
	if page.NextCursor == nil || *page.NextCursor != testCursorPage2 {
		t.Fatalf("unexpected nextCursor: %+v", page.NextCursor)
	}

	var nilSlice []App
	page = pageOf(&nilSlice, &next)
	if page.Data == nil {
		t.Fatal("expected non-nil empty data slice")
	}
	if page.NextCursor == nil || *page.NextCursor != testCursorPage2 {
		t.Fatalf("unexpected nextCursor for nil slice: %+v", page.NextCursor)
	}
}

func TestListApps_EmptyDataKeepsNextCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"nextCursor":"` + testCursorPage2 + `"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListApps(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(page.Data) != 0 {
		t.Fatalf("expected empty data, got %+v", page.Data)
	}
	if page.NextCursor == nil || *page.NextCursor != testCursorPage2 {
		t.Fatalf("unexpected nextCursor: %+v", page.NextCursor)
	}
}

func TestListApps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/apps" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("status"); got != "active" {
			t.Errorf("status query = %q, want active", got)
		}
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Errorf("limit query = %q, want 10", got)
		}
		if got := r.URL.Query().Get("cursor"); got != testCursorPage2 {
			t.Errorf("cursor query = %q, want %s", got, testCursorPage2)
		}
		if got := r.URL.Query().Get("q"); got != "demo" {
			t.Errorf("q query = %q, want demo", got)
		}
		if got := r.URL.Query().Get("gpuType"); got != testGPUType {
			t.Errorf("gpuType query = %q, want %s", got, testGPUType)
		}
		if got := r.URL.Query().Get("sort"); got != "name" {
			t.Errorf("sort query = %q, want name", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{
			"appId":"my-app",
			"appName":"My App",
			"status":"active",
			"configuration":{"maxWorkers":1,"idleTtlSecs":60,"scalingDelaySecs":10,"minWorkers":0,"gpusPerWorker":1,"concurrency":1,"gracefulStopTtlSecs":120,"computeType":"gpu"},
			"environmentVariables":[],
			"secrets":[],
			"createdAt":"2026-07-30T12:00:00Z",
			"updatedAt":"2026-07-30T12:00:00Z"
		}],"nextCursor":"` + testCursorPage3 + `"}`))
	}))
	defer srv.Close()

	limit := Limit(10)
	cursor := Cursor(testCursorPage2)
	status := AppStatus("active")
	q := "demo"
	gpuType := testGPUType
	sort := AppSort("name")
	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListApps(context.Background(), &ListAppsParams{
		Limit:   &limit,
		Cursor:  &cursor,
		Status:  &status,
		Q:       &q,
		GpuType: &gpuType,
		Sort:    &sort,
	})
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].AppId != testAppID {
		t.Fatalf("unexpected apps: %+v", page.Data)
	}
	if page.NextCursor == nil || *page.NextCursor != testCursorPage3 {
		t.Fatalf("unexpected nextCursor: %+v", page.NextCursor)
	}
}

func TestListApps_BadCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Bad Request","status":400,"detail":"cursor does not match the current sort and filters"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.ListApps(context.Background(), nil)
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", re.StatusCode)
	}
	if re.Message != "cursor does not match the current sort and filters" {
		t.Errorf("unexpected message: %q", re.Message)
	}
}

func TestListApps_UnimplementedSort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{
			"type":"about:blank",
			"title":"Unprocessable Entity",
			"status":422,
			"detail":"sort activity is not available yet",
			"errors":[{"detail":"traffic metrics are not collected yet","pointer":"/sort"}]
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.ListApps(context.Background(), nil)
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected status 422, got %d", re.StatusCode)
	}
	if !strings.Contains(re.Message, "sort activity is not available yet") {
		t.Errorf("missing detail: %q", re.Message)
	}
	if !strings.Contains(re.Message, "/sort: traffic metrics are not collected yet") {
		t.Errorf("missing field error: %q", re.Message)
	}
}

func TestGetApp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/apps/"+testAppID {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"appId":"my-app",
			"appName":"My App",
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
	app, err := c.GetApp(context.Background(), testAppID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if app.AppId != testAppID || app.Status != AppStatusInitializing {
		t.Errorf("unexpected app: %+v", app)
	}
}

func TestWaitApp_PollsUntilActive(t *testing.T) {
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/apps/"+testAppID {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		n := gets.Add(1)
		w.Header().Set("Content-Type", "application/json")
		status := AppStatusInitializing
		if n > 1 {
			status = AppStatusActive
		}
		_, _ = w.Write([]byte(testAppJSON(status)))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	app, err := c.WaitApp(context.Background(), testAppID, time.Millisecond)
	if err != nil {
		t.Fatalf("WaitApp: %v", err)
	}
	if app.Status != AppStatusActive {
		t.Fatalf("unexpected app: %+v", app)
	}
	if gets.Load() < 2 {
		t.Fatalf("expected at least 2 GETs, got %d", gets.Load())
	}
}

func TestWaitApp_FailedIsTerminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testAppJSON(AppStatusFailed)))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	app, err := c.WaitApp(context.Background(), testAppID, time.Millisecond)
	if err != nil {
		t.Fatalf("WaitApp: %v", err)
	}
	if app.Status != AppStatusFailed {
		t.Fatalf("unexpected app: %+v", app)
	}
}

func TestAppDeployTerminal(t *testing.T) {
	terminal := []AppStatus{
		AppStatusActive,
		AppStatusFailed,
		AppStatusStopped,
		AppStatusDeleting,
		AppStatusDeleted,
	}
	for _, status := range terminal {
		if !AppDeployTerminal(status) {
			t.Errorf("%s should be terminal", status)
		}
	}
	for _, status := range []AppStatus{AppStatusInitializing, AppStatusStopping} {
		if AppDeployTerminal(status) {
			t.Errorf("%s should keep polling", status)
		}
	}
}

func TestWaitApp_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testAppJSON(AppStatusInitializing)))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.WaitApp(ctx, testAppID, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestGetApp_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"No app 'missing' exists"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.GetApp(context.Background(), "missing")
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", re.Code)
	}
}

func TestUpdateApp(t *testing.T) {
	maxWorkers := int32(2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/apps/"+testAppID {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var body AppUpdate
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Configuration == nil || body.Configuration.MaxWorkers == nil || *body.Configuration.MaxWorkers != maxWorkers {
			t.Errorf("unexpected body: %s", raw)
		}
		if body.AppName != nil || body.AppSource != nil || body.Secrets != nil || body.EnvironmentVariables != nil {
			t.Errorf("patch included out-of-scope fields: %s", raw)
		}
		var rawMap map[string]json.RawMessage
		if err := json.Unmarshal(raw, &rawMap); err != nil {
			t.Fatalf("decode raw map: %v", err)
		}
		if len(rawMap) != 1 {
			t.Errorf("expected only configuration in body, got %s", raw)
		}
		var cfg map[string]any
		if err := json.Unmarshal(rawMap["configuration"], &cfg); err != nil {
			t.Fatalf("decode configuration: %v", err)
		}
		if len(cfg) != 1 {
			t.Errorf("omitted flags should not appear in configuration: %s", raw)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"appId":"my-app",
			"appName":"My App",
			"status":"active",
			"configuration":{"maxWorkers":2,"idleTtlSecs":60,"scalingDelaySecs":10,"minWorkers":0,"gpusPerWorker":1,"concurrency":1,"computeType":"gpu"},
			"environmentVariables":[],
			"secrets":[],
			"createdAt":"2026-07-30T12:00:00Z",
			"updatedAt":"2026-07-30T12:00:00Z"
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	app, err := c.UpdateApp(context.Background(), testAppID, AppUpdate{
		Configuration: &WorkerConfigPatch{
			MaxWorkers: &maxWorkers,
		},
	})
	if err != nil {
		t.Fatalf("UpdateApp: %v", err)
	}
	if app.AppId != testAppID || app.Configuration.MaxWorkers != maxWorkers {
		t.Errorf("unexpected app: %+v", app)
	}
}

func TestUpdateApp_AppSource(t *testing.T) {
	sourceID := uuid.MustParse("019c7654-8b21-7abc-9123-abcdef123456")
	appSource, err := NewCodeAppSource(CodeSourceUpsert{
		BaseImage: "python:3.11-slim",
		Codebase: CodebaseSource{
			SourceId:  sourceID,
			ModelFile: testModelFile,
		},
	})
	if err != nil {
		t.Fatalf("NewCodeAppSource: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/apps/"+testAppID {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var body AppUpdate
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.AppSource == nil {
			t.Fatalf("missing appSource: %s", raw)
		}
		if body.AppName != nil || body.Configuration != nil || body.Secrets != nil || body.EnvironmentVariables != nil {
			t.Errorf("patch included out-of-scope fields: %s", raw)
		}
		var rawMap map[string]json.RawMessage
		if err := json.Unmarshal(raw, &rawMap); err != nil {
			t.Fatalf("decode raw map: %v", err)
		}
		if len(rawMap) != 1 {
			t.Errorf("expected only appSource in body, got %s", raw)
		}
		if body.AppSource.Type != AppSourceTypeCode {
			t.Errorf("appSource.type = %q, want code", body.AppSource.Type)
		}
		inner, err := body.AppSource.Source.AsCodeSourceUpsert()
		if err != nil {
			t.Fatalf("AsCodeSourceUpsert: %v", err)
		}
		if inner.Codebase.SourceId != sourceID || inner.Codebase.ModelFile != testModelFile {
			t.Errorf("unexpected codebase: %+v", inner.Codebase)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"appId":"my-app",
			"appName":"My App",
			"status":"initializing",
			"configuration":{"maxWorkers":1,"idleTtlSecs":60,"scalingDelaySecs":10,"minWorkers":0,"gpusPerWorker":1,"concurrency":1,"computeType":"gpu"},
			"environmentVariables":[],
			"secrets":[],
			"createdAt":"2026-07-30T12:00:00Z",
			"updatedAt":"2026-07-30T12:00:00Z"
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	app, err := c.UpdateApp(context.Background(), testAppID, AppUpdate{AppSource: &appSource})
	if err != nil {
		t.Fatalf("UpdateApp: %v", err)
	}
	if app.AppId != testAppID || app.Status != AppStatusInitializing {
		t.Errorf("unexpected app: %+v", app)
	}
}

func TestUpdateApp_Unprocessable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{
			"type":"about:blank",
			"title":"Unprocessable Entity",
			"status":422,
			"detail":"maxWorkers must be at least 1",
			"errors":[{"detail":"must be at least 1","pointer":"/configuration/maxWorkers"}]
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	zero := int32(0)
	_, err := c.UpdateApp(context.Background(), testAppID, AppUpdate{
		Configuration: &WorkerConfigPatch{
			MaxWorkers: &zero,
		},
	})
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeValidation {
		t.Errorf("expected CodeValidation, got %v", re.Code)
	}
	if re.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected status 422, got %d", re.StatusCode)
	}
	if !strings.Contains(re.Message, "maxWorkers must be at least 1") {
		t.Errorf("missing detail: %q", re.Message)
	}
	if !strings.Contains(re.Message, "/configuration/maxWorkers: must be at least 1") {
		t.Errorf("missing field error: %q", re.Message)
	}
}

func TestUpdateApp_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.UpdateApp(context.Background(), testAppID, AppUpdate{}); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

type lifecycleOp struct {
	name   string
	method string
	path   string
	status string
	call   func(*Client, context.Context, string) (*App, error)
	has409 bool
}

func lifecycleOps() []lifecycleOp {
	return []lifecycleOp{
		{
			name:   "StopApp",
			method: http.MethodPost,
			path:   "/v1/apps/" + testAppID + "/stop",
			status: "stopping",
			call:   (*Client).StopApp,
			has409: true,
		},
		{
			name:   "ResumeApp",
			method: http.MethodPost,
			path:   "/v1/apps/" + testAppID + "/resume",
			status: "initializing",
			call:   (*Client).ResumeApp,
			has409: true,
		},
		{
			name:   "DeleteApp",
			method: http.MethodDelete,
			path:   "/v1/apps/" + testAppID,
			status: "deleting",
			call:   (*Client).DeleteApp,
		},
	}
}

func TestLifecycleApps(t *testing.T) {
	for _, op := range lifecycleOps() {
		t.Run(op.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != op.method || r.URL.Path != op.path {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte(lifecycleAppJSON(op.status)))
			}))
			defer srv.Close()

			c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
			app, err := op.call(c, context.Background(), testAppID)
			if err != nil {
				t.Fatalf("%s: %v", op.name, err)
			}
			if app.AppId != testAppID || string(app.Status) != op.status {
				t.Errorf("unexpected app: %+v", app)
			}
		})
	}
}

func TestLifecycleApps_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"No app 'missing' exists"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	for _, op := range lifecycleOps() {
		t.Run(op.name, func(t *testing.T) {
			_, err := op.call(c, context.Background(), "missing")
			var re *transport.RunwareError
			if !errors.As(err, &re) {
				t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
			}
			if re.Code != transport.CodeNotFound {
				t.Errorf("expected CodeNotFound, got %v", re.Code)
			}
			if re.StatusCode != http.StatusNotFound {
				t.Errorf("expected status 404, got %d", re.StatusCode)
			}
			if re.Message != "No app 'missing' exists" {
				t.Errorf("unexpected message: %q", re.Message)
			}
		})
	}
}

func TestLifecycleApps_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Conflict","status":409,"detail":"App is not in the required status"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	for _, op := range lifecycleOps() {
		if !op.has409 {
			continue
		}
		t.Run(op.name, func(t *testing.T) {
			_, err := op.call(c, context.Background(), testAppID)
			var re *transport.RunwareError
			if !errors.As(err, &re) {
				t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
			}
			if re.Code != transport.CodeValidation {
				t.Errorf("expected CodeValidation, got %v", re.Code)
			}
			if re.StatusCode != http.StatusConflict {
				t.Errorf("expected status 409, got %d", re.StatusCode)
			}
			if re.Message != "App is not in the required status" {
				t.Errorf("unexpected message: %q", re.Message)
			}
		})
	}
}

func TestLifecycleApps_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	for _, op := range lifecycleOps() {
		t.Run(op.name, func(t *testing.T) {
			if _, err := op.call(c, context.Background(), testAppID); !errors.Is(err, transport.ErrNoAPIKey) {
				t.Fatalf("expected ErrNoAPIKey, got %v", err)
			}
		})
	}
}

func lifecycleAppJSON(status string) string {
	return `{
			"appId":"my-app",
			"appName":"My App",
			"status":"` + status + `",
			"configuration":{"maxWorkers":1,"idleTtlSecs":60,"scalingDelaySecs":10,"minWorkers":0,"gpusPerWorker":1,"concurrency":1,"computeType":"gpu"},
			"environmentVariables":[],
			"secrets":[],
			"createdAt":"2026-07-30T12:00:00Z",
			"updatedAt":"2026-07-30T12:00:00Z"
		}`
}

func TestListEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/endpoints"
		if r.Method != http.MethodGet || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{
			"id":"` + testEndpointID + `",
			"appId":"my-app",
			"path":"/infer",
			"createdAt":"2026-07-30T12:00:00Z"
		}]}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListEndpoints(context.Background(), testAppID, nil)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].Path != "/infer" {
		t.Fatalf("unexpected endpoints: %+v", page.Data)
	}
	if page.NextCursor != nil {
		t.Fatalf("expected nil nextCursor, got %+v", page.NextCursor)
	}
}

func TestGetEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/endpoints/" + testEndpointID
		if r.Method != http.MethodGet || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"` + testEndpointID + `",
			"appId":"` + testAppID + `",
			"path":"generate",
			"createdAt":"2026-07-30T12:00:00Z",
			"updatedAt":"2026-07-30T12:00:00Z"
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	e, err := c.GetEndpoint(context.Background(), testAppID, uuid.MustParse(testEndpointID))
	if err != nil {
		t.Fatalf("GetEndpoint: %v", err)
	}
	if e.Id.String() != testEndpointID || e.Path != "generate" || e.AppId != testAppID {
		t.Errorf("unexpected endpoint: %+v", e)
	}
}

func TestGetEndpoint_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"No endpoint exists"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.GetEndpoint(context.Background(), testAppID, uuid.MustParse(testEndpointID))
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", re.StatusCode)
	}
}

func TestGetEndpoint_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.GetEndpoint(context.Background(), testAppID, uuid.MustParse(testEndpointID)); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestListBuilds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/builds"
		if r.Method != http.MethodGet || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Errorf("limit query = %q, want 10", got)
		}
		if got := r.URL.Query().Get("cursor"); got != testCursorPage2 {
			t.Errorf("cursor query = %q, want %s", got, testCursorPage2)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{
			"id":"` + testBuildID + `",
			"status":"failed",
			"error":"pip install failed",
			"exitCode":1,
			"logTail":"ERROR: Could not find a version",
			"createdAt":"2026-07-30T12:00:00Z"
		}],"nextCursor":"` + testCursorPage3 + `"}`))
	}))
	defer srv.Close()

	limit := Limit(10)
	cursor := Cursor(testCursorPage2)
	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListBuilds(context.Background(), testAppID, &ListBuildsParams{
		Limit:  &limit,
		Cursor: &cursor,
	})
	if err != nil {
		t.Fatalf("ListBuilds: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].Id.String() != testBuildID {
		t.Fatalf("unexpected builds: %+v", page.Data)
	}
	if string(page.Data[0].Status) != "failed" {
		t.Errorf("unexpected status: %s", page.Data[0].Status)
	}
	if page.Data[0].Error == nil || *page.Data[0].Error != "pip install failed" {
		t.Errorf("unexpected error: %+v", page.Data[0].Error)
	}
	if page.NextCursor == nil || *page.NextCursor != testCursorPage3 {
		t.Fatalf("unexpected nextCursor: %+v", page.NextCursor)
	}
}

func TestListBuilds_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.ListBuilds(context.Background(), testAppID, nil); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestGetBuild(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/builds/" + testBuildID
		if r.Method != http.MethodGet || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"` + testBuildID + `",
			"status":"ready",
			"createdAt":"2026-07-30T12:00:00Z"
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	b, err := c.GetBuild(context.Background(), testAppID, uuid.MustParse(testBuildID))
	if err != nil {
		t.Fatalf("GetBuild: %v", err)
	}
	if b.Id.String() != testBuildID || string(b.Status) != testStatusReady {
		t.Errorf("unexpected build: %+v", b)
	}
}

func TestGetBuild_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"No build exists"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.GetBuild(context.Background(), testAppID, uuid.MustParse(testBuildID))
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", re.StatusCode)
	}
}

func TestGetBuild_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.GetBuild(context.Background(), testAppID, uuid.MustParse(testBuildID)); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestDeleteBuild(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/builds/" + testBuildID
		if r.Method != http.MethodDelete || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	if err := c.DeleteBuild(context.Background(), testAppID, uuid.MustParse(testBuildID)); err != nil {
		t.Fatalf("DeleteBuild: %v", err)
	}
}

func TestDeleteBuild_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"No build exists"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	err := c.DeleteBuild(context.Background(), testAppID, uuid.MustParse(testBuildID))
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", re.StatusCode)
	}
}

func TestDeleteBuild_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Conflict","status":409,"detail":"Build is still referenced by a live version"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	err := c.DeleteBuild(context.Background(), testAppID, uuid.MustParse(testBuildID))
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeValidation {
		t.Errorf("expected CodeValidation, got %v", re.Code)
	}
	if re.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409, got %d", re.StatusCode)
	}
	if re.Message != "Build is still referenced by a live version" {
		t.Errorf("unexpected message: %q", re.Message)
	}
}

func TestDeleteBuild_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if err := c.DeleteBuild(context.Background(), testAppID, uuid.MustParse(testBuildID)); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestListVersions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/versions"
		if r.Method != http.MethodGet || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{
			"id":"` + testVersionID + `",
			"appId":"my-app",
			"buildId":"` + testBuildID + `",
			"versionNumber":1,
			"createdAt":"2026-07-30T12:00:00Z"
		}]}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListVersions(context.Background(), testAppID, nil)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].VersionNumber != 1 {
		t.Fatalf("unexpected versions: %+v", page.Data)
	}
}

func TestGetVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := fmt.Sprintf("/v1/apps/%s/versions/%d", testAppID, testVersionNumber)
		if r.Method != http.MethodGet || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"` + testVersionID + `",
			"appId":"` + testAppID + `",
			"buildId":"` + testBuildID + `",
			"versionNumber":1,
			"createdAt":"2026-07-30T12:00:00Z"
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	v, err := c.GetVersion(context.Background(), testAppID, testVersionNumber)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if v.Id.String() != testVersionID || v.VersionNumber != testVersionNumber {
		t.Errorf("unexpected version: %+v", v)
	}
	if v.BuildId == nil || v.BuildId.String() != testBuildID {
		t.Errorf("unexpected buildId: %+v", v.BuildId)
	}
}

func TestGetVersion_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"No version exists"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.GetVersion(context.Background(), testAppID, 99999)
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", re.StatusCode)
	}
}

func TestGetVersion_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.GetVersion(context.Background(), testAppID, testVersionNumber); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestDeleteVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := fmt.Sprintf("/v1/apps/%s/versions/%d", testAppID, testVersionNumber)
		if r.Method != http.MethodDelete || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	if err := c.DeleteVersion(context.Background(), testAppID, testVersionNumber); err != nil {
		t.Fatalf("DeleteVersion: %v", err)
	}
}

func TestDeleteVersion_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"No version exists"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	err := c.DeleteVersion(context.Background(), testAppID, 99999)
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", re.StatusCode)
	}
}

func TestDeleteVersion_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Conflict","status":409,"detail":"Version is active"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	err := c.DeleteVersion(context.Background(), testAppID, testVersionNumber)
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeValidation {
		t.Errorf("expected CodeValidation, got %v", re.Code)
	}
	if re.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409, got %d", re.StatusCode)
	}
	if re.Message != "Version is active" {
		t.Errorf("unexpected message: %q", re.Message)
	}
}

func TestDeleteVersion_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if err := c.DeleteVersion(context.Background(), testAppID, testVersionNumber); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestDeployVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/deploy"
		if r.Method != http.MethodPost || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var req DeployRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("body is not JSON: %s", body)
		}
		if req.VersionNumber != testVersionNumber {
			t.Errorf("versionNumber = %d, want %d", req.VersionNumber, testVersionNumber)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{
			"appId":"my-app",
			"appName":"My App",
			"status":"initializing",
			"activeVersionId":"` + testVersionID + `",
			"configuration":{"maxWorkers":1,"idleTtlSecs":60,"scalingDelaySecs":10,"minWorkers":0,"gpusPerWorker":1,"concurrency":1,"computeType":"gpu"},
			"environmentVariables":[],
			"secrets":[],
			"createdAt":"2026-07-30T12:00:00Z",
			"updatedAt":"2026-07-30T12:00:00Z"
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	app, err := c.DeployVersion(context.Background(), testAppID, testVersionNumber)
	if err != nil {
		t.Fatalf("DeployVersion: %v", err)
	}
	if app.AppId != testAppID {
		t.Fatalf("unexpected app: %+v", app)
	}
	if app.ActiveVersionId == nil || app.ActiveVersionId.String() != testVersionID {
		t.Fatalf("unexpected activeVersionId: %+v", app.ActiveVersionId)
	}
}

func TestDeployVersion_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Conflict","status":409,"detail":"Version is not ready"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.DeployVersion(context.Background(), testAppID, testVersionNumber)
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeValidation {
		t.Errorf("expected CodeValidation, got %v", re.Code)
	}
	if re.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409, got %d", re.StatusCode)
	}
	if re.Message != "Version is not ready" {
		t.Errorf("unexpected message: %q", re.Message)
	}
}

func TestDeployVersion_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"No app 'missing' exists"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.DeployVersion(context.Background(), "missing", testVersionNumber)
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", re.Code)
	}
	if re.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", re.StatusCode)
	}
	if re.Message != "No app 'missing' exists" {
		t.Errorf("unexpected message: %q", re.Message)
	}
}

func TestDeployVersion_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.DeployVersion(context.Background(), testAppID, testVersionNumber); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestListWorkers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/workers"
		if r.Method != http.MethodGet || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("status"); got != testStatusReady {
			t.Errorf("status query = %q, want ready", got)
		}
		if got := r.URL.Query().Get("state"); got != string(WorkerStateFilterLive) {
			t.Errorf("state query = %q, want live", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{
			"id":"` + testWorkerID + `",
			"appId":"my-app",
			"versionId":"22222222-2222-2222-2222-222222222222",
			"status":"ready",
			"podName":"worker-0",
			"nodeName":"node-a",
			"lastSeenAt":"2026-07-30T12:01:00Z"
		}]}`))
	}))
	defer srv.Close()

	status := WorkerStatus(testStatusReady)
	state := WorkerStateFilterLive
	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListWorkers(context.Background(), testAppID, &ListWorkersParams{
		Status: &status,
		State:  &state,
	})
	if err != nil {
		t.Fatalf("ListWorkers: %v", err)
	}
	if len(page.Data) != 1 || string(page.Data[0].Status) != testStatusReady || page.Data[0].PodName != "worker-0" {
		t.Fatalf("unexpected workers: %+v", page.Data)
	}
}

func TestGetWorker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/workers/" + testWorkerID
		if r.Method != http.MethodGet || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"` + testWorkerID + `",
			"appId":"` + testAppID + `",
			"versionId":"` + testVersionID + `",
			"status":"ready",
			"podName":"worker-0",
			"nodeName":"node-a",
			"gpuType":"h100",
			"gpuCount":1,
			"createdAt":"2026-07-30T12:00:00Z",
			"statusOccurredAt":"2026-07-30T12:00:05Z",
			"lastSeenAt":"2026-07-30T12:01:00Z"
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	w, err := c.GetWorker(context.Background(), testAppID, uuid.MustParse(testWorkerID))
	if err != nil {
		t.Fatalf("GetWorker: %v", err)
	}
	if w.Id.String() != testWorkerID || w.AppId != testAppID || string(w.Status) != testStatusReady || w.PodName != "worker-0" {
		t.Errorf("unexpected worker: %+v", w)
	}
	if w.GpuType == nil || *w.GpuType != testGPUType || w.GpuCount != 1 {
		t.Errorf("unexpected gpu: type=%v count=%d", w.GpuType, w.GpuCount)
	}
	if w.VersionId.String() != testVersionID {
		t.Errorf("unexpected versionId: %s", w.VersionId)
	}
}

func TestGetWorker_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"No worker exists"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.GetWorker(context.Background(), testAppID, uuid.MustParse(testWorkerID))
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", re.StatusCode)
	}
}

func TestGetWorker_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.GetWorker(context.Background(), testAppID, uuid.MustParse(testWorkerID)); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func testAppJSON(status AppStatus) string {
	return `{
			"appId":"my-app",
			"appName":"My App",
			"status":"` + string(status) + `",
			"configuration":{"maxWorkers":1,"idleTtlSecs":60,"scalingDelaySecs":10,"minWorkers":0,"gpusPerWorker":1,"concurrency":1,"gracefulStopTtlSecs":120,"computeType":"gpu"},
			"environmentVariables":[],
			"secrets":[],
			"createdAt":"2026-07-30T12:00:00Z",
			"updatedAt":"2026-07-30T12:00:00Z"
		}`
}
