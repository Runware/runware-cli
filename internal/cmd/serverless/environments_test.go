package serverless

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	testEnvironmentsGroup = "environments"
	testDefaultsGroup     = "defaults"
	testClearFlag         = "--clear"
	testMaxWorkersFlag    = "--max-workers"
	testMaxGPUsFlag       = "--max-gpus"
	testInheritedGPUs     = "8 (org default)"
	testUpdateCommand     = "update"
	testDefaultsPath      = "/v1/environment-defaults"
	testStagingPath       = testEnvironmentsPath + "/" + testEnvironment
	testRefusedName       = "Staging"
	testNameRule          = "environment name must be"

	// testStagingEnvironment sets its own worker cap and inherits its GPU cap.
	testStagingEnvironment = `{
		"name":"staging",
		"isDefault":false,
		"maxConcurrentWorkers":2,
		"monthlyBudget":{"amount":"50.00","currency":"USD"},
		"budgetAlertPercents":[50,80,100],
		"pauseAtBudget":true,
		"unauthenticatedAccess":null,
		"resolved":{"maxConcurrentWorkers":2,"maxConcurrentGpus":8,"unauthenticatedAccess":"deny"},
		"createdAt":"2026-09-01T00:00:00Z",
		"updatedAt":"2026-09-02T00:00:00Z"
	}`
	// testProductionEnvironmentBody sets nothing and inherits no GPU cap.
	testProductionEnvironmentBody = `{
		"name":"production",
		"isDefault":true,
		"budgetAlertPercents":[],
		"pauseAtBudget":false,
		"resolved":{"maxConcurrentWorkers":4,"unauthenticatedAccess":"deny"},
		"createdAt":"2026-08-01T00:00:00Z",
		"updatedAt":"2026-08-01T00:00:00Z"
	}`
)

// bodyRecorder answers each request with a fixed status and body and keeps
// every request body it received, by method and path.
type bodyRecorder struct {
	mu     sync.Mutex
	bodies map[string]string
}

func (b *bodyRecorder) body(t *testing.T, key string) map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	raw, ok := b.bodies[key]
	if !ok {
		t.Fatalf("no %s request; got %v", key, b.bodies)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("%s body %q: %v", key, raw, err)
	}
	return got
}

// environmentAPI is a stub API for the environment commands: responses maps
// "METHOD /path" to the status and body it answers with.
func environmentAPI(t *testing.T, rec *bodyRecorder, responses map[string]stubResponse) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		rec.mu.Lock()
		if rec.bodies == nil {
			rec.bodies = map[string]string{}
		}
		rec.bodies[key] = string(raw)
		rec.mu.Unlock()
		resp, ok := responses[key]
		if !ok {
			t.Errorf("unexpected %s", key)
			writeNotFound(w)
			return
		}
		contentType := "application/json"
		if resp.status >= http.StatusBadRequest {
			contentType = "application/problem+json"
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(resp.status)
		_, _ = io.WriteString(w, resp.body)
	}
}

// stubResponse is one canned API answer.
type stubResponse struct {
	status int
	body   string
}

func decodeEnvironment(t *testing.T, body string) serverlessapi.Environment {
	t.Helper()
	var env serverlessapi.Environment
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	return env
}

func decodeJSONObject(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("%q: %v", body, err)
	}
	return m
}

func TestEnvironmentsListShowsOwnInheritedAndMissingValues(t *testing.T) {
	envs := environmentsResult{
		decodeEnvironment(t, testProductionEnvironmentBody),
		decodeEnvironment(t, testStagingEnvironment),
	}

	want := [][]any{
		{"production", "yes", "4 (org default)", "-", "-", "-", "no"},
		{testEnvironment, "no", "2", testInheritedGPUs, "50.00 USD", "50, 80, 100%", "yes"},
	}
	if got := envs.Rows(); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v\nwant %v", got, want)
	}
	wantHeaders := []string{"Name", "Default", "Max workers", colMaxGPUs, "Monthly budget", "Budget alerts", "Pause at budget"}
	if got := envs.Headers(); !reflect.DeepEqual(got, wantHeaders) {
		t.Fatalf("headers = %v, want %v", got, wantHeaders)
	}
}

