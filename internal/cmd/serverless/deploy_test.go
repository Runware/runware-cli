package serverless

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/spf13/cobra"
)

const (
	testContainerFlag = "--container"
	testWrapperDir    = "./wrapper"
	testPipPackage    = "torch"
	testSourceID      = "019c7654-8b21-7abc-9123-abcdef123456"
	testSrcDirFlag    = "--src-dir"
)

func TestValidateDeployArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		flags   []string
		wantErr string
	}{
		{
			name: "file only",
			args: []string{testModelFile},
		},
		{
			name:  "container only",
			flags: []string{testContainerFlag, testWrapperDir},
		},
		{
			name:    "neither",
			wantErr: deploySourceChoice,
		},
		{
			name:    "both",
			args:    []string{testModelFile},
			flags:   []string{testContainerFlag, testWrapperDir},
			wantErr: "not both",
		},
		{
			name:    "container with src-dir",
			flags:   []string{testContainerFlag, testWrapperDir, testSrcDirFlag, "."},
			wantErr: testSrcDirFlag,
		},
		{
			name:    "container with base-image",
			flags:   []string{testContainerFlag, testWrapperDir, "--base-image", "python:3.12-slim"},
			wantErr: "--base-image",
		},
		{
			name:    "container with requirement",
			flags:   []string{testContainerFlag, testWrapperDir, "--requirement", testPipPackage},
			wantErr: "--requirement",
		},
		{
			name:  "code with src-dir",
			args:  []string{testModelFile},
			flags: []string{testSrcDirFlag, "."},
		},
		{
			name: "code with default base-image is allowed",
			args: []string{testModelFile},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newDeployCmd(nil)
			if err := cmd.ParseFlags(tc.flags); err != nil {
				t.Fatalf("ParseFlags: %v", err)
			}
			containerDir, err := cmd.Flags().GetString("container")
			if err != nil {
				t.Fatalf("container: %v", err)
			}
			err = validateDeployArgs(cmd, tc.args, containerDir)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestBuildDeployArchive_Code(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		testModelFile: testPySource,
	})

	archive, source, err := buildDeployArchive(dir, "", "python:3.11-slim", []string{testPipPackage}, []string{testModelFile})
	if err != nil {
		t.Fatalf("buildDeployArchive: %v", err)
	}
	if source.sourceType != serverlessapi.AppSourceTypeCode {
		t.Errorf("sourceType = %q, want code", source.sourceType)
	}
	if len(archive) == 0 {
		t.Fatal("empty archive")
	}

	id := uuid.MustParse(testSourceID)
	appSource, err := source.appSource(id)
	if err != nil {
		t.Fatalf("appSource: %v", err)
	}
	if appSource.Type != serverlessapi.AppSourceTypeCode {
		t.Errorf("type = %q, want code", appSource.Type)
	}
	inner, err := appSource.Source.AsCodeSourceUpsert()
	if err != nil {
		t.Fatalf("AsCodeSourceUpsert: %v", err)
	}
	if inner.Codebase.SourceId != id || inner.Codebase.ModelFile != testModelFile {
		t.Errorf("codebase = %+v", inner.Codebase)
	}
	if inner.Requirements == nil || len(*inner.Requirements) != 1 || (*inner.Requirements)[0] != testPipPackage {
		t.Errorf("requirements = %v, want [%s]", inner.Requirements, testPipPackage)
	}
}

func TestBuildDeployArchive_Container(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		containerDockerfile: testDockerfile,
		containerConfig:     testContainer,
	})

	archive, source, err := buildDeployArchive("", dir, "python:3.11-slim", []string{testPipPackage}, nil)
	if err != nil {
		t.Fatalf("buildDeployArchive: %v", err)
	}
	if source.sourceType != serverlessapi.AppSourceTypeContainer {
		t.Errorf("sourceType = %q, want container", source.sourceType)
	}
	if len(archive) == 0 {
		t.Fatal("empty archive")
	}

	id := uuid.MustParse(testSourceID)
	appSource, err := source.appSource(id)
	if err != nil {
		t.Fatalf("appSource: %v", err)
	}
	if appSource.Type != serverlessapi.AppSourceTypeContainer {
		t.Errorf("type = %q, want container", appSource.Type)
	}
	inner, err := appSource.Source.AsContainerSource()
	if err != nil {
		t.Fatalf("AsContainerSource: %v", err)
	}
	if inner.SourceId != id {
		t.Errorf("sourceId = %s, want %s", inner.SourceId, id)
	}

	raw, err := json.Marshal(appSource)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "baseImage") || strings.Contains(string(raw), "modelFile") {
		t.Errorf("container source leaked code fields: %s", raw)
	}
}

func TestDeploySource_UnsupportedType(t *testing.T) {
	var source deploySource
	_, err := source.appSource(uuid.MustParse(testSourceID))
	if err == nil {
		t.Fatal("expected an error for an empty source type")
	}
}

