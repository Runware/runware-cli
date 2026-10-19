package serverless

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

const (
	testEnvironment           = "staging"
	testProductionEnvironment = "production"
	testUUID                  = "11111111-1111-1111-1111-111111111111"
	testAppsGroup             = "apps"
	testVersionsGroup         = "versions"
	testYesFlag               = "--yes"
	testEnvFlag               = "--env"
	testAppsPath              = "/v1/apps"
	testSourceUploadsPath     = "/v1/source-uploads"
	testEnvironmentsPath      = "/v1/environments"
	testNextCursor            = "c1"
	testValueFlag             = "--value"
	testBuildsGroup           = "builds"
	testEventsCommand         = "events"
	testUsageCommand          = "usage"
	testSecretsGroup          = "secrets"
	testAttachedSecret        = "TOKEN"
	testInitCommand           = "init"
	testDeployCommand         = "deploy"
	testSetCommand            = "set"
	testStagingHint           = testEnvFlag + " " + testEnvironment
	testEnvironmentBody       = `{"name":"` + testEnvironment + `","createdAt":"2026-09-01T00:00:00Z","budgetAlertPercents":[]}`
)

// recordedRequest is one request the stub API received.
type recordedRequest struct {
	method string
	path   string
	query  url.Values
}

func (r recordedRequest) String() string {
	return r.method + " " + r.path
}

// requestRecorder keeps every request and answers it with handler, or with a
// 404 problem so each command sends its first app request and stops there.
type requestRecorder struct {
	mu       sync.Mutex
	requests []recordedRequest
	handler  http.HandlerFunc
}

func (r *requestRecorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.requests = append(r.requests, recordedRequest{
		method: req.Method,
		path:   req.URL.Path,
		query:  req.URL.Query(),
	})
	r.mu.Unlock()
	if r.handler != nil {
		r.handler(w, req)
		return
	}
	writeNotFound(w)
}

func writeNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, `{"title":"Not Found","status":404}`)
}

// serverlessRun is what one `serverless` command did against the stub API.
type serverlessRun struct {
	requests []recordedRequest
	// cobraOutput is what Cobra itself wrote: errors, deprecation notices and
	// completions. Command data goes to os.Stdout and lands in stdout.
	cobraOutput string
	stdout      string
	err         error
}

// runServerlessAgainst runs `serverless <args>` against a stub API that
// answers with handler, or with 404 when handler is nil.
func runServerlessAgainst(t *testing.T, handler http.HandlerFunc, args ...string) serverlessRun {
	t.Helper()
	rec := &requestRecorder{
		handler: handler,
	}
	srv := httptest.NewServer(rec)
	defer srv.Close()
	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)

	cmd := NewCmd(log.New(io.Discard))
	var cobraOutput bytes.Buffer
	cmd.SetOut(&cobraOutput)
	cmd.SetErr(&cobraOutput)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)

	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = stdout
	defer func() { os.Stdout = old }()
	execErr := cmd.Execute()
	_ = stdout.Close()
	out, err := os.ReadFile(stdout.Name())
	if err != nil {
		t.Fatal(err)
	}
	return serverlessRun{
		requests:    rec.requests,
		cobraOutput: cobraOutput.String(),
		stdout:      string(out),
		err:         execErr,
	}
}

func runServerless(t *testing.T, args ...string) serverlessRun {
	t.Helper()
	return runServerlessAgainst(t, nil, args...)
}

// newSourceUploadStub returns a handler for the source-upload create and
// complete requests of a deploy, staging the archive on its own server. The
// handler reports whether it answered the request.
func newSourceUploadStub(t *testing.T) func(http.ResponseWriter, *http.Request) bool {
	t.Helper()
	stage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(stage.Close)
	return func(w http.ResponseWriter, r *http.Request) bool {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == testSourceUploadsPath:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{
				"upload": {"id":"`+testSourceID+`","declaredByteLength":1,"sha256":"00","sourceType":"code","state":"pending","expiresAt":"2026-09-02T12:00:00Z","createdAt":"2026-09-02T11:00:00Z","updatedAt":"2026-09-02T11:00:00Z"},
				"transfer": {"mode":"singlePut","method":"PUT","url":"`+stage.URL+`/obj","headers":{"Content-Type":"application/zip"},"expiresAt":"2026-09-02T12:00:00Z"}
			}`)
			return true
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, testSourceUploadsPath+"/") && strings.HasSuffix(r.URL.Path, "/complete"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"`+testSourceID+`","declaredByteLength":1,"sha256":"00","sourceType":"code","sourceId":"`+testSourceID+`","state":"ready","expiresAt":"2026-09-02T12:00:00Z","createdAt":"2026-09-02T11:00:00Z","updatedAt":"2026-09-02T11:00:00Z"}`)
			return true
		default:
			return false
		}
	}
}

