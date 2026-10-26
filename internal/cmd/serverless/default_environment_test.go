package serverless

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/config"
)

// TestMain keeps a RUNWARE_ENV in the developer's shell from moving every
// command the tests run to another environment.
func TestMain(m *testing.M) {
	_ = os.Unsetenv(config.EnvironmentFromEnvVar)
	os.Exit(m.Run())
}

const testDevelopmentEnvironment = "development"

// withDefaultEnvironment gives the test a config file whose default
// environment is name, and no RUNWARE_ENV.
func withDefaultEnvironment(t *testing.T, name string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.EnvironmentFromEnvVar, "")
	if err := config.Init(); err != nil {
		t.Fatal(err)
	}
	err := config.Save(&config.Config{
		Defaults: config.Defaults{
			Environment: name,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

// reportsOrganizationCases are the commands whose --env only narrows a report
// of the whole organization.
var reportsOrganizationCases = map[string]struct{}{
	"usage":        {},
	"usage events": {},
}

func TestCommandsWithoutTheFlagAddressTheDefaultEnvironment(t *testing.T) {
	for _, tc := range appCommandCases() {
		t.Run(tc.name, func(t *testing.T) {
			withDefaultEnvironment(t, testEnvironment)
			run := runServerless(t, tc.args...)

			if len(run.requests) == 0 {
				t.Fatalf("no request reached the API: %v", run.err)
			}
			_, report := reportsOrganizationCases[tc.name]
			for _, r := range run.requests {
				switch {
				case report && r.query.Has("environment"):
					t.Errorf("%s: an organization report sent environment %q", r, r.query.Get("environment"))
				case !report && r.query.Get("environment") != testEnvironment:
					t.Errorf("%s: environment = %q, want the default %q", r, r.query.Get("environment"), testEnvironment)
				}
			}
		})
	}
}

func TestEnvironmentPrecedence(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		envVar     string
		args       []string
		want       string
	}{
		{
			name:       "config",
			configured: testEnvironment,
			want:       testEnvironment,
		},
		{
			name:       "RUNWARE_ENV over config",
			configured: testEnvironment,
			envVar:     testDevelopmentEnvironment,
			want:       testDevelopmentEnvironment,
		},
		{
			name:       "--env over both",
			configured: testEnvironment,
			envVar:     testDevelopmentEnvironment,
			args:       []string{testEnvFlag, testProductionEnvironment},
			want:       testProductionEnvironment,
		},
		{
			name: "nothing set sends none",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDefaultEnvironment(t, tc.configured)
			t.Setenv(config.EnvironmentFromEnvVar, tc.envVar)
			run := runServerless(t, append([]string{testAppsGroup, appHealthShowCommand, testAppID}, tc.args...)...)

			if len(run.requests) != 1 {
				t.Fatalf("requests = %v, want one", run.requests)
			}
			if got := run.requests[0].query.Get("environment"); got != tc.want {
				t.Fatalf("environment = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDefaultEnvironmentIsAnnounced(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		envVar     string
		args       []string
		want       string
	}{
		{
			name:       "from config",
			configured: testEnvironment,
			want:       "Using environment staging from config",
		},
		{
			name:   "from RUNWARE_ENV",
			envVar: testEnvironment,
			want:   "Using environment staging from RUNWARE_ENV",
		},
		{
			name:       "named by --env",
			configured: testEnvironment,
			args:       []string{testEnvFlag, testEnvironment},
		},
		{
			name:       "production",
			configured: testProductionEnvironment,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDefaultEnvironment(t, tc.configured)
			t.Setenv(config.EnvironmentFromEnvVar, tc.envVar)
			run := runServerless(t, append([]string{testAppsGroup, "stop", testAppID}, tc.args...)...)

			got := strings.Contains(run.logs, "Using environment")
			if tc.want == "" && got {
				t.Fatalf("logs = %q, want no notice", run.logs)
			}
			if tc.want != "" && !strings.Contains(run.logs, tc.want) {
				t.Fatalf("logs = %q, want %q", run.logs, tc.want)
			}
		})
	}
}

func TestPrintedCommandsNameTheDefaultEnvironment(t *testing.T) {
	withDefaultEnvironment(t, testEnvironment)
	run := runServerlessAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"nextCursor":"`+testNextCursor+`"}`)
	}, testAppsGroup, appHealthListCommand)

	if run.err != nil {
		t.Fatal(run.err)
	}
	if !strings.Contains(run.cobraOutput, testStagingHint) {
		t.Fatalf("next-page hint %q, want %q so it runs the same anywhere", run.cobraOutput, testStagingHint)
	}
}

func TestDeletePromptNamesTheDefaultEnvironment(t *testing.T) {
	withDefaultEnvironment(t, testEnvironment)
	run := runServerlessAnswering(t, "n\n", testAppsGroup, testDeleteCommand, testAppID)

	if !errors.Is(run.err, errDeleteCancelled) {
		t.Fatalf("err = %v, want the delete cancelled", run.err)
	}
	want := "Delete application " + testAppID + " in " + testEnvironment + "? [y/N]"
	if !strings.Contains(run.cobraOutput, want) {
		t.Fatalf("prompt = %q, want %q", run.cobraOutput, want)
	}
}

func TestDeployChecksTheDefaultEnvironmentBeforeTheUpload(t *testing.T) {
	withDefaultEnvironment(t, testEnvironment)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{testModelFile: testPySource})
	run := runServerless(t, testDeployCommand, testModelFile, testSrcDirFlag, dir, testIDFlag, testAppID, testGPUTypeFlag, testGPUType)

	want := `environment "` + testEnvironment + `" not found`
	if run.err == nil || !strings.Contains(run.err.Error(), want) {
		t.Fatalf("err = %v, want %q", run.err, want)
	}
	wantPath := testEnvironmentsPath + "/" + testEnvironment
	if len(run.requests) != 2 || run.requests[1].path != wantPath {
		t.Fatalf("requests = %v, want the app lookup then %s, and no upload", run.requests, wantPath)
	}
}
