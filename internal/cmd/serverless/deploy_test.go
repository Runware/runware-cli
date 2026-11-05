package serverless

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
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
	testFromFlag      = "--from"
	testFallbackGPU   = "l40s"
	testBaseImageFlag = "--base-image"
	testRequirement   = "--requirement"
	testArchiveURL    = "https://runware.ai/docs/serverless/examples/echo.zip"
	testIDFlag        = "--id"
	testVolumePath    = "/weights"
	testWaitFlag      = "--wait"
	testTimeoutFlag   = "--timeout"
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
			name:    "a file and a container",
			args:    []string{testModelFile},
			flags:   []string{testContainerFlag, testWrapperDir},
			wantErr: "not more than one",
		},
		{
			name:  "from only",
			flags: []string{testFromFlag, testArchiveURL},
		},
		{
			name:    "from and a file",
			args:    []string{testModelFile},
			flags:   []string{testFromFlag, testArchiveURL},
			wantErr: "not more than one",
		},
		{
			name:    "from and a container",
			flags:   []string{testFromFlag, testArchiveURL, testContainerFlag, testWrapperDir},
			wantErr: "not more than one",
		},
		{
			name:    "from with src-dir",
			flags:   []string{testFromFlag, testArchiveURL, testSrcDirFlag, "."},
			wantErr: testSrcDirFlag,
		},
		{
			name:  "from with a flag that overrides the manifest",
			flags: []string{testFromFlag, testArchiveURL, "--gpu-type", testFallbackGPU},
		},
		{
			name:    "container with src-dir",
			flags:   []string{testContainerFlag, testWrapperDir, testSrcDirFlag, "."},
			wantErr: testSrcDirFlag,
		},
		{
			name:    "container with base-image",
			flags:   []string{testContainerFlag, testWrapperDir, testBaseImageFlag, "python:3.12-slim"},
			wantErr: testBaseImageFlag,
		},
		{
			name:    "container with requirement",
			flags:   []string{testContainerFlag, testWrapperDir, testRequirement, testPipPackage},
			wantErr: testRequirement,
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
			from, err := cmd.Flags().GetString("from")
			if err != nil {
				t.Fatalf("from: %v", err)
			}
			err = validateDeployArgs(cmd, tc.args, containerDir, from)
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
	if cmd.Flags().Lookup("timeout") == nil {
		t.Fatal("deploy is missing --timeout")
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

type existingAppCase struct {
	name       string
	status     int
	body       string
	wantExists bool
	wantErr    bool
}

func TestExistingApp(t *testing.T) {
	cases := []existingAppCase{
		{
			name:   "no copy",
			status: http.StatusNotFound,
			body:   `{"type":"about:blank","title":"Not Found","status":404,"detail":"No app 'my-app' exists"}`,
		},
		{
			name:       "copy exists",
			status:     http.StatusOK,
			body:       activeAppBody(""),
			wantExists: true,
		},
		{
			name:    "lookup fails",
			status:  http.StatusInternalServerError,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			exists, err := existingApp(context.Background(), serverlessapi.NewClient("test-key", srv.URL, slog.Default()), testAppID)

			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if exists != tc.wantExists {
				t.Fatalf("exists = %v, want %v", exists, tc.wantExists)
			}
		})
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

func TestDeploy_RejectsPaddedNameBeforeUpload(t *testing.T) {
	cmd := newDeployCmd(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{testModelFile, testIDFlag, testAppID, "--name", " padded"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "non-whitespace") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeploy_RejectsInvalidGPUsPerWorkerBeforeUpload(t *testing.T) {
	cmd := newDeployCmd(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{testModelFile, testIDFlag, testAppID, "--gpus-per-worker", "3"})
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
	cmd.SetArgs([]string{testModelFile, testIDFlag, testAppID, "--available-workers-pct", "101"})
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
	serveSourceUpload := newSourceUploadStub(t)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveSourceUpload(w, r) {
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/apps/"):
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404}`))
		case r.Method == http.MethodPost && r.URL.Path == testAppsPath:
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
		testIDFlag, testAppID,
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
		{flags: []string{"--env-var", "FOO=bar"}, wantErr: "apps env-vars"},
		{flags: []string{"--env-file", envDotfile}, wantErr: "apps env-vars"},
		{flags: []string{"--volume", "/data"}, wantErr: "immutable"},
		{flags: []string{"--secret", "ORG_TOKEN"}, wantErr: "secrets attach"},
		{flags: []string{"--fallback-gpu-type", testFallbackGPU}, wantErr: scaleHint},
		{flags: []string{"--min-available-workers", "1"}, wantErr: scaleHint},
		{flags: []string{"--available-workers-pct", "10"}, wantErr: scaleHint},
		{flags: []string{"--name", "My App"}, wantErr: "apps rename"},
		{flags: []string{testRequirement, testPipPackage}},
		{flags: []string{testBaseImageFlag, "python:3.12-slim"}},
		{flags: []string{testSrcDirFlag, "."}},
		{flags: []string{testWaitFlag}},
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

func TestDeployWait_TimeoutDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{testModelFile: testPySource})

	serveSourceUpload := newSourceUploadStub(t)

	var gets int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveSourceUpload(w, r) {
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/apps/"):
			gets++
			if gets == 1 {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(strings.Replace(activeAppBody(""), `"status":"active"`, `"status":"initializing"`, 1)))
		case r.Method == http.MethodPost && r.URL.Path == testAppsPath:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(strings.Replace(activeAppBody(""), `"status":"active"`, `"status":"initializing"`, 1)))
		default:
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
		testIDFlag, testAppID,
		testGPUTypeFlag, testGPUType,
		testWaitFlag,
		testTimeoutFlag, "50ms",
		"--poll-interval", "10ms",
	})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "timed out waiting for application "+testAppID) {
		t.Fatalf("err = %v", err)
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
	if err := appFailedErr(context.Background(), nil, nil, ""); err != nil {
		t.Fatalf("nil app: %v", err)
	}

	active := &serverlessapi.App{
		AppId:  testAppID,
		Status: serverlessapi.AppStatusActive,
	}
	if err := appFailedErr(context.Background(), nil, active, ""); err != nil {
		t.Fatalf("active: %v", err)
	}

	stopped := &serverlessapi.App{
		AppId:  testAppID,
		Status: serverlessapi.AppStatusStopped,
	}
	err := appFailedErr(context.Background(), nil, stopped, "")
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
	err = appFailedErr(context.Background(), client, failed, "")
	if err == nil || !strings.Contains(err.Error(), "pip install failed") {
		t.Fatalf("failed with build error: %v", err)
	}
}

type buildsHintCase struct {
	name               string
	environment        string
	defaultEnvironment string
	wantHint           string
}

func TestAppFailedErr_BuildsHintAddressesTheSameCopy(t *testing.T) {
	cases := []buildsHintCase{
		{
			name:        "production copy",
			environment: testProductionEnvironment,
			wantHint:    "'runware serverless apps builds list " + testAppID + "'",
		},
		{
			name:        "staging copy",
			environment: testEnvironment,
			wantHint:    "'runware serverless apps builds list " + testAppID + " --env " + testEnvironment + "'",
		},
		{
			name:               "production copy under a staging default",
			environment:        testProductionEnvironment,
			defaultEnvironment: testEnvironment,
			wantHint:           "'runware serverless apps builds list " + testAppID + " --env " + testProductionEnvironment + "'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[]}`))
			}))
			defer empty.Close()
			failed := &serverlessapi.App{
				AppId:       testAppID,
				Environment: tc.environment,
				Status:      serverlessapi.AppStatusFailed,
			}

			err := appFailedErr(context.Background(), serverlessapi.NewClient("test-key", empty.URL, slog.Default()), failed, tc.defaultEnvironment)

			if err == nil || !strings.Contains(err.Error(), tc.wantHint) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.wantHint)
			}
		})
	}
}