type appCommandCase struct {
	name string
	args []string
}

func appCommandCases() []appCommandCase {
	return []appCommandCase{
		{
			name: "apps list",
			args: []string{testAppsGroup, appHealthListCommand},
		},
		{
			name: "apps show",
			args: []string{testAppsGroup, appHealthShowCommand, testAppID},
		},
		{
			name: "apps endpoints",
			args: []string{testAppsGroup, "endpoints", testAppID},
		},
		{
			name: "apps endpoints show",
			args: []string{testAppsGroup, "endpoints", appHealthShowCommand, testAppID, testUUID},
		},
		{
			name: "apps invoke",
			args: []string{testAppsGroup, "invoke", testAppID, "infer"},
		},
		{
			name: "apps tasks",
			args: []string{testAppsGroup, "tasks", testAppID},
		},
		{
			name: "apps tasks show",
			args: []string{testAppsGroup, "tasks", appHealthShowCommand, testAppID, testUUID},
		},
		{
			name: "apps env-vars list",
			args: []string{testAppsGroup, envVarsGroup, appHealthListCommand, testAppID},
		},
		{
			name: "apps env-vars set one",
			args: []string{testAppsGroup, envVarsGroup, testSetCommand, testAppID, testEnvKey, testValueFlag, "v"},
		},
		{
			name: "apps env-vars set many",
			args: []string{testAppsGroup, envVarsGroup, testSetCommand, testAppID, "--" + envVarFlag, "A=1"},
		},
		{
			name: "apps env-vars unset",
			args: []string{testAppsGroup, envVarsGroup, "unset", testAppID, testEnvKey},
		},
		{
			name: "apps env list",
			args: []string{testAppsGroup, deprecatedEnvVarsGroup, appHealthListCommand, testAppID},
		},
		{
			name: "apps versions list",
			args: []string{testAppsGroup, testVersionsGroup, appHealthListCommand, testAppID},
		},
		{
			name: "apps versions show",
			args: []string{testAppsGroup, testVersionsGroup, appHealthShowCommand, testAppID, "1"},
		},
		{
			name: "apps versions activate",
			args: []string{testAppsGroup, testVersionsGroup, "activate", testAppID, "1"},
		},
		{
			name: "apps versions delete",
			args: []string{testAppsGroup, testVersionsGroup, "delete", testAppID, "1", testYesFlag},
		},
		{
			name: "apps builds list",
			args: []string{testAppsGroup, testBuildsGroup, appHealthListCommand, testAppID},
		},
		{
			name: "apps builds show",
			args: []string{testAppsGroup, testBuildsGroup, appHealthShowCommand, testAppID, testUUID},
		},
		{
			name: "apps builds delete",
			args: []string{testAppsGroup, testBuildsGroup, "delete", testAppID, testUUID, testYesFlag},
		},
		{
			name: "apps logs",
			args: []string{testAppsGroup, "logs", testAppID},
		},
		{
			name: "apps events",
			args: []string{testAppsGroup, testEventsCommand, testAppID},
		},
		{
			name: "apps errors",
			args: []string{testAppsGroup, "errors", testAppID},
		},
		{
			name: "apps workers",
			args: []string{testAppsGroup, "workers", testAppID},
		},
		{
			name: "apps workers show",
			args: []string{testAppsGroup, "workers", appHealthShowCommand, testAppID, testUUID},
		},
		{
			name: "apps scale",
			args: []string{testAppsGroup, "scale", testAppID, "--max-workers", "2"},
		},
		{
			name: "apps rename",
			args: []string{testAppsGroup, "rename", testAppID, "New name"},
		},
		{
			name: "apps usage",
			args: []string{testAppsGroup, testUsageCommand, testAppID},
		},
		{
			name: "apps stop",
			args: []string{testAppsGroup, "stop", testAppID},
		},
		{
			name: "apps resume",
			args: []string{testAppsGroup, "resume", testAppID},
		},
		{
			name: "apps delete",
			args: []string{testAppsGroup, "delete", testAppID, testYesFlag},
		},
		{
			name: "usage events",
			args: []string{testUsageCommand, testEventsCommand, "--app", testAppID},
		},
		{
			name: "secrets attachments",
			args: []string{testSecretsGroup, "attachments", testAppID},
		},
		{
			name: "secrets attach",
			args: []string{testSecretsGroup, "attach", testAppID, testAttachedSecret},
		},
		{
			name: "secrets detach",
			args: []string{testSecretsGroup, "detach", testAppID, testAttachedSecret},
		},
	}
}

