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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/spf13/cobra"
)

const (
	testContainerFlag           = "--container"
	testWrapperDir              = "./wrapper"
	testPipPackage              = "torch"
	testSourceID                = "019c7654-8b21-7abc-9123-abcdef123456"
	testSrcDirFlag              = "--src-dir"
	testFromFlag                = "--from"
	testFallbackGPU             = "l40s"
	testBaseImageFlag           = "--base-image"
	testBaseImage               = "python:3.12-slim"
	testRequirement             = "--requirement"
	testArchiveURL              = "https://runware.ai/docs/serverless/examples/echo.zip"
	testIDFlag                  = "--id"
	testVolumePath              = "/weights"
	testWaitFlag                = "--wait"
	testTimeoutFlag             = "--timeout"
	testMinWorkersFlag          = "--min-workers"
	testIdleTTLFlag             = "--idle-ttl"
	testScalingDelayFlag        = "--scaling-delay"
	testGPUsPerWorkerFlag       = "--gpus-per-worker"
	testFallbackGPUTypeFlag     = "--fallback-gpu-type"
	testMinAvailableWorkersFlag = "--min-available-workers"
	testAvailableWorkersPctFlag = "--available-workers-pct"
	testNameFlag                = "--name"
	testSecretFlag              = "--secret"
	testEnvFileFlag             = "--env-file"
	testVolumeFlag              = "--volume"
	testOrgToken                = "ORG_TOKEN"
	testDisplayName             = "My App"
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
			flags: []string{testFromFlag, testArchiveURL, testGPUTypeFlag, testFallbackGPU},
		},
		{
			name:    "container with src-dir",
			flags:   []string{testContainerFlag, testWrapperDir, testSrcDirFlag, "."},
			wantErr: testSrcDirFlag,
		},
		{
			name:    "container with base-image",
			flags:   []string{testContainerFlag, testWrapperDir, testBaseImageFlag, testBaseImage},
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

			app, err := existingApp(context.Background(), serverlessapi.NewClient("test-key", srv.URL, slog.Default()), testAppID)

			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if got := app != nil; got != tc.wantExists {
				t.Fatalf("app = %v, want exists %v", app, tc.wantExists)
			}
			if tc.wantExists && app.Status != serverlessapi.AppStatusActive {
				t.Fatalf("status = %q, want the getApp body returned", app.Status)
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
	cmd.SetArgs([]string{testModelFile, testIDFlag, testAppID, testNameFlag, " padded"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "non-whitespace") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeploy_RejectsInvalidGPUsPerWorkerBeforeUpload(t *testing.T) {
	cmd := newDeployCmd(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{testModelFile, testIDFlag, testAppID, testGPUsPerWorkerFlag, "3"})
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
	cmd.SetArgs([]string{testModelFile, testIDFlag, testAppID, testAvailableWorkersPctFlag, "101"})
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
		testSecretFlag, "API_KEY=INFERENCE_KEY",
		testMinAvailableWorkersFlag, "1",
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

// storedDeployAppBody is a getApp response whose create-only settings differ
// from both a zero App and the flag defaults. A redeploy that compared a
// stand-in app, or skipped the comparison, cannot accept these same flags.
func storedDeployAppBody() string {
	return `{
		"appId":"` + testAppID + `","appName":"My App","status":"active",
		"configuration":{"maxWorkers":1,"idleTtlSecs":61,"scalingDelaySecs":10,"minWorkers":0,"gpusPerWorker":1,"computeType":"gpu","gpuType":"` + testGPUType + `"},
		"environmentVariables":[{"key":"FOO","value":"bar"}],
		"secrets":[{"id":"` + testUUID + `","name":"ORG_TOKEN","type":"generic","environment":"production"}],
		"createdAt":"2026-07-30T12:00:00Z","updatedAt":"2026-07-30T12:00:00Z"
	}`
}

// runDeployAgainstExistingApp runs deploy against a getApp that returns body,
// and reports how many creates and updates the stub saw.
func runDeployAgainstExistingApp(t *testing.T, body string, args ...string) (int, int, error) {
	t.Helper()
	var updates, creates int
	serveSourceUpload := newSourceUploadStub(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveSourceUpload(w, r) {
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, testAppsPath+"/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, testAppsPath+"/"):
			updates++
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == testAppsPath:
			creates++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, body)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(api.Close)

	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", api.URL)

	cmd := newDeployCmd(log.New(io.Discard))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	return updates, creates, cmd.Execute()
}

// TestDeployUpdate_AcceptsMatchingCreateOnlyFlags locks the path from
// resolveDeployTarget to validateUpdateDeployFlags(cmd, app): the stub app is
// the one compared. The same gpu type, idle ttl, environment variable and
// secret reach UpdateApp. --volume still fails, and the error names it alone,
// because that read does not return volumes.
func TestDeployUpdate_AcceptsMatchingCreateOnlyFlags(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{testModelFile: testPySource})
	body := storedDeployAppBody()
	matching := []string{
		testModelFile,
		testSrcDirFlag, dir,
		testIDFlag, testAppID,
		testGPUTypeFlag, testGPUType,
		testIdleTTLFlag, "61",
		"--" + envVarFlag, "FOO=bar",
		testSecretFlag, "ORG_TOKEN=ORG_TOKEN",
	}

	t.Run("matching flags update", func(t *testing.T) {
		updates, creates, err := runDeployAgainstExistingApp(t, body, matching...)
		if err != nil {
			t.Fatalf("deploy: %v", err)
		}
		if updates != 1 || creates != 0 {
			t.Fatalf("updates = %d, creates = %d, want an update and no create", updates, creates)
		}
	})

	t.Run("volume is rejected when the other flags match", func(t *testing.T) {
		args := append(append([]string{}, matching...), testVolumeFlag, testVolumePath)
		updates, creates, err := runDeployAgainstExistingApp(t, body, args...)
		if err == nil || !strings.Contains(err.Error(), testVolumeFlag) {
			t.Fatalf("err = %v, want it to name --volume", err)
		}
		for _, name := range []string{testGPUTypeFlag, testIdleTTLFlag, "--" + envVarFlag, testSecretFlag} {
			if strings.Contains(err.Error(), name) {
				t.Fatalf("err = %v, want only --volume named", err)
			}
		}
		if updates != 0 || creates != 0 {
			t.Fatalf("updates = %d, creates = %d, want no write", updates, creates)
		}
	})
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

	got, err = parseSecretAttaches([]string{testOrgToken, "API_KEY=INFERENCE_KEY"})
	if err != nil || got == nil || len(*got) != 2 {
		t.Fatalf("attaches: got=%v err=%v", got, err)
	}
	if (*got)[0].SecretName != testOrgToken || (*got)[0].EnvVarName != nil {
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
	if err := validateUpdateDeployFlags(cmd, &serverlessapi.App{}); err != nil {
		t.Fatalf("no create flags: %v", err)
	}

	const scaleHint = "apps scale"
	cases := []struct {
		flags   []string
		wantErr string
	}{
		{flags: []string{testGPUTypeFlag, testGPUType}, wantErr: scaleHint},
		{flags: []string{"--max-workers", "2"}, wantErr: scaleHint},
		{flags: []string{"--env-var", "FOO=bar"}, wantErr: "apps env-vars"},
		{flags: []string{testEnvFileFlag, envDotfile}, wantErr: "apps env-vars"},
		{flags: []string{testVolumeFlag, "/data"}, wantErr: "immutable"},
		{flags: []string{testSecretFlag, testOrgToken}, wantErr: "secrets attach"},
		{flags: []string{testFallbackGPUTypeFlag, testFallbackGPU}, wantErr: scaleHint},
		{flags: []string{testMinAvailableWorkersFlag, "1"}, wantErr: scaleHint},
		{flags: []string{testAvailableWorkersPctFlag, "10"}, wantErr: scaleHint},
		{flags: []string{testNameFlag, testDisplayName}, wantErr: "apps rename"},
		{flags: []string{testRequirement, testPipPackage}},
		{flags: []string{testBaseImageFlag, testBaseImage}},
		{flags: []string{testSrcDirFlag, "."}},
		{flags: []string{testWaitFlag}},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.flags, " "), func(t *testing.T) {
			flags := append([]string(nil), tc.flags...)
			for i, flag := range flags {
				if flag == envDotfile {
					path := t.TempDir() + "/env"
					if err := os.WriteFile(path, []byte("FOO=bar\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					flags[i] = path
				}
			}
			cmd := newDeployCmd(nil)
			if err := cmd.ParseFlags(flags); err != nil {
				t.Fatalf("ParseFlags: %v", err)
			}
			err := validateUpdateDeployFlags(cmd, &serverlessapi.App{})
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

func TestValidateUpdateDeployFlagsNamesEveryFlag(t *testing.T) {
	cmd := newDeployCmd(nil)
	if err := cmd.ParseFlags([]string{testGPUTypeFlag, testGPUType, testVolumeFlag, testVolumePath, testSecretFlag, "DEPLOY_TOKEN"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	err := validateUpdateDeployFlags(cmd, &serverlessapi.App{})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, name := range []string{testGPUTypeFlag, testVolumeFlag, testSecretFlag} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("missing %s in %v", name, err)
		}
	}
}

func TestValidateUpdateDeployFlagsAllowsAnUnchangedGPUType(t *testing.T) {
	cmd := newDeployCmd(nil)
	if err := cmd.ParseFlags([]string{testGPUTypeFlag, testGPUType}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	gpu := testGPUType
	app := &serverlessapi.App{Configuration: serverlessapi.WorkerConfig{GpuType: &gpu}}
	if err := validateUpdateDeployFlags(cmd, app); err != nil {
		t.Fatal(err)
	}
}

// TestValidateUpdateDeployFlagsAcceptsAStoredValue: each create-only flag is
// accepted when the app already holds that value, and a different value names
// the flag. The stored numbers, variable and secret differ from a zero App, so
// a comparison that ignores the app or treats a missing key as "" fails.
func TestValidateUpdateDeployFlagsAcceptsAStoredValue(t *testing.T) {
	gpu := testGPUType
	fallback := testFallbackGPU
	minAvailable := int32(2)
	availablePct := int32(15)
	stored := &serverlessapi.App{
		AppName: testDisplayName,
		Configuration: serverlessapi.WorkerConfig{
			GpuType:             &gpu,
			FallbackGpuType:     &fallback,
			MaxWorkers:          3,
			IdleTtlSecs:         61,
			ScalingDelaySecs:    11,
			MinWorkers:          2,
			GpusPerWorker:       2,
			MinAvailableWorkers: &minAvailable,
			AvailableWorkersPct: &availablePct,
		},
		EnvironmentVariables: []serverlessapi.EnvironmentVariable{{Key: "FOO", Value: "bar"}},
		Secrets:              []serverlessapi.SecretAttachment{{Name: testOrgToken}},
	}
	onlyOld := &serverlessapi.App{
		EnvironmentVariables: []serverlessapi.EnvironmentVariable{{Key: "OLD", Value: "x"}},
	}

	cases := []struct {
		name    string
		flags   []string
		envFile string
		app     *serverlessapi.App
		wantErr string
	}{
		{name: "gpu type matches", flags: []string{testGPUTypeFlag, testGPUType}},
		{name: "gpu type differs", flags: []string{testGPUTypeFlag, testFallbackGPU}, wantErr: testGPUTypeFlag},
		{name: "name matches", flags: []string{testNameFlag, testDisplayName}},
		{name: "name differs", flags: []string{testNameFlag, "Other"}, wantErr: testNameFlag},
		{name: "max workers matches", flags: []string{testMaxWorkersFlag, "3"}},
		{name: "max workers differs", flags: []string{testMaxWorkersFlag, "4"}, wantErr: testMaxWorkersFlag},
		{name: "idle ttl matches", flags: []string{testIdleTTLFlag, "61"}},
		{name: "idle ttl differs", flags: []string{testIdleTTLFlag, "62"}, wantErr: testIdleTTLFlag},
		{name: "scaling delay matches", flags: []string{testScalingDelayFlag, "11"}},
		{name: "scaling delay differs", flags: []string{testScalingDelayFlag, "12"}, wantErr: testScalingDelayFlag},
		{name: "min workers matches", flags: []string{testMinWorkersFlag, "2"}},
		{name: "min workers differs", flags: []string{testMinWorkersFlag, "3"}, wantErr: testMinWorkersFlag},
		{name: "gpus per worker matches", flags: []string{testGPUsPerWorkerFlag, "2"}},
		{name: "gpus per worker differs", flags: []string{testGPUsPerWorkerFlag, "4"}, wantErr: testGPUsPerWorkerFlag},
		{name: "fallback gpu matches", flags: []string{testFallbackGPUTypeFlag, testFallbackGPU}},
		{name: "fallback gpu differs", flags: []string{testFallbackGPUTypeFlag, testGPUType}, wantErr: testFallbackGPUTypeFlag},
		{name: "min available workers matches", flags: []string{testMinAvailableWorkersFlag, "2"}},
		{name: "min available workers differs", flags: []string{testMinAvailableWorkersFlag, "3"}, wantErr: testMinAvailableWorkersFlag},
		{name: "available workers pct matches", flags: []string{testAvailableWorkersPctFlag, "15"}},
		{name: "available workers pct differs", flags: []string{testAvailableWorkersPctFlag, "20"}, wantErr: testAvailableWorkersPctFlag},
		{name: "secret name matches", flags: []string{testSecretFlag, testOrgToken}},
		{name: "secret env equal to its name matches", flags: []string{testSecretFlag, "ORG_TOKEN=ORG_TOKEN"}},
		{name: "secret env override differs", flags: []string{testSecretFlag, "ORG_TOKEN=OTHER"}, wantErr: testSecretFlag},
		{name: "env var matches", flags: []string{"--" + envVarFlag, "FOO=bar"}},
		{name: "env var differs", flags: []string{"--" + envVarFlag, "FOO=baz"}, wantErr: "--" + envVarFlag},
		{
			name:    "empty env value is a different key",
			flags:   []string{"--" + envVarFlag, "NEW="},
			app:     onlyOld,
			wantErr: "--" + envVarFlag,
		},
		{name: "env file matches", flags: []string{testEnvFileFlag, envDotfile}, envFile: "FOO=bar\n"},
		{name: "env file differs", flags: []string{testEnvFileFlag, envDotfile}, envFile: "FOO=baz\n", wantErr: testEnvFileFlag},
		{name: "volume is always rejected", flags: []string{testVolumeFlag, testVolumePath}, wantErr: testVolumeFlag},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := tc.app
			if app == nil {
				app = stored
			}
			flags := append([]string(nil), tc.flags...)
			if tc.envFile != "" {
				path := t.TempDir() + "/env"
				if err := os.WriteFile(path, []byte(tc.envFile), 0o600); err != nil {
					t.Fatal(err)
				}
				for i, flag := range flags {
					if flag == envDotfile {
						flags[i] = path
					}
				}
			}
			cmd := newDeployCmd(nil)
			if err := cmd.ParseFlags(flags); err != nil {
				t.Fatalf("ParseFlags: %v", err)
			}
			err := validateUpdateDeployFlags(cmd, app)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %v, want it to name %s", err, tc.wantErr)
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
			flags:   []string{testFromFlag, testArchiveURL, testBaseImageFlag, testBaseImage},
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
// GPU type, a volume and a secret, so a create has to carry all three.
const deployFromArchiveManifest = `configVersion: 1
id: ` + testAppID + `
source:
  type: code
  entry: ` + testModelFile + `
compute:
  gpuType: ` + testGPUType + `
volumes:
  - /weights
secrets:
  - name: ` + testManifestSecret + `
`

// testManifestSecret is the secret deployFromArchiveManifest names.
const testManifestSecret = "HF_TOKEN"

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
	if created.Secrets == nil || len(*created.Secrets) != 1 || (*created.Secrets)[0].SecretName != testManifestSecret {
		t.Fatalf("secrets = %#v", created.Secrets)
	}
}

// TestDeployFrom_RefusesAnExistingApp fixes the create-only rule: a manifest
// names an id, so deploying one over an application that already has that id
// would roll this archive out as that application's next version, running it
// with the secrets, environment variables and volumes already attached.
func TestDeployFrom_RefusesAnExistingApp(t *testing.T) {
	from := serveDeployFromArchive(t)

	// No upload stub: the refusal has to come before the source is uploaded, so
	// an upload POST counts as a write here rather than being served.
	var writes int

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/apps/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(activeAppBody("")))
		case r.Method == http.MethodPatch || r.Method == http.MethodPost:
			writes++
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
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected the deploy to be refused")
	}
	for _, want := range []string{testAppID, "only creates", "--id"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if writes != 0 {
		t.Fatalf("writes = %d, want 0", writes)
	}
}

// deployFromContainerManifest is a packaged container source. Its archive
// carries the container directory a container deploy expects.
const deployFromContainerManifest = `configVersion: 1
id: ` + testAppID + `
source:
  type: container
compute:
  gpuType: ` + testGPUType + `
`

// TestDeployFrom_RefusesCodeOnlyFlagsOnAContainerArchive drives the check
// through the command. The source type is inside the archive, so a code-only
// flag can only be refused once the manifest has been read, and the packer
// would otherwise drop it without a word.
func TestDeployFrom_RefusesCodeOnlyFlagsOnAContainerArchive(t *testing.T) {
	from := serveArchive(t, buildTestArchive(t, []archiveEntry{
		{name: manifestFile, body: deployFromContainerManifest},
		{name: containerDockerfile, body: testDockerfile},
		{name: containerConfig, body: testContainer},
	}))

	var calls int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer api.Close()

	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", api.URL)

	cmd := newDeployCmd(log.New(io.Discard))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{testFromFlag, from, testBaseImageFlag, testBaseImage})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected the deploy to be refused")
	}
	for _, want := range []string{testBaseImageFlag, "container source"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if calls != 0 {
		t.Fatalf("the API was called %d times, want 0", calls)
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

// appIDValidationDetail is the getApp 422 for an appId outside the AppId
// schema: one errors entry naming the path parameter, and no pointer, because
// a parameter is not a field of the body.
const appIDValidationDetail = `parameter "appId" in path: minimum string length is 6`

// TestDeployStopsWhenTheAppLookupFails: getApp's 422 ValidationError stops
// the deploy before an upload or a create. The CLI does not check id length
// itself, so the stub is the refusal getApp returns for a bad appId.
func TestDeployStopsWhenTheAppLookupFails(t *testing.T) {
	withoutDefaultEnvironment(t)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{testModelFile: testPySource})
	const shortAppID = "sdxl"
	problem, err := json.Marshal(map[string]any{
		"type":   "https://docs.runware.ai/serverless/errors#unprocessable-entity",
		"title":  "Unprocessable Entity",
		"status": http.StatusUnprocessableEntity,
		"detail": appIDValidationDetail,
		"errors": []map[string]string{{"detail": appIDValidationDetail}},
	})
	if err != nil {
		t.Fatal(err)
	}

	run := runServerlessAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write(problem)
	}, testDeployCommand, testModelFile, testSrcDirFlag, dir, testIDFlag, shortAppID, testGPUTypeFlag, testGPUType)

	requireDeployRefused(t, run, appIDValidationDetail, testAppsPath+"/"+shortAppID)
}

// TestDeployRefusesCreateWithoutGPUType: a 404 is a create, and a create
// without --gpu-type is refused before the upload.
func TestDeployRefusesCreateWithoutGPUType(t *testing.T) {
	withoutDefaultEnvironment(t)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{testModelFile: testPySource})

	run := runServerless(t, testDeployCommand, testModelFile, testSrcDirFlag, dir, testIDFlag, testAppID)

	requireDeployRefused(t, run, "--gpu-type is required", testAppsPath+"/"+testAppID)
}

// TestDeployRefusesADeletingApp: delete is asynchronous, so the copy still
// exists. Deploying it, with or without a create-only flag, has to stop
// before the upload and tell the caller to wait until the copy is deleted.
func TestDeployRefusesADeletingApp(t *testing.T) {
	withoutDefaultEnvironment(t)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{testModelFile: testPySource})

	deleting := `"status":"` + string(serverlessapi.AppStatusDeleting) + `"`
	body := strings.Replace(activeAppBody(""), `"status":"`+string(serverlessapi.AppStatusActive)+`"`, deleting, 1)
	if !strings.Contains(body, deleting) {
		t.Fatalf("app fixture has no status to mark deleting: %s", body)
	}
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
	want := `application "` + testAppID + `" is deleting; wait until it is deleted, then deploy`

	t.Run("with --gpu-type", func(t *testing.T) {
		run := runServerlessAgainst(t, handler, testDeployCommand, testModelFile, testSrcDirFlag, dir, testIDFlag, testAppID, testGPUTypeFlag, testGPUType)
		requireDeployRefused(t, run, want, testAppsPath+"/"+testAppID)
	})
	t.Run("without --gpu-type", func(t *testing.T) {
		run := runServerlessAgainst(t, handler, testDeployCommand, testModelFile, testSrcDirFlag, dir, testIDFlag, testAppID)
		requireDeployRefused(t, run, want, testAppsPath+"/"+testAppID)
	})
}