func TestEnvironmentsShowListsEverySetting(t *testing.T) {
	env := environmentResult(decodeEnvironment(t, testStagingEnvironment))

	want := [][]any{
		{"Name", testEnvironment},
		{"Default", "no"},
		{"Max workers", "2"},
		{colMaxGPUs, testInheritedGPUs},
		{"Monthly budget", "50.00 USD"},
		{"Budget alerts", "50, 80, 100%"},
		{"Pause at budget", "yes"},
		{"Created", "2026-09-01T00:00:00Z"},
		{"Updated", "2026-09-02T00:00:00Z"},
	}
	if got := env.Rows(); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v\nwant %v", got, want)
	}
}

func TestEnvironmentJSONIsTheAPIObject(t *testing.T) {
	out, err := json.Marshal(environmentResult(decodeEnvironment(t, testStagingEnvironment)))
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSONObject(t, string(out))
	want := decodeJSONObject(t, testStagingEnvironment)
	delete(want, "unauthenticatedAccess") // null is omitted on the way back out
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("json = %v\nwant %v", got, want)
	}
}

// environmentReadCase is a read command, what the stub API answers and what
// the output must contain.
type environmentReadCase struct {
	name     string
	args     []string
	response map[string]stubResponse
	want     []string
}

func TestEnvironmentsListAndShowRunAgainstTheAPI(t *testing.T) {
	cases := []environmentReadCase{
		{
			name: "list",
			args: []string{testEnvironmentsGroup, "list"},
			response: map[string]stubResponse{
				"GET " + testEnvironmentsPath: {
					status: http.StatusOK,
					body:   `{"data":[` + testProductionEnvironmentBody + `,` + testStagingEnvironment + `]}`,
				},
			},
			want: []string{"production", "4 (org default)", "staging", testInheritedGPUs, "50.00 USD", "50, 80, 100%"},
		},
		{
			name: "show",
			args: []string{testEnvironmentsGroup, "show", testEnvironment},
			response: map[string]stubResponse{
				"GET " + testStagingPath: {
					status: http.StatusOK,
					body:   testStagingEnvironment,
				},
			},
			want: []string{colMaxGPUs, testInheritedGPUs, "Pause at budget"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := runServerlessAgainst(t, environmentAPI(t, &bodyRecorder{}, tc.response), tc.args...)

			if run.err != nil {
				t.Fatal(run.err)
			}
			for _, w := range tc.want {
				if !strings.Contains(run.stdout, w) {
					t.Errorf("output missing %q:\n%s", w, run.stdout)
				}
			}
		})
	}
}