func TestEveryAppCommandAddressesTheChosenEnvironment(t *testing.T) {
	for _, tc := range appCommandCases() {
		t.Run(tc.name, func(t *testing.T) {
			run := runServerless(t, append(tc.args, "-e", testEnvironment)...)

			if len(run.requests) == 0 {
				t.Fatalf("no request reached the API: %v", run.err)
			}
			for _, r := range run.requests {
				if got := r.query.Get("environment"); got != testEnvironment {
					t.Errorf("%s: environment = %q, want %q", r, got, testEnvironment)
				}
			}
		})
	}
}

func TestAppCommandWithoutTheFlagSendsNoEnvironment(t *testing.T) {
	for _, tc := range appCommandCases() {
		t.Run(tc.name, func(t *testing.T) {
			run := runServerless(t, tc.args...)

			if len(run.requests) == 0 {
				t.Fatalf("no request reached the API: %v", run.err)
			}
			for _, r := range run.requests {
				if r.query.Has("environment") {
					t.Errorf("%s: sent environment %q", r, r.query.Get("environment"))
				}
			}
		})
	}
}

func TestEnvironmentFlagOnTheAppsGroupReachesTheSubcommand(t *testing.T) {
	run := runServerless(t, testAppsGroup, testEnvFlag, testEnvironment, appHealthShowCommand, testAppID)

	if len(run.requests) != 1 || run.requests[0].query.Get("environment") != testEnvironment {
		t.Fatalf("requests = %+v, want one request in %s", run.requests, testEnvironment)
	}
}

func TestCommandsWithoutAnEnvironmentRejectTheFlag(t *testing.T) {
	cases := []appCommandCase{
		{
			name: "usage",
			args: []string{testUsageCommand},
		},
		{
			name: "gpus",
			args: []string{"gpus"},
		},
		{
			name: "init",
			args: []string{testInitCommand, t.TempDir()},
		},
		{
			name: "secrets list",
			args: []string{testSecretsGroup, appHealthListCommand},
		},
		{
			name: "secrets set",
			args: []string{testSecretsGroup, testSetCommand, testAttachedSecret, testValueFlag, "v"},
		},
		{
			name: "secrets remove",
			args: []string{testSecretsGroup, "remove", testAttachedSecret},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := runServerless(t, append(tc.args, testEnvFlag, testEnvironment)...)

			if run.err == nil || !strings.Contains(run.err.Error(), "unknown flag: "+testEnvFlag) {
				t.Fatalf("err = %v, want unknown flag %s", run.err, testEnvFlag)
			}
			if len(run.requests) != 0 {
				t.Fatalf("sent %d requests", len(run.requests))
			}
		})
	}
}

type environmentNameCase struct {
	name    string
	value   string
	wantErr string
}