func TestNewDeployCmd_RegistersContainerFlag(t *testing.T) {
	cmd := newDeployCmd(nil)
	if cmd.Flags().Lookup("container") == nil {
		t.Fatal("deploy is missing --container")
	}
	if cmd.Flags().Lookup("wait") == nil {
		t.Fatal("deploy is missing --wait")
	}
	if cmd.Flags().Lookup("poll-interval") == nil {
		t.Fatal("deploy is missing --poll-interval")
	}
	if cmd.Use != "deploy [file]" {
		t.Errorf("Use = %q, want deploy [file]", cmd.Use)
	}
	if cmd.Short != "Create or update a serverless application" {
		t.Errorf("Short = %q", cmd.Short)
	}
	if vals := cmd.Flags().Lookup("gpu-type").Annotations[cobra.BashCompOneRequiredFlag]; len(vals) > 0 {
		t.Fatal("gpu-type should not be cobra-required")
	}
}

func TestExistingApp(t *testing.T) {
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"No app 'my-app' exists"}`))
	}))
	defer missing.Close()

	ok, err := existingApp(context.Background(), serverlessapi.NewClient("test-key", missing.URL, slog.Default()), testAppID)
	if err != nil || ok {
		t.Fatalf("404: ok=%v err=%v", ok, err)
	}

	found := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"appId":"my-app",
			"appName":"My App",
			"status":"active",
			"configuration":{"maxWorkers":1,"idleTtlSecs":60,"scalingDelaySecs":10,"minWorkers":0,"gpusPerWorker":1,"gracefulStopTtlSecs":120,"computeType":"gpu"},
			"environmentVariables":[],
			"secrets":[],
			"createdAt":"2026-07-30T12:00:00Z",
			"updatedAt":"2026-07-30T12:00:00Z"
		}`))
	}))
	defer found.Close()

	ok, err = existingApp(context.Background(), serverlessapi.NewClient("test-key", found.URL, slog.Default()), testAppID)
	if err != nil || !ok {
		t.Fatalf("200: ok=%v err=%v", ok, err)
	}

	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer fail.Close()

	if _, err := existingApp(context.Background(), serverlessapi.NewClient("test-key", fail.URL, slog.Default()), testAppID); err == nil {
		t.Fatal("expected an error for 500")
	}
}

func TestValidateGPUsPerWorker(t *testing.T) {
	for _, n := range []int32{1, 2, 4, 8} {
		if err := validateGPUsPerWorker(n); err != nil {
			t.Errorf("validateGPUsPerWorker(%d): %v", n, err)
		}
	}
	for _, n := range []int32{0, 3, 5, 16} {
		err := validateGPUsPerWorker(n)
		if err == nil || !strings.Contains(err.Error(), gpusPerWorkerValuesText()) {
			t.Errorf("validateGPUsPerWorker(%d) = %v, want an allowed-values error", n, err)
		}
	}
}

