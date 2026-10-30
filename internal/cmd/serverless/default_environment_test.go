package serverless

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
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
// of the whole organization. usage events is one only without --app.
var reportsOrganizationCases = map[string]struct{}{
	"usage":                        {},
	usageEventsWithoutAppCase.name: {},
}

// usageEventsWithoutAppCase reports every environment's events. It is no app
// command case, because it refuses --env.
var usageEventsWithoutAppCase = appCommandCase{
	name: "usage events without --app",
	args: []string{testUsageCommand, testEventsCommand},
}

func TestCommandsWithoutTheFlagAddressTheDefaultEnvironment(t *testing.T) {
	for _, tc := range appCommandCases() {
		if _, report := reportsOrganizationCases[tc.name]; report {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			withDefaultEnvironment(t, testEnvironment)
			run := runServerless(t, tc.args...)

			if len(run.requests) == 0 {
				t.Fatalf("no request reached the API: %v", run.err)
			}
			for _, r := range run.requests {
				if r.query.Get("environment") != testEnvironment {
					t.Errorf("%s: environment = %q, want the default %q", r, r.query.Get("environment"), testEnvironment)
				}
			}
		})
	}
}

func TestOrganizationReportsIgnoreTheDefaultEnvironment(t *testing.T) {
	for _, tc := range append(appCommandCases(), usageEventsWithoutAppCase) {
		if _, report := reportsOrganizationCases[tc.name]; !report {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			withDefaultEnvironment(t, testEnvironment)
			run := runServerless(t, tc.args...)

			if len(run.requests) == 0 {
				t.Fatalf("no request reached the API: %v", run.err)
			}
			for _, r := range run.requests {
				if r.query.Has("environment") {
					t.Errorf("%s: an organization report sent environment %q", r, r.query.Get("environment"))
				}
			}
		})
	}
}

// environmentDefaultCase sets the default environment one way or another and
// says what a command without --env should do.
type environmentDefaultCase struct {
	name       string
	configured string
	envVar     string
	args       []string
	want       string
}

func TestEnvironmentPrecedence(t *testing.T) {
	cases := []environmentDefaultCase{
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
			name:       "RUNWARE_ENV=production over a config default",
			configured: testEnvironment,
			envVar:     testProductionEnvironment,
		},
		{
			name:       "a production default sends none",
			configured: testProductionEnvironment,
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
			if got := run.requests[0].query.Get("environment"); got != tc.want || run.requests[0].query.Has("environment") != (tc.want != "") {
				t.Fatalf("environment = %q (sent %v), want %q", got, run.requests[0].query.Has("environment"), tc.want)
			}
		})
	}
}

func TestCommandsWithoutAnEnvironmentIgnoreTheDefault(t *testing.T) {
	withDefaultEnvironment(t, testEnvironment)
	run := runServerless(t, testGPUsCommand)

	if len(run.requests) == 0 {
		t.Fatalf("no request reached the API: %v", run.err)
	}
	for _, r := range run.requests {
		if r.query.Has("environment") {
			t.Errorf("%s: sent environment %q", r, r.query.Get("environment"))
		}
	}
}

// announce runs announceEnvironment for the serverless command args name,
// with its flags parsed, and returns what it logged.
func announce(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd, rest, err := NewCmd(log.New(io.Discard)).Find(args)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.ParseFlags(rest); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	err = announceEnvironment(cmd, log.New(&logs))
	return logs.String(), err
}

func TestDefaultEnvironmentIsAnnounced(t *testing.T) {
	cases := []environmentDefaultCase{
		{
			name:       "from config",
			configured: testEnvironment,
			want:       "Using environment environment=staging source=config",
		},
		{
			name:   "from RUNWARE_ENV",
			envVar: testEnvironment,
			want:   "Using environment environment=staging source=RUNWARE_ENV",
		},
		{
			name:       "named by --env",
			configured: testEnvironment,
			args:       []string{testEnvFlag, testEnvironment},
		},
		{
			name:       testProductionEnvironment,
			configured: testProductionEnvironment,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDefaultEnvironment(t, tc.configured)
			t.Setenv(config.EnvironmentFromEnvVar, tc.envVar)
			logs, err := announce(t, append([]string{testAppsGroup, "stop", testAppID}, tc.args...)...)

			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" && strings.Contains(logs, "Using environment") {
				t.Fatalf("logs = %q, want no notice", logs)
			}
			if tc.want != "" && !strings.Contains(logs, tc.want) {
				t.Fatalf("logs = %q, want %q", logs, tc.want)
			}
		})
	}
}