func TestEnvironmentDefaultsShowCapsOrNoCap(t *testing.T) {
	workers := int32(5)
	defaults := environmentDefaultsResult{
		MaxConcurrentWorkers:  &workers,
		UnauthenticatedAccess: "deny",
	}

	want := [][]any{
		{"Max workers", "5"},
		{colMaxGPUs, "-"},
	}
	if got := defaults.Rows(); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

type environmentBodyCase struct {
	name string
	args []string
	want string
}

func TestEnvironmentsCreateSendsOnlyTheFlagsSet(t *testing.T) {
	cases := []environmentBodyCase{
		{
			name: "no flags",
			want: `{"name":"staging"}`,
		},
		{
			name: "every flag",
			args: []string{testMaxWorkersFlag, "4", testMaxGPUsFlag, "8", "--budget", "49.99", "--alert-percents", "25,75", "--pause-at-budget"},
			want: `{"name":"staging","maxConcurrentWorkers":4,"maxConcurrentGpus":8,"monthlyBudget":{"amount":"49.99","currency":"USD"},"budgetAlertPercents":[25,75],"pauseAtBudget":true}`,
		},
		{
			name: "zero caps and pause off are sent",
			args: []string{testMaxWorkersFlag, "0", testMaxGPUsFlag, "0", "--pause-at-budget=false"},
			want: `{"name":"staging","maxConcurrentWorkers":0,"maxConcurrentGpus":0,"pauseAtBudget":false}`,
		},
		{
			name: "whole dollar budget",
			args: []string{"--budget", "50"},
			want: `{"name":"staging","monthlyBudget":{"amount":"50.00","currency":"USD"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &bodyRecorder{}
			api := environmentAPI(t, rec, map[string]stubResponse{
				"POST " + testEnvironmentsPath: {
					status: http.StatusCreated,
					body:   testStagingEnvironment,
				},
			})

			run := runServerlessAgainst(t, api, append([]string{testEnvironmentsGroup, "create", testEnvironment}, tc.args...)...)

			if run.err != nil {
				t.Fatal(run.err)
			}
			if got, want := rec.body(t, "POST "+testEnvironmentsPath), decodeJSONObject(t, tc.want); !reflect.DeepEqual(got, want) {
				t.Fatalf("body = %v\nwant %v", got, want)
			}
		})
	}
}

func TestEnvironmentsUpdateSendsAbsentNullOrValue(t *testing.T) {
	cases := []environmentBodyCase{
		{
			name: "one cap",
			args: []string{testMaxWorkersFlag, "3"},
			want: `{"maxConcurrentWorkers":3}`,
		},
		{
			name: "zero cap is a value",
			args: []string{testMaxGPUsFlag, "0"},
			want: `{"maxConcurrentGpus":0}`,
		},
		{
			name: "pause off",
			args: []string{"--pause-at-budget=false"},
			want: `{"pauseAtBudget":false}`,
		},
		{
			name: "budget alone",
			args: []string{"--budget", "10"},
			want: `{"monthlyBudget":{"amount":"10.00","currency":"USD"}}`,
		},
		{
			name: "alerts alone",
			args: []string{"--alert-percents", "25,75"},
			want: `{"budgetAlertPercents":[25,75]}`,
		},
		{
			name: "clear a cap",
			args: []string{testClearFlag, maxWorkersField},
			want: `{"maxConcurrentWorkers":null}`,
		},
		{
			name: "clear every field, comma separated and repeated",
			args: []string{testClearFlag, "max-workers, max-gpus", testClearFlag, budgetField, testClearFlag, alertPercentsField},
			want: `{"maxConcurrentWorkers":null,"maxConcurrentGpus":null,"monthlyBudget":null,"budgetAlertPercents":[]}`,
		},
		{
			name: "clear one field and set another",
			args: []string{testClearFlag, budgetField, testMaxWorkersFlag, "6"},
			want: `{"monthlyBudget":null,"maxConcurrentWorkers":6}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &bodyRecorder{}
			api := environmentAPI(t, rec, map[string]stubResponse{
				"PATCH " + testStagingPath: {
					status: http.StatusOK,
					body:   testStagingEnvironment,
				},
			})

			run := runServerlessAgainst(t, api, append([]string{testEnvironmentsGroup, testUpdateCommand, testEnvironment}, tc.args...)...)

			if run.err != nil {
				t.Fatal(run.err)
			}
			if got, want := rec.body(t, "PATCH "+testStagingPath), decodeJSONObject(t, tc.want); !reflect.DeepEqual(got, want) {
				t.Fatalf("body = %v\nwant %v", got, want)
			}
		})
	}
}

type localErrorCase struct {
	name    string
	args    []string
	wantErr string
}