func TestDeploy_RejectsInvalidGPUsPerWorkerBeforeUpload(t *testing.T) {
	cmd := newDeployCmd(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{testModelFile, "--id", testAppID, "--gpus-per-worker", "3"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), gpusPerWorkerValuesText()) {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateAvailableWorkersPct(t *testing.T) {
	for _, n := range []int32{0, 50, 100} {
		if err := validateAvailableWorkersPct(n); err != nil {
			t.Errorf("validateAvailableWorkersPct(%d): %v", n, err)
		}
	}
	for _, n := range []int32{-1, 101} {
		err := validateAvailableWorkersPct(n)
		if err == nil || !strings.Contains(err.Error(), "0 and 100") {
			t.Errorf("validateAvailableWorkersPct(%d) = %v, want a range error", n, err)
		}
	}
}

func TestDeployCreate_RejectsAvailableWorkersPctBeforeUpload(t *testing.T) {
	cmd := newDeployCmd(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{testModelFile, "--id", testAppID, "--available-workers-pct", "101"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "0 and 100") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeployCreate_SendsSecretsAndMinAvailableWorkers(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{testModelFile: testPySource})

	var created serverlessapi.AppCreate
	var creates int
	stage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stage.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/apps/"):
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/source-uploads":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{
				"upload": {"id":"` + testSourceID + `","declaredByteLength":1,"sha256":"00","sourceType":"code","state":"pending","expiresAt":"2026-09-02T12:00:00Z","createdAt":"2026-09-02T11:00:00Z","updatedAt":"2026-09-02T11:00:00Z"},
				"transfer": {"mode":"singlePut","method":"PUT","url":"` + stage.URL + `/obj","headers":{"Content-Type":"application/zip"},"expiresAt":"2026-09-02T12:00:00Z"}
			}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"` + testSourceID + `","declaredByteLength":1,"sha256":"00","sourceType":"code","sourceId":"` + testSourceID + `","state":"ready","expiresAt":"2026-09-02T12:00:00Z","createdAt":"2026-09-02T11:00:00Z","updatedAt":"2026-09-02T11:00:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/apps":
			creates++
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Errorf("decode create: %v", err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(activeAppBody("")))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()

	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", api.URL)

	cmd := newDeployCmd(log.New(io.Discard))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		testModelFile,
		"--src-dir", dir,
		"--id", testAppID,
		testGPUTypeFlag, testGPUType,
		"--secret", "API_KEY=INFERENCE_KEY",
		"--min-available-workers", "1",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if creates != 1 {
		t.Fatalf("creates = %d, want 1", creates)
	}
	if created.Secrets == nil || len(*created.Secrets) != 1 {
		t.Fatalf("secrets = %#v", created.Secrets)
	}
	sec := (*created.Secrets)[0]
	if sec.SecretName != "API_KEY" || sec.EnvVarName == nil || *sec.EnvVarName != "INFERENCE_KEY" {
		t.Fatalf("secret = %#v", sec)
	}
	if created.Configuration.MinAvailableWorkers == nil || *created.Configuration.MinAvailableWorkers != 1 {
		t.Fatalf("minAvailableWorkers = %#v", created.Configuration.MinAvailableWorkers)
	}
}

func TestValidateCreateDeployGPU(t *testing.T) {
	if err := validateCreateDeployGPU(""); err == nil || !strings.Contains(err.Error(), testGPUTypeFlag) {
		t.Fatalf("empty: %v", err)
	}
	if err := validateCreateDeployGPU("h100"); err != nil {
		t.Fatalf("h100: %v", err)
	}
}

func TestParseSecretAttaches(t *testing.T) {
	got, err := parseSecretAttaches(nil)
	if err != nil || got != nil {
		t.Fatalf("empty: got=%v err=%v", got, err)
	}

	got, err = parseSecretAttaches([]string{"ORG_TOKEN", "API_KEY=INFERENCE_KEY"})
	if err != nil || got == nil || len(*got) != 2 {
		t.Fatalf("attaches: got=%v err=%v", got, err)
	}
	if (*got)[0].SecretName != "ORG_TOKEN" || (*got)[0].EnvVarName != nil {
		t.Fatalf("name only: %#v", (*got)[0])
	}
	if (*got)[1].SecretName != "API_KEY" || (*got)[1].EnvVarName == nil || *(*got)[1].EnvVarName != "INFERENCE_KEY" {
		t.Fatalf("env override: %#v", (*got)[1])
	}

	for _, raw := range []string{"", "=NOPE", "NAME=", "bad-name", "1TOKEN"} {
		if _, err := parseSecretAttaches([]string{raw}); err == nil || !strings.Contains(err.Error(), "invalid --secret") {
			t.Fatalf("%q: got %v", raw, err)
		}
	}
}

func TestValidateUpdateDeployFlags(t *testing.T) {
	cmd := newDeployCmd(nil)
	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if err := validateUpdateDeployFlags(cmd); err != nil {
		t.Fatalf("no create flags: %v", err)
	}

	const scaleHint = "apps scale"
	cases := []struct {
		flags   []string
		wantErr string
	}{
		{flags: []string{testGPUTypeFlag, "h100"}, wantErr: scaleHint},
		{flags: []string{"--max-workers", "2"}, wantErr: scaleHint},
		{flags: []string{"--env", "FOO=bar"}, wantErr: "apps env"},
		{flags: []string{"--env-file", envDotfile}, wantErr: "apps env"},
		{flags: []string{"--volume", "/data"}, wantErr: "immutable"},
		{flags: []string{"--secret", "ORG_TOKEN"}, wantErr: "secrets attach"},
		{flags: []string{"--fallback-gpu-type", "l40s"}, wantErr: scaleHint},
		{flags: []string{"--min-available-workers", "1"}, wantErr: scaleHint},
		{flags: []string{"--available-workers-pct", "10"}, wantErr: scaleHint},
		{flags: []string{"--name", "My App"}, wantErr: "omit it"},
		{flags: []string{"--requirement", testPipPackage}},
		{flags: []string{"--base-image", "python:3.12-slim"}},
		{flags: []string{testSrcDirFlag, "."}},
		{flags: []string{"--wait"}},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.flags, " "), func(t *testing.T) {
			cmd := newDeployCmd(nil)
			if err := cmd.ParseFlags(tc.flags); err != nil {
				t.Fatalf("ParseFlags: %v", err)
			}
			err := validateUpdateDeployFlags(cmd)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestDeployWaitMessage(t *testing.T) {
	if got := deployWaitMessage(testAppID, "initializing", ""); got != "Waiting for application my-app (initializing)..." {
		t.Fatalf("no build: %q", got)
	}
	if got := deployWaitMessage(testAppID, "initializing", "building"); got != "Waiting for application my-app (initializing, build building)..." {
		t.Fatalf("building: %q", got)
	}
}

func TestWaitForAppDeploy_ReportsBuildStatus(t *testing.T) {
	var gets int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/builds") {
			_, _ = w.Write([]byte(`{"data":[{"id":"33333333-3333-3333-3333-333333333333","status":"building","phases":[]}]}`))
			return
		}
		gets++
		status := string(serverlessapi.AppStatusInitializing)
		if gets > 1 {
			status = string(serverlessapi.AppStatusActive)
		}
		_, _ = w.Write([]byte(`{"appId":"` + testAppID + `","appName":"My App","status":"` + status + `","configuration":{"maxWorkers":1,"idleTtlSecs":60,"scalingDelaySecs":10,"computeType":"gpu","gpuType":"h100"},"environmentVariables":[],"secrets":[],"createdAt":"2026-07-30T12:00:00Z","updatedAt":"2026-07-30T12:00:00Z"}`))
	}))
	defer srv.Close()

	var seen []string
	client := serverlessapi.NewClient("test-key", srv.URL, slog.Default())
	app, err := waitForAppDeploy(context.Background(), client, testAppID, time.Millisecond, func(appStatus, buildStatus string) {
		seen = append(seen, deployWaitMessage(testAppID, appStatus, buildStatus))
	})
	if err != nil {
		t.Fatalf("waitForAppDeploy: %v", err)
	}
	if app.Status != serverlessapi.AppStatusActive {
		t.Fatalf("status = %s", app.Status)
	}
	if len(seen) != 1 || seen[0] != "Waiting for application my-app (initializing, build building)..." {
		t.Fatalf("reports = %#v", seen)
	}
}