// --from cannot judge the code-only flags up front, because the source type is
// inside the archive. Once the manifest says container, they have to be refused
// rather than accepted and dropped by the packer.
func TestRefuseCodeOnlyFlags(t *testing.T) {
	for _, tc := range []struct {
		name    string
		flags   []string
		wantErr string
	}{
		{
			name:  "nothing code-only passed",
			flags: []string{testFromFlag, testArchiveURL},
		},
		{
			name:    "base-image",
			flags:   []string{testFromFlag, testArchiveURL, testBaseImageFlag, "python:3.12-slim"},
			wantErr: testBaseImageFlag,
		},
		{
			name:    "requirement",
			flags:   []string{testFromFlag, testArchiveURL, testRequirement, testPipPackage},
			wantErr: testRequirement,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newDeployCmd(nil)
			if err := cmd.ParseFlags(tc.flags); err != nil {
				t.Fatalf("ParseFlags: %v", err)
			}

			// What validateDeployArgs cannot do before the fetch, and what the
			// deploy does as soon as the manifest names a container source.
			if err := validateDeployArgs(cmd, nil, "", testArchiveURL); err != nil {
				t.Fatalf("the source choice itself was refused: %v", err)
			}

			err := refuseCodeOnlyFlags(cmd, "this archive holds a container source")
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
				t.Errorf("error %q does not name %q", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), "container source") {
				t.Errorf("error %q does not say why", err)
			}
		})
	}
}

// deployFromArchiveManifest is a packaged code source whose manifest carries a
// GPU type and a volume. Both are create-only flags, which is what makes it the
// fixture for the redeploy rule below.
const deployFromArchiveManifest = `configVersion: 1
id: ` + testAppID + `
source:
  type: code
  entry: ` + testModelFile + `
compute:
  gpuType: ` + testGPUType + `
volumes:
  - /weights
`