func TestEnvironmentFlagRejectsBeforeAnyRequest(t *testing.T) {
	cases := []environmentNameCase{
		{
			name:    "variable assignment",
			value:   "HF_TOKEN=x",
			wantErr: "use --env-var KEY=VALUE",
		},
		{
			name:    "uppercase",
			value:   "Staging",
			wantErr: "lowercase",
		},
		{
			name:    "leading hyphen",
			value:   "-x",
			wantErr: "starting with a letter or digit",
		},
		{
			name:    "64 characters",
			value:   strings.Repeat("a", 64),
			wantErr: "1-63",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := runServerless(t, testAppsGroup, appHealthShowCommand, testAppID, testEnvFlag+"="+tc.value)

			if run.err == nil || !strings.Contains(run.err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", run.err, tc.wantErr)
			}
			if len(run.requests) != 0 {
				t.Fatalf("sent %d requests before rejecting the flag", len(run.requests))
			}
		})
	}
}

func TestEnvironmentFlagAcceptsValidNames(t *testing.T) {
	cases := []environmentNameCase{
		{
			name:  "every allowed character",
			value: "prod-1.a_b",
		},
		{
			name:  "63 characters",
			value: strings.Repeat("a", 63),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := runServerless(t, testAppsGroup, appHealthShowCommand, testAppID, testEnvFlag, tc.value)

			if len(run.requests) != 1 {
				t.Fatalf("requests = %d, want 1 (err %v)", len(run.requests), run.err)
			}
			if got := run.requests[0].query.Get("environment"); got != tc.value {
				t.Fatalf("environment = %q, want %q", got, tc.value)
			}
		})
	}
}

func TestOldEnvVariableSyntaxFailsWithTheEnvVarHintBeforeAnyRequest(t *testing.T) {
	cases := []appCommandCase{
		{
			name: testDeployCommand,
			args: []string{testDeployCommand, "app.py", testIDFlag, testAppID, testGPUTypeFlag, testGPUType},
		},
		{
			name: "deprecated apps env set",
			args: []string{testAppsGroup, deprecatedEnvVarsGroup, testSetCommand, testAppID, testEnvKey, testValueFlag, "v"},
		},
		{
			name: "apps env-vars set",
			args: []string{testAppsGroup, envVarsGroup, testSetCommand, testAppID, testEnvKey, testValueFlag, "v"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := runServerless(t, append(tc.args, testEnvFlag, "HF_TOKEN=x")...)

			if run.err == nil || !strings.Contains(run.err.Error(), errEnvironmentIsVariable.Error()) {
				t.Fatalf("err = %v, want %q", run.err, errEnvironmentIsVariable)
			}
			if len(run.requests) != 0 {
				t.Fatalf("sent %d requests", len(run.requests))
			}
		})
	}
}

func TestUsageEventsEnvironmentNeedsAnApp(t *testing.T) {
	run := runServerless(t, testUsageCommand, testEventsCommand, testEnvFlag, testEnvironment)

	if run.err == nil || !strings.Contains(run.err.Error(), "--app") {
		t.Fatalf("err = %v, want it to require --app", run.err)
	}
	if len(run.requests) != 0 {
		t.Fatalf("sent %d requests", len(run.requests))
	}
}

type envVarsGroupCase struct {
	group      string
	wantNotice bool
}

func TestDeprecatedEnvGroupStillRunsAndPointsAtEnvVars(t *testing.T) {
	cases := []envVarsGroupCase{
		{
			group:      deprecatedEnvVarsGroup,
			wantNotice: true,
		},
		{
			group: envVarsGroup,
		},
	}
	for _, tc := range cases {
		t.Run(tc.group, func(t *testing.T) {
			run := runServerless(t, testAppsGroup, tc.group, appHealthListCommand, testAppID)

			if len(run.requests) != 1 || run.requests[0].path != testAppsPath+"/"+testAppID+"/environment-variables" {
				t.Fatalf("requests = %+v", run.requests)
			}
			hasNotice := strings.Contains(run.cobraOutput, "use 'apps env-vars list' instead")
			if hasNotice != tc.wantNotice {
				t.Fatalf("notice shown = %v, want %v; output = %q", hasNotice, tc.wantNotice, run.cobraOutput)
			}
		})
	}
}