func TestOneAppsUsageEventsAnnounceTheDefaultEnvironment(t *testing.T) {
	withDefaultEnvironment(t, testEnvironment)
	logs, err := announce(t, testUsageCommand, testEventsCommand, testAppFlag, testAppID)

	if err != nil {
		t.Fatal(err)
	}
	if want := "Using environment environment=staging source=config"; !strings.Contains(logs, want) {
		t.Fatalf("logs = %q, want %q", logs, want)
	}
}

func TestAnInvalidDefaultEnvironmentNamesItsSource(t *testing.T) {
	cases := []environmentDefaultCase{
		{
			name:   "RUNWARE_ENV",
			envVar: testInvalidEnvironment,
			want:   `environment "Staging" from RUNWARE_ENV`,
		},
		{
			name:       "hand-edited config",
			configured: testInvalidEnvironment,
			want:       `environment "Staging" from config`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDefaultEnvironment(t, tc.configured)
			t.Setenv(config.EnvironmentFromEnvVar, tc.envVar)
			_, err := announce(t, testAppsGroup, appHealthListCommand)

			if !errors.Is(err, serverlessapi.ErrEnvironmentName) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q and the name rule", err, tc.want)
			}
		})
	}
}

// ignoredDefaultCase is a command that addresses no default environment,
// run under an invalid one.
type ignoredDefaultCase struct {
	name       string
	configured string
	envVar     string
	args       []string
}

func TestCommandsThatIgnoreTheDefaultNeitherCheckNorAnnounceIt(t *testing.T) {
	cases := []ignoredDefaultCase{
		{
			name:   "gpus under RUNWARE_ENV",
			envVar: testInvalidEnvironment,
			args:   []string{testGPUsCommand},
		},
		{
			name:       "gpus under the config",
			configured: testInvalidEnvironment,
			args:       []string{testGPUsCommand},
		},
		{
			name:   "an organization report under RUNWARE_ENV",
			envVar: testInvalidEnvironment,
			args:   []string{testUsageCommand},
		},
		{
			name:       "an organization report under the config",
			configured: testInvalidEnvironment,
			args:       []string{testUsageCommand},
		},
		{
			name:       "the organization's usage events under the config",
			configured: testInvalidEnvironment,
			args:       []string{testUsageCommand, testEventsCommand},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDefaultEnvironment(t, tc.configured)
			t.Setenv(config.EnvironmentFromEnvVar, tc.envVar)

			if logs, err := announce(t, tc.args...); err != nil || logs != "" {
				t.Fatalf("announce = %q, %v, want the default ignored", logs, err)
			}
		})
	}
}

// printedHintCase is a command run under a default environment and the
// --env its next-page hint must carry, or none.
type printedHintCase struct {
	name     string
	args     []string
	wantHint string
}

func TestPrintedCommandsNameTheEnvironmentUnderADefault(t *testing.T) {
	cases := []printedHintCase{
		{
			name:     "the default",
			args:     []string{testAppsGroup, appHealthListCommand},
			wantHint: testStagingHint,
		},
		{
			name:     "production named while a default is set",
			args:     []string{testAppsGroup, appHealthListCommand, testEnvFlag, testProductionEnvironment},
			wantHint: testEnvFlag + " " + testProductionEnvironment,
		},
		{
			name: "an organization report",
			args: []string{testUsageCommand, testEventsCommand},
		},
		{
			name:     "one app's usage events",
			args:     []string{testUsageCommand, testEventsCommand, testAppFlag, testAppID},
			wantHint: testStagingHint + " --app " + testAppID,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDefaultEnvironment(t, testEnvironment)
			run := runServerlessAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":[],"nextCursor":"`+testNextCursor+`"}`)
			}, tc.args...)

			if run.err != nil {
				t.Fatal(run.err)
			}
			if !strings.Contains(run.cobraOutput, "--cursor") {
				t.Fatalf("no next-page hint in %q", run.cobraOutput)
			}
			if got := strings.Contains(run.cobraOutput, testEnvFlag); tc.wantHint == "" && got {
				t.Fatalf("next-page hint %q names an environment", run.cobraOutput)
			}
			if tc.wantHint != "" && !strings.Contains(run.cobraOutput, tc.wantHint) {
				t.Fatalf("next-page hint %q, want %q so it runs the same here and elsewhere", run.cobraOutput, tc.wantHint)
			}
		})
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