// serveDeployFromArchive packages the manifest with its entry file and returns
// the URL to deploy from.
func serveDeployFromArchive(t *testing.T) string {
	t.Helper()
	return serveArchive(t, buildTestArchive(t, []archiveEntry{
		{name: manifestFile, body: deployFromArchiveManifest},
		{name: testModelFile, body: testPySource},
	}))
}

func TestDeployFrom_CreatesFromTheManifest(t *testing.T) {
	from := serveDeployFromArchive(t)

	var created serverlessapi.AppCreate
	var creates int
	serveSourceUpload := newSourceUploadStub(t)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveSourceUpload(w, r) {
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/apps/"):
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404}`))
		case r.Method == http.MethodPost && r.URL.Path == testAppsPath:
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
	cmd.SetArgs([]string{testFromFlag, from})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("deploy %s: %v", testFromFlag, err)
	}

	if creates != 1 {
		t.Fatalf("creates = %d, want 1", creates)
	}
	if created.AppId != testAppID {
		t.Fatalf("appId = %q, want %q", created.AppId, testAppID)
	}
	if created.Configuration.GpuType != testGPUType {
		t.Fatalf("gpuType = %q, want %q", created.Configuration.GpuType, testGPUType)
	}
	if created.Volumes == nil || len(*created.Volumes) != 1 || (*created.Volumes)[0].MountPath != testVolumePath {
		t.Fatalf("volumes = %#v", created.Volumes)
	}
}

// TestDeployFrom_RedeploysAnExistingApp fixes the redeploy rule: the manifest
// describes the app rather than asking to change a running one, so its
// create-only GPU type and volume must not be refused on an update.
func TestDeployFrom_RedeploysAnExistingApp(t *testing.T) {
	from := serveDeployFromArchive(t)

	var creates, updates int
	serveSourceUpload := newSourceUploadStub(t)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveSourceUpload(w, r) {
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/apps/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(activeAppBody("")))
		case r.Method == http.MethodPatch && r.URL.Path == testAppsPath+"/"+testAppID:
			updates++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(activeAppBody("")))
		case r.Method == http.MethodPost && r.URL.Path == testAppsPath:
			creates++
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

	var stderr bytes.Buffer
	cmd := newDeployCmd(log.New(io.Discard))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{testFromFlag, from})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("redeploy %s: %v", testFromFlag, err)
	}

	// The manifest carries a volume and a compute block, and an update sends
	// neither, so the command has to say which sections it dropped.
	for _, want := range []string{"volumes", "compute", "the source only"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to mention %q", stderr.String(), want)
		}
	}

	if updates != 1 {
		t.Fatalf("updates = %d, want 1", updates)
	}
	if creates != 0 {
		t.Fatalf("creates = %d, want 0", creates)
	}
}

func TestUnappliedManifestSections(t *testing.T) {
	full := deployManifest{
		Volumes: []string{testVolumePath},
		EnvVars: map[string]string{"A": "1"},
		Secrets: []manifestSecret{{Name: "HF_TOKEN"}},
		Compute: manifestCompute{GpuType: testGPUType},
	}

	if got := unappliedManifestSections("", full); got != nil {
		t.Fatalf("without --from: %v, want nil", got)
	}
	want := []string{"volumes", "envVars", "secrets", "compute"}
	if got := unappliedManifestSections(testArchiveURL, full); !slices.Equal(got, want) {
		t.Fatalf("full manifest: %v, want %v", got, want)
	}
	if got := unappliedManifestSections(testArchiveURL, deployManifest{}); got != nil {
		t.Fatalf("empty manifest: %v, want nil", got)
	}
}

// deployFromEnvManifest carries two environment variables, so a single
// --env-var can be seen to override one of them and keep the other.
const deployFromEnvManifest = `configVersion: 1
id: ` + testAppID + `
source:
  type: code
  entry: ` + testModelFile + `
compute:
  gpuType: ` + testGPUType + `
envVars:
  MODEL_REVISION: main
  CACHE_DIR: /weights
`

func TestDeployFrom_EnvVarOverridesOneManifestKey(t *testing.T) {
	from := serveArchive(t, buildTestArchive(t, []archiveEntry{
		{name: manifestFile, body: deployFromEnvManifest},
		{name: testModelFile, body: testPySource},
	}))

	var created serverlessapi.AppCreate
	serveSourceUpload := newSourceUploadStub(t)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveSourceUpload(w, r) {
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/apps/"):
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404}`))
		case r.Method == http.MethodPost && r.URL.Path == testAppsPath:
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
		testFromFlag, from,
		"--" + envVarFlag, "MODEL_REVISION=abc123",
		"--" + envVarFlag, "EXTRA=1",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("deploy %s: %v", testFromFlag, err)
	}

	if created.EnvironmentVariables == nil {
		t.Fatal("environmentVariables = nil")
	}
	want := map[string]string{
		"MODEL_REVISION": "abc123",
		"CACHE_DIR":      testVolumePath,
		"EXTRA":          "1",
	}
	if got := *created.EnvironmentVariables; !maps.Equal(got, want) {
		t.Fatalf("environmentVariables = %v, want %v", got, want)
	}
}