func TestEnvironmentCommandsRejectBeforeAnyRequest(t *testing.T) {
	update := []string{testEnvironmentsGroup, testUpdateCommand, testEnvironment}
	create := []string{testEnvironmentsGroup, "create", testEnvironment}
	defaultsSet := []string{testEnvironmentsGroup, testDefaultsGroup, testSetCommand}
	cases := []localErrorCase{
		{
			name:    "update with nothing to change",
			args:    update,
			wantErr: errNothingToUpdate.Error(),
		},
		{
			name:    "update clears an unknown field",
			args:    append(update, testClearFlag, pauseAtBudgetFlag),
			wantErr: `--clear takes max-workers, max-gpus, budget, alert-percents; got "pause-at-budget"`,
		},
		{
			name:    "update sets and clears the same field",
			args:    append(update, "--budget", "5", testClearFlag, budgetField),
			wantErr: "--budget and --clear budget both change budget; pass one",
		},
		{
			name:    "update budget not an amount",
			args:    append(update, "--budget", "5 USD"),
			wantErr: errBudgetAmount.Error(),
		},
		{
			name:    "create a name the API refuses",
			args:    []string{testEnvironmentsGroup, "create", testRefusedName},
			wantErr: testNameRule,
		},
		{
			name:    "show a name the API refuses",
			args:    []string{testEnvironmentsGroup, appHealthShowCommand, testRefusedName},
			wantErr: testNameRule,
		},
		{
			name:    "update a name with an equals sign",
			args:    []string{testEnvironmentsGroup, testUpdateCommand, "a=b", testMaxWorkersFlag, "1"},
			wantErr: testNameRule,
		},
		{
			name:    "show without a name",
			args:    []string{testEnvironmentsGroup, appHealthShowCommand},
			wantErr: "accepts 1 arg(s), received 0",
		},
		{
			name:    "delete a name the API refuses",
			args:    []string{testEnvironmentsGroup, testDeleteCommand, testRefusedName, testYesFlag},
			wantErr: testNameRule,
		},
		{
			name:    "create negative budget",
			args:    append(create, "--budget", "-5"),
			wantErr: errBudgetAmount.Error(),
		},
		{
			name:    "create budget with a trailing point",
			args:    append(create, "--budget", "50."),
			wantErr: errBudgetAmount.Error(),
		},
		{
			name:    "create takes no --clear",
			args:    append(create, testClearFlag, budgetField),
			wantErr: "unknown flag: --clear",
		},
		{
			name:    "defaults set with nothing to set",
			args:    defaultsSet,
			wantErr: errNothingToSetOnDefaults.Error(),
		},
		{
			name:    "defaults set clears a field it does not take",
			args:    append(defaultsSet, testClearFlag, budgetField),
			wantErr: `--clear takes max-workers, max-gpus; got "budget"`,
		},
		{
			name:    "defaults set sets and clears the same field",
			args:    append(defaultsSet, testMaxGPUsFlag, "1", testClearFlag, maxGPUsField),
			wantErr: "--max-gpus and --clear max-gpus both change max-gpus; pass one",
		},
		{
			name:    "environments take no --env",
			args:    []string{testEnvironmentsGroup, "list", testEnvFlag, testEnvironment},
			wantErr: "unknown flag: " + testEnvFlag,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := runServerless(t, tc.args...)

			if run.err == nil || !strings.Contains(run.err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", run.err, tc.wantErr)
			}
			if len(run.requests) != 0 {
				t.Fatalf("sent %v", run.requests)
			}
		})
	}
}

// moneyAmountSchema is the part of the spec's MoneyAmount schema a test reads.
type moneyAmountSchema struct {
	Properties map[string]openAPISchema `yaml:"properties"`
}

// moneyAmountSpec is the part of the vendored API spec that holds MoneyAmount.
type moneyAmountSpec struct {
	Components moneyAmountComponents `yaml:"components"`
}

// moneyAmountComponents holds the spec's MoneyAmount schema.
type moneyAmountComponents struct {
	Schemas map[string]moneyAmountSchema `yaml:"schemas"`
}

func TestBudgetAppliesTheSpecsAmountRule(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "serverless", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var spec moneyAmountSpec
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	want := spec.Components.Schemas["MoneyAmount"].Properties["amount"].Pattern

	if want == "" {
		t.Fatal("the spec has no MoneyAmount amount pattern")
	}
	if got := moneyAmountPattern.String(); got != want {
		t.Fatalf("moneyAmountPattern = %q, spec says %q", got, want)
	}
}

type defaultsSetCase struct {
	name    string
	current string
	args    []string
	want    string
}