func TestAppsHelpListsOnlyTheEnvVarsGroup(t *testing.T) {
	var out bytes.Buffer
	cmd := newAppsCmd(log.New(io.Discard))
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	listed := map[string]struct{}{}
	for line := range strings.SplitSeq(out.String(), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			listed[fields[0]] = struct{}{}
		}
	}
	if _, ok := listed[envVarsGroup]; !ok {
		t.Errorf("help does not list %q:\n%s", envVarsGroup, out.String())
	}
	if _, ok := listed[deprecatedEnvVarsGroup]; ok {
		t.Errorf("help lists the deprecated %q:\n%s", deprecatedEnvVarsGroup, out.String())
	}
}

type nextPageHintCase struct {
	name     string
	args     []string
	body     string
	wantHint string
}

func TestNextPageHintRepeatsTheEnvironment(t *testing.T) {
	const listBody = `{"data":[],"nextCursor":"` + testNextCursor + `"}`
	const logBody = `{"entries":[],"nextCursor":"` + testNextCursor + `"}`
	cases := []nextPageHintCase{
		{
			name:     "apps list",
			args:     []string{testAppsGroup, appHealthListCommand, "--query", "demo"},
			body:     listBody,
			wantHint: testStagingHint + " --query demo",
		},
		{
			name:     "apps endpoints",
			args:     []string{testAppsGroup, "endpoints", testAppID},
			body:     listBody,
			wantHint: testStagingHint,
		},
		{
			name:     "apps events",
			args:     []string{testAppsGroup, testEventsCommand, testAppID, "--type", "error"},
			body:     listBody,
			wantHint: testStagingHint + " --type error",
		},
		{
			name:     "apps workers",
			args:     []string{testAppsGroup, "workers", testAppID},
			body:     listBody,
			wantHint: testStagingHint,
		},
		{
			name:     "apps builds list",
			args:     []string{testAppsGroup, testBuildsGroup, appHealthListCommand, testAppID},
			body:     listBody,
			wantHint: testStagingHint,
		},
		{
			name:     "apps versions list",
			args:     []string{testAppsGroup, testVersionsGroup, appHealthListCommand, testAppID},
			body:     listBody,
			wantHint: testStagingHint,
		},
		{
			name:     "apps env-vars list",
			args:     []string{testAppsGroup, envVarsGroup, appHealthListCommand, testAppID},
			body:     listBody,
			wantHint: testStagingHint,
		},
		{
			name:     "apps tasks",
			args:     []string{testAppsGroup, "tasks", testAppID},
			body:     listBody,
			wantHint: testStagingHint,
		},
		{
			name:     "apps errors",
			args:     []string{testAppsGroup, "errors", testAppID},
			body:     listBody,
			wantHint: testStagingHint + " --window 24h",
		},
		{
			name:     "apps logs",
			args:     []string{testAppsGroup, "logs", testAppID},
			body:     logBody,
			wantHint: testStagingHint + " --window 1h",
		},
		{
			name:     "secrets attachments",
			args:     []string{testSecretsGroup, "attachments", testAppID},
			body:     listBody,
			wantHint: testStagingHint,
		},
		{
			name:     "usage events",
			args:     []string{testUsageCommand, testEventsCommand, "--app", testAppID},
			body:     listBody,
			wantHint: testStagingHint + " --app " + testAppID,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}

			run := runServerlessAgainst(t, handler, append(tc.args, testEnvFlag, testEnvironment)...)

			if run.err != nil {
				t.Fatalf("err = %v", run.err)
			}
			if want := "Next page: " + tc.wantHint + " --cursor " + testNextCursor; !strings.Contains(run.cobraOutput, want) {
				t.Fatalf("output = %q, want it to contain %q", run.cobraOutput, want)
			}
		})
	}
}

type environmentCompletionCase struct {
	name      string
	status    int
	body      string
	wantNames []string
}

