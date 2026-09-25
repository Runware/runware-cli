package serverless

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

func TestValidateAppName(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
	}{
		{name: "Image generator"},
		{name: "A"},
		{name: "", wantErr: true},
		{name: " padded", wantErr: true},
		{name: "padded ", wantErr: true},
		{name: "   ", wantErr: true},
	}
	for _, tc := range cases {
		err := validateAppName(tc.name)
		if tc.wantErr && err == nil {
			t.Errorf("%q: expected error", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%q: %v", tc.name, err)
		}
	}
}

func TestAppsRename_PatchesNameOnly(t *testing.T) {
	const wantName = "Image generator"
	var patches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/apps/"+testAppID {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		patches++
		var body serverlessapi.AppUpdate
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		if body.AppName == nil || *body.AppName != wantName {
			t.Errorf("appName = %v", body.AppName)
		}
		if body.AppSource != nil || body.Configuration != nil || body.Secrets != nil || body.EnvironmentVariables != nil {
			t.Errorf("patch included out-of-scope fields")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Replace(activeAppBody(""), `"appName":"My App"`, `"appName":"`+wantName+`"`, 1)))
	}))
	defer srv.Close()

	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)

	cmd := newAppsRenameCmd(log.New(io.Discard))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{testAppID, wantName})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if patches != 1 {
		t.Fatalf("patches = %d, want 1", patches)
	}
}

func TestAppsRename_RejectsPaddedName(t *testing.T) {
	cmd := newAppsRenameCmd(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{testAppID, " padded"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "non-whitespace") {
		t.Fatalf("err = %v", err)
	}
}
