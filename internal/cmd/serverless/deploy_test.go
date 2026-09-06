package serverless

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

const (
	testContainerFlag = "--container"
	testWrapperDir    = "./wrapper"
	testPipPackage    = "torch"
	testSourceID      = "019c7654-8b21-7abc-9123-abcdef123456"
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
			flags:   []string{testContainerFlag, testWrapperDir, "--src-dir", "."},
			wantErr: "--src-dir",
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
			flags: []string{"--src-dir", "."},
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