func TestEnvironmentFlagCompletesTheOrganizationsEnvironments(t *testing.T) {
	cases := []environmentCompletionCase{
		{
			name:      "environments listed",
			status:    http.StatusOK,
			body:      `{"data":[{"name":"production"},{"name":"staging"}]}`,
			wantNames: []string{testProductionEnvironment, testEnvironment},
		},
		{
			name:   "list refused",
			status: http.StatusUnauthorized,
			body:   `{"title":"Unauthorized","status":401}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != testEnvironmentsPath {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}

			run := runServerlessAgainst(t, handler, "__complete", testAppsGroup, appHealthShowCommand, testAppID, testEnvFlag, "")

			var names []string
			directive := ""
			for line := range strings.SplitSeq(run.cobraOutput, "\n") {
				if strings.HasPrefix(line, ":") {
					directive = line
					break
				}
				names = append(names, line)
			}
			if directive != ":4" {
				t.Fatalf("directive = %q, want :4 (no file completion); output %q", directive, run.cobraOutput)
			}
			if strings.Join(names, ",") != strings.Join(tc.wantNames, ",") {
				t.Fatalf("completions = %q, want %q", names, tc.wantNames)
			}
		})
	}
}

type environmentTextCase struct {
	name        string
	environment string
	want        string
}

func TestAppDashboardURL(t *testing.T) {
	cases := []environmentTextCase{
		{
			name: "no environment",
			want: "https://dash.example/serverless/apps/my-app",
		},
		{
			name:        "staging",
			environment: testEnvironment,
			want:        "https://dash.example/serverless/apps/my-app?environment=staging",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := appDashboardURL("https://dash.example/", testAppID, tc.environment); got != tc.want {
				t.Fatalf("url = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEnvironmentSubject(t *testing.T) {
	cases := []environmentTextCase{
		{
			name: "no environment",
			want: "application my-app",
		},
		{
			name:        "staging",
			environment: testEnvironment,
			want:        "application my-app in staging",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := environmentSubject("application "+testAppID, tc.environment); got != tc.want {
				t.Fatalf("subject = %q, want %q", got, tc.want)
			}
		})
	}
}

type createOnlyHintCase struct {
	name        string
	flag        string
	environment string
	wantCommand string
}

func TestCreateOnlyDeployHintAddressesTheSameCopy(t *testing.T) {
	cases := []createOnlyHintCase{
		{
			name:        "worker setting on the production copy",
			flag:        "max-workers",
			wantCommand: "runware serverless apps scale",
		},
		{
			name:        "worker setting on the staging copy",
			flag:        "max-workers",
			environment: testEnvironment,
			wantCommand: "runware serverless apps scale --env staging",
		},
		{
			name:        "environment variable on the staging copy",
			flag:        envVarFlag,
			environment: testEnvironment,
			wantCommand: "runware serverless apps env-vars --env staging",
		},
		{
			name:        "secret on the staging copy",
			flag:        "secret",
			environment: testEnvironment,
			wantCommand: "runware serverless secrets attach --env staging",
		},
		{
			name:        "display name on the staging copy",
			flag:        "name",
			environment: testEnvironment,
			wantCommand: "runware serverless apps rename --env staging",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := createOnlyDeployHint(tc.flag, tc.environment)

			if want := "use '" + tc.wantCommand + "'"; !strings.HasPrefix(got, want) {
				t.Fatalf("hint = %q, want it to start with %q", got, want)
			}
		})
	}
}

// deployEnvironmentCase drives one deploy under --env against a stub whose
// app lookup and environment lookup answer the given statuses.
type deployEnvironmentCase struct {
	name              string
	appStatus         int
	environmentStatus int
	createStatus      int
	createFlags       []string
	wantErr           string
	wantRequests      []string
}

func TestDeployUnderAnEnvironmentAddressesThatCopy(t *testing.T) {
	appPath := testAppsPath + "/" + testAppID
	environmentPath := testEnvironmentsPath + "/" + testEnvironment
	upload := []string{"POST " + testSourceUploadsPath, "POST " + testSourceUploadsPath + "/" + testSourceID + "/complete"}
	create := []string{testGPUTypeFlag, testGPUType}
	cases := []deployEnvironmentCase{
		{
			name:              "new copy is created",
			appStatus:         http.StatusNotFound,
			environmentStatus: http.StatusOK,
			createStatus:      http.StatusCreated,
			createFlags:       create,
			wantRequests:      append([]string{"GET " + appPath, "GET " + environmentPath}, append(upload, "POST "+testAppsPath)...),
		},
		{
			name:         "existing copy is updated",
			appStatus:    http.StatusOK,
			wantRequests: append([]string{"GET " + appPath}, append(upload, "PATCH "+appPath)...),
		},
		{
			name:         "create-only flag on an existing copy names that copy",
			appStatus:    http.StatusOK,
			createFlags:  create,
			wantErr:      "apps scale --env staging",
			wantRequests: []string{"GET " + appPath},
		},
		{
			name:              "create conflict falls back to update",
			appStatus:         http.StatusNotFound,
			environmentStatus: http.StatusOK,
			createStatus:      http.StatusConflict,
			createFlags:       create,
			wantRequests:      append([]string{"GET " + appPath, "GET " + environmentPath}, append(upload, "POST "+testAppsPath, "PATCH "+appPath)...),
		},
		{
			name:              "unknown environment fails before the upload",
			appStatus:         http.StatusNotFound,
			environmentStatus: http.StatusNotFound,
			createFlags:       create,
			wantErr:           `environment "` + testEnvironment + `" not found`,
			wantRequests:      []string{"GET " + appPath, "GET " + environmentPath},
		},
		{
			name:              "unknown environment is reported before a missing create flag",
			appStatus:         http.StatusNotFound,
			environmentStatus: http.StatusNotFound,
			wantErr:           `environment "` + testEnvironment + `" not found`,
			wantRequests:      []string{"GET " + appPath, "GET " + environmentPath},
		},
		{
			name:              "environment lookup fails",
			appStatus:         http.StatusNotFound,
			environmentStatus: http.StatusInternalServerError,
			createFlags:       create,
			wantErr:           "500",
			wantRequests:      []string{"GET " + appPath, "GET " + environmentPath},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{testModelFile: testPySource})
			serveSourceUpload := newSourceUploadStub(t)
			handler := func(w http.ResponseWriter, r *http.Request) {
				if serveSourceUpload(w, r) {
					return
				}
				switch {
				case r.URL.Path == environmentPath:
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.environmentStatus)
					_, _ = io.WriteString(w, testEnvironmentBody)
				case r.Method == http.MethodGet && r.URL.Path == appPath && tc.appStatus == http.StatusNotFound:
					writeNotFound(w)
				case r.Method == http.MethodPost && r.URL.Path == testAppsPath && tc.createStatus == http.StatusConflict:
					w.Header().Set("Content-Type", "application/problem+json")
					w.WriteHeader(http.StatusConflict)
					_, _ = io.WriteString(w, `{"title":"Conflict","status":409}`)
				case r.Method == http.MethodPost && r.URL.Path == testAppsPath:
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_, _ = io.WriteString(w, activeAppBody(""))
				default:
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, activeAppBody(""))
				}
			}
			args := append([]string{testDeployCommand, testModelFile, testSrcDirFlag, dir, testIDFlag, testAppID, testEnvFlag, testEnvironment}, tc.createFlags...)

			run := runServerlessAgainst(t, handler, args...)

			got := make([]string, len(run.requests))
			for i, r := range run.requests {
				got[i] = r.String()
				scoped := strings.HasPrefix(r.path, testAppsPath)
				if scoped != r.query.Has("environment") || (scoped && r.query.Get("environment") != testEnvironment) {
					t.Errorf("%s: environment = %q, want it only on app requests, as %q", r, r.query.Get("environment"), testEnvironment)
				}
			}
			if strings.Join(got, ", ") != strings.Join(tc.wantRequests, ", ") {
				t.Fatalf("requests = %v, want %v (err %v)", got, tc.wantRequests, run.err)
			}
			if tc.wantErr == "" && run.err != nil {
				t.Fatalf("deploy: %v", run.err)
			}
			if tc.wantErr != "" && (run.err == nil || !strings.Contains(run.err.Error(), tc.wantErr)) {
				t.Fatalf("err = %v, want %q", run.err, tc.wantErr)
			}
		})
	}
}

func TestEnvironmentCompletionGivesUpWhenTheAPIDoesNotAnswer(t *testing.T) {
	const timeout = 20 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	started := time.Now()

	names, directive := environmentCompletion(log.New(io.Discard), timeout)(cmd, nil, "")

	if waited := time.Since(started); waited > 50*timeout {
		t.Fatalf("completion waited %v for a silent API", waited)
	}
	if len(names) != 0 || directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("names = %v, directive = %v, want no names and no file completion", names, directive)
	}
}