func TestEnvironmentDefaultsSetSendsBackWhatItDoesNotChange(t *testing.T) {
	cases := []defaultsSetCase{
		{
			name:    "set one cap keeps the other and the access setting",
			current: `{"maxConcurrentWorkers":5,"maxConcurrentGpus":2,"unauthenticatedAccess":"deny"}`,
			args:    []string{testMaxGPUsFlag, "4"},
			want:    `{"maxConcurrentWorkers":5,"maxConcurrentGpus":4,"unauthenticatedAccess":"deny"}`,
		},
		{
			name:    "clear one cap keeps the other and the access setting",
			current: `{"maxConcurrentWorkers":5,"maxConcurrentGpus":2,"unauthenticatedAccess":"allow"}`,
			args:    []string{testClearFlag, maxWorkersField},
			want:    `{"maxConcurrentGpus":2,"unauthenticatedAccess":"allow"}`,
		},
		{
			name:    "clear the other cap",
			current: `{"maxConcurrentWorkers":5,"maxConcurrentGpus":2,"unauthenticatedAccess":"allow"}`,
			args:    []string{testClearFlag, maxGPUsField},
			want:    `{"maxConcurrentWorkers":5,"unauthenticatedAccess":"allow"}`,
		},
		{
			name:    "set a cap where there was none",
			current: `{"unauthenticatedAccess":"allow"}`,
			args:    []string{testMaxWorkersFlag, "0"},
			want:    `{"maxConcurrentWorkers":0,"unauthenticatedAccess":"allow"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &bodyRecorder{}
			api := environmentAPI(t, rec, map[string]stubResponse{
				"GET " + testDefaultsPath: {
					status: http.StatusOK,
					body:   tc.current,
				},
				"PUT " + testDefaultsPath: {
					status: http.StatusOK,
					body:   tc.want,
				},
			})

			run := runServerlessAgainst(t, api, append([]string{testEnvironmentsGroup, testDefaultsGroup, testSetCommand}, tc.args...)...)

			if run.err != nil {
				t.Fatal(run.err)
			}
			if got, want := rec.body(t, "PUT "+testDefaultsPath), decodeJSONObject(t, tc.want); !reflect.DeepEqual(got, want) {
				t.Fatalf("body = %v\nwant %v", got, want)
			}
		})
	}
}

func TestEnvironmentDefaultsSetWritesNothingWhenTheReadFails(t *testing.T) {
	api := environmentAPI(t, &bodyRecorder{}, map[string]stubResponse{
		"GET " + testDefaultsPath: {
			status: http.StatusServiceUnavailable,
			body:   `{"title":"Service Unavailable","status":503}`,
		},
	})

	run := runServerlessAgainst(t, api, testEnvironmentsGroup, testDefaultsGroup, testSetCommand, testMaxWorkersFlag, "1")

	if run.err == nil {
		t.Fatal("want the read error")
	}
	for _, r := range run.requests {
		if r.method != http.MethodGet {
			t.Fatalf("sent %s after a failed read", r)
		}
	}
}

func TestEnvironmentDefaultsGetReadsTheDefaults(t *testing.T) {
	api := environmentAPI(t, &bodyRecorder{}, map[string]stubResponse{
		"GET " + testDefaultsPath: {
			status: http.StatusOK,
			body:   `{"maxConcurrentWorkers":7,"unauthenticatedAccess":"allow"}`,
		},
	})

	run := runServerlessAgainst(t, api, testEnvironmentsGroup, testDefaultsGroup, "get")

	if run.err != nil {
		t.Fatal(run.err)
	}
	if !strings.Contains(run.stdout, "7") {
		t.Fatalf("output missing the worker cap:\n%s", run.stdout)
	}
}

type deleteConflictCase struct {
	name        string
	environment string
	status      int
	want        string
}

func TestEnvironmentsDeleteNamesTheCommandsThatEmptyIt(t *testing.T) {
	const detail = "Environment still has live apps"
	hint := "\nList what it still holds with 'runware serverless apps list --env staging' and " +
		"'runware serverless secrets list --env staging', remove each with 'apps delete' or 'secrets remove' under the same --env, and retry once no app shows as deleting."
	cases := []deleteConflictCase{
		{
			name:        "still holds apps",
			environment: testEnvironment,
			status:      http.StatusConflict,
			want:        detail + hint,
		},
		{
			name:        "production is refused as production",
			environment: testProductionEnvironment,
			status:      http.StatusConflict,
			want:        detail,
		},
		{
			name:        "not found gets no hint",
			environment: testEnvironment,
			status:      http.StatusNotFound,
			want:        detail,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := environmentAPI(t, &bodyRecorder{}, map[string]stubResponse{
				"DELETE " + testEnvironmentsPath + "/" + tc.environment: {
					status: tc.status,
					body:   fmt.Sprintf(`{"title":"x","status":%d,"detail":%q}`, tc.status, detail),
				},
			})

			run := runServerlessAgainst(t, api, testEnvironmentsGroup, testDeleteCommand, tc.environment, testYesFlag)

			var re *transport.RunwareError
			if !errors.As(run.err, &re) {
				t.Fatalf("err = %T %v, want *transport.RunwareError", run.err, run.err)
			}
			if re.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", re.StatusCode, tc.status)
			}
			if re.Message != tc.want {
				t.Fatalf("message = %q\nwant %q", re.Message, tc.want)
			}
		})
	}
}

func TestEnvironmentsDeleteAsksFirst(t *testing.T) {
	run := runServerlessAnswering(t, "n\n", testEnvironmentsGroup, testDeleteCommand, testEnvironment)

	if !errors.Is(run.err, errDeleteCancelled) {
		t.Fatalf("err = %v, want the delete cancelled", run.err)
	}
	if want := "Delete environment " + testEnvironment + "? [y/N]"; !strings.Contains(run.cobraOutput, want) {
		t.Fatalf("prompt = %q, want %q", run.cobraOutput, want)
	}
	if len(run.requests) != 0 {
		t.Fatalf("sent %v after a refused prompt", run.requests)
	}
}

func TestEnvironmentsDeleteWithYesDeletes(t *testing.T) {
	for _, flag := range []string{"-y", testYesFlag, "--force"} {
		t.Run(flag, func(t *testing.T) {
			api := environmentAPI(t, &bodyRecorder{}, map[string]stubResponse{
				"DELETE " + testStagingPath: {
					status: http.StatusNoContent,
				},
			})

			run := runServerlessAgainst(t, api, testEnvironmentsGroup, testDeleteCommand, testEnvironment, flag)

			if run.err != nil {
				t.Fatal(run.err)
			}
			if len(run.requests) != 1 || run.requests[0].String() != "DELETE "+testStagingPath {
				t.Fatalf("requests = %v, want one DELETE %s", run.requests, testStagingPath)
			}
			if !strings.Contains(run.stdout, testEnvironment) {
				t.Fatalf("output = %q, want the deleted name", run.stdout)
			}
		})
	}
}

// completions runs a __complete request and returns the names offered and
// the directive line.
func completions(t *testing.T, handler http.HandlerFunc, args ...string) ([]string, string) {
	t.Helper()
	run := runServerlessAgainst(t, handler, append([]string{"__complete"}, args...)...)
	var names []string
	for line := range strings.SplitSeq(run.cobraOutput, "\n") {
		if strings.HasPrefix(line, ":") {
			return names, line
		}
		names = append(names, line)
	}
	t.Fatalf("no directive in %q", run.cobraOutput)
	return nil, ""
}

func TestEnvironmentNameArgumentCompletesFromTheList(t *testing.T) {
	list := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"name":"production"},{"name":"staging"}]}`)
	}
	noFiles := fmt.Sprintf(":%d", cobra.ShellCompDirectiveNoFileComp)
	for _, command := range []string{"show", testUpdateCommand, testDeleteCommand} {
		t.Run(command, func(t *testing.T) {
			names, directive := completions(t, list, testEnvironmentsGroup, command, "")
			if directive != noFiles || strings.Join(names, ",") != "production,staging" {
				t.Fatalf("completions = %q %s, want production,staging %s", names, directive, noFiles)
			}

			names, _ = completions(t, list, testEnvironmentsGroup, command, testEnvironment, "")
			if len(names) != 0 {
				t.Fatalf("second argument completed %q", names)
			}
		})
	}
}

// clearCompletionCase is a command line completing --clear and the fields
// offered.
type clearCompletionCase struct {
	name string
	args []string
	want string
}

func TestClearCompletesTheFieldsTheCommandTakes(t *testing.T) {
	cases := []clearCompletionCase{
		{
			name: testUpdateCommand,
			args: []string{testEnvironmentsGroup, testUpdateCommand, testEnvironment, testClearFlag, ""},
			want: "max-workers,max-gpus,budget,alert-percents",
		},
		{
			name: "defaults set",
			args: []string{testEnvironmentsGroup, testDefaultsGroup, testSetCommand, testClearFlag, ""},
			want: "max-workers,max-gpus",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			names, _ := completions(t, nil, tc.args...)
			if got := strings.Join(names, ","); got != tc.want {
				t.Fatalf("completions = %q, want %q", got, tc.want)
			}
		})
	}
}