func TestASecondExecuteResolvesTheEnvironmentAgain(t *testing.T) {
	rec := &requestRecorder{}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)
	cmd := NewCmd(log.New(io.Discard))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{testAppsGroup, testDeleteCommand, testAppID, testYesFlag})

	t.Setenv(config.EnvironmentFromEnvVar, testEnvironment)
	_ = cmd.Execute()
	t.Setenv(config.EnvironmentFromEnvVar, testDevelopmentEnvironment)
	_ = cmd.Execute()

	if len(rec.requests) != 2 {
		t.Fatalf("requests = %v, want one per Execute", rec.requests)
	}
	if got := rec.requests[1].query.Get("environment"); got != testDevelopmentEnvironment {
		t.Fatalf("second Execute addressed %q, want %q", got, testDevelopmentEnvironment)
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

func TestCreateOnlyDeployHintNamesProductionUnderADefault(t *testing.T) {
	withDefaultEnvironment(t, testEnvironment)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{testModelFile: testPySource})
	run := runServerlessAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, activeAppBody(""))
	}, testDeployCommand, testModelFile, testSrcDirFlag, dir, testIDFlag, testAppID, testGPUTypeFlag, testGPUType, testEnvFlag, testProductionEnvironment)

	if want := "apps scale --env production"; run.err == nil || !strings.Contains(run.err.Error(), want) {
		t.Fatalf("err = %v, want %q", run.err, want)
	}
}

func TestOpenLinksTheConsoleToTheDefaultEnvironment(t *testing.T) {
	withDefaultEnvironment(t, testEnvironment)
	t.Setenv("PATH", "")
	t.Setenv("BROWSER", "")

	run := runServerless(t, "open", testAppID)

	if run.err != nil {
		t.Fatalf("open: %v", run.err)
	}
	if !strings.Contains(run.stdout, "environment="+testEnvironment) {
		t.Fatalf("stdout = %q, want the link to carry environment=%s", run.stdout, testEnvironment)
	}
}

// configSwitchingInput moves the config's default environment to another one
// when a prompt reads its answer, as `config set environment` in another shell
// could.
type configSwitchingInput struct {
	t  *testing.T
	to string
	io.Reader
}

func (r *configSwitchingInput) Read(p []byte) (int, error) {
	if r.to != "" {
		if err := config.Save(&config.Config{Defaults: config.Defaults{Environment: r.to}}); err != nil {
			r.t.Fatal(err)
		}
		r.to = ""
	}
	return r.Reader.Read(p)
}

func TestDeleteAddressesTheEnvironmentItsPromptNamed(t *testing.T) {
	withDefaultEnvironment(t, testEnvironment)
	stdin := terminalInput{&configSwitchingInput{t: t, to: testDevelopmentEnvironment, Reader: strings.NewReader("y\n")}}

	run := runServerlessWith(t, nil, stdin, testAppsGroup, testDeleteCommand, testAppID)

	if !strings.Contains(run.cobraOutput, " in "+testEnvironment+"?") {
		t.Fatalf("prompt = %q, want it to name %q", run.cobraOutput, testEnvironment)
	}
	if len(run.requests) == 0 {
		t.Fatalf("no request reached the API: %v", run.err)
	}
	for _, r := range run.requests {
		if got := r.query.Get("environment"); got != testEnvironment {
			t.Errorf("%s: environment = %q, want %q, the one the prompt named", r, got, testEnvironment)
		}
	}
}