func TestWaitForAppDeploy_ContinuesWhenBuildsFail(t *testing.T) {
	var gets int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/builds") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"unavailable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		gets++
		status := string(serverlessapi.AppStatusInitializing)
		if gets > 1 {
			status = string(serverlessapi.AppStatusActive)
		}
		_, _ = w.Write([]byte(`{"appId":"` + testAppID + `","appName":"My App","status":"` + status + `","configuration":{"maxWorkers":1,"idleTtlSecs":60,"scalingDelaySecs":10,"computeType":"gpu","gpuType":"h100"},"environmentVariables":[],"secrets":[],"createdAt":"2026-07-30T12:00:00Z","updatedAt":"2026-07-30T12:00:00Z"}`))
	}))
	defer srv.Close()

	var seen []string
	client := serverlessapi.NewClient("test-key", srv.URL, slog.Default())
	app, err := waitForAppDeploy(context.Background(), client, testAppID, time.Millisecond, func(appStatus, buildStatus string) {
		seen = append(seen, deployWaitMessage(testAppID, appStatus, buildStatus))
	})
	if err != nil {
		t.Fatalf("waitForAppDeploy: %v", err)
	}
	if app.Status != serverlessapi.AppStatusActive {
		t.Fatalf("status = %s", app.Status)
	}
	if len(seen) != 1 || seen[0] != "Waiting for application my-app (initializing)..." {
		t.Fatalf("reports = %#v", seen)
	}
}

func TestAppFailedErr(t *testing.T) {
	if err := appFailedErr(context.Background(), nil, nil); err != nil {
		t.Fatalf("nil app: %v", err)
	}

	active := &serverlessapi.App{
		AppId:  testAppID,
		Status: serverlessapi.AppStatusActive,
	}
	if err := appFailedErr(context.Background(), nil, active); err != nil {
		t.Fatalf("active: %v", err)
	}

	stopped := &serverlessapi.App{
		AppId:  testAppID,
		Status: serverlessapi.AppStatusStopped,
	}
	err := appFailedErr(context.Background(), nil, stopped)
	if err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatalf("stopped: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apps/"+testAppID+"/builds" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"33333333-3333-3333-3333-333333333333","status":"failed","error":"pip install failed","createdAt":"2026-07-30T12:00:00Z"}]}`))
	}))
	defer srv.Close()

	client := serverlessapi.NewClient("test-key", srv.URL, slog.Default())
	failed := &serverlessapi.App{
		AppId:  testAppID,
		Status: serverlessapi.AppStatusFailed,
	}
	err = appFailedErr(context.Background(), client, failed)
	if err == nil || !strings.Contains(err.Error(), "pip install failed") {
		t.Fatalf("failed with build error: %v", err)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer empty.Close()

	err = appFailedErr(context.Background(), serverlessapi.NewClient("test-key", empty.URL, slog.Default()), failed)
	if err == nil || !strings.Contains(err.Error(), "inspect builds") {
		t.Fatalf("failed without build error: %v", err)
	}
}
