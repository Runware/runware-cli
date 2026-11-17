package serverless

import (
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/runware/runware-cli/internal/api/transport"
)

const (
	groupByFlag         = "--group-by"
	app                 = "app"
	wipFlag             = "--workload-identity-provider"
	roleARNFlag         = "--role-arn"
	clearFlagName       = "--clear"
	clearSAField        = "service-account-email"
	serviceAccountFlag  = "--service-account-email"
	testAudience        = "runware:org-1"
	kindFlag            = "--kind"
	audienceFlag        = "--audience"
	kindAws             = "aws"
	kindGcp             = "gcp"
	kindGeneric         = "generic"
	createCommand       = "create"
	nameKey             = "name"
	formatJSON          = "json"
	formatYAML          = "yaml"
	testConnectorsGroup = "connectors"
	testConnectorsPath  = "/v1/oidc-connectors"
	testConnectorName   = "aws-prod"
	testConnectorPath   = testConnectorsPath + "/" + testConnectorName
	testAppConnectors   = "/v1/apps/" + testAppID + "/oidc-connectors"
	testRoleARN         = "arn:aws:iam::123456789012:role/runware-worker"
	testNewRoleARN      = "arn:aws:iam::123456789012:role/runware-worker-v2"
	testWIP             = "projects/123456789/locations/global/workloadIdentityPools/runware/providers/runware"
	testNewWIP          = "projects/123456789/locations/global/workloadIdentityPools/runware/providers/runware-v2"
	testServiceAccount  = "worker@my-project.iam.gserviceaccount.com"
	testSubject         = "worker:org:0190a6a8-0000-7000-8000-00000000000a:env:0190a6a8-0000-7000-8000-0000000000e1:app:my-app"
	testSubjectPattern  = "worker:org:0190a6a8-0000-7000-8000-00000000000a:env:0190a6a8-0000-7000-8000-0000000000e1:*"
	testStagingSubject  = "worker:org:0190a6a8-0000-7000-8000-00000000000a:env:0190a6a8-0000-7000-8000-0000000000e2:app:my-app"
	testConnectorTail   = `"createdAt":"2026-11-01T00:00:00Z","updatedAt":"2026-11-02T00:00:00Z"`
	testAWSConnector    = `{"id":"11111111-1111-1111-1111-111111111111","name":"aws-prod","kind":"aws","audience":"runware:org-1",` +
		`"aws":{"roleArn":"` + testRoleARN + `"},"issuer":"https://oidc.runware.example",` + testConnectorTail + `}`
	testGCPConnector = `{"id":"22222222-2222-2222-2222-222222222222","name":"gcp-prod","kind":"gcp","audience":"//iam.googleapis.com/` + testWIP + `",` +
		`"gcp":{"workloadIdentityProvider":"` + testWIP + `","serviceAccountEmail":"` + testServiceAccount + `"},` + testConnectorTail + `}`
	testGCPWithoutSA = `{"id":"22222222-2222-2222-2222-222222222222","name":"gcp-prod","kind":"gcp","audience":"//iam.googleapis.com/` + testWIP + `",` +
		`"gcp":{"workloadIdentityProvider":"` + testWIP + `"},` + testConnectorTail + `}`
	testGenericConnector = `{"id":"33333333-3333-3333-3333-333333333333","name":"vault","kind":"generic","audience":"https://vault.example.com",` + testConnectorTail + `}`
	testAttachedAWS      = `{"id":"11111111-1111-1111-1111-111111111111","name":"aws-prod","kind":"aws","audience":"runware:org-1",` +
		`"aws":{"roleArn":"` + testRoleARN + `"},"subject":"` + testSubject + `","subjectPattern":"` + testSubjectPattern + `",` + testConnectorTail + `}`
)

func connectorAPI(t *testing.T, rec *bodyRecorder, responses map[string]stubResponse) http.HandlerFunc {
	t.Helper()
	return environmentAPI(t, rec, responses)
}

func TestConnectorsCreateSendsTheBodyOfItsKind(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		response string
		want     string
	}{
		{
			name:     kindAws,
			args:     []string{testConnectorName, kindFlag, kindAws, roleARNFlag, testRoleARN},
			response: testAWSConnector,
			want:     `{"name":"aws-prod","kind":"aws","aws":{"roleArn":"` + testRoleARN + `"}}`,
		},
		{
			name:     "aws with its own audience",
			args:     []string{testConnectorName, kindFlag, kindAws, roleARNFlag, testRoleARN, audienceFlag, testAudience},
			response: testAWSConnector,
			want:     `{"name":"aws-prod","kind":"aws","audience":"runware:org-1","aws":{"roleArn":"` + testRoleARN + `"}}`,
		},
		{
			name:     kindGcp,
			args:     []string{"gcp-prod", kindFlag, kindGcp, wipFlag, testWIP, "--service-account-email", testServiceAccount},
			response: testGCPConnector,
			want:     `{"name":"gcp-prod","kind":"gcp","gcp":{"workloadIdentityProvider":"` + testWIP + `","serviceAccountEmail":"` + testServiceAccount + `"}}`,
		},
		{
			name:     "gcp without a service account",
			args:     []string{"gcp-prod", kindFlag, kindGcp, wipFlag, testWIP},
			response: testGCPConnector,
			want:     `{"name":"gcp-prod","kind":"gcp","gcp":{"workloadIdentityProvider":"` + testWIP + `"}}`,
		},
		{
			name:     kindGeneric,
			args:     []string{"vault", kindFlag, kindGeneric, audienceFlag, "https://vault.example.com"},
			response: testGenericConnector,
			want:     `{"name":"vault","kind":"generic","audience":"https://vault.example.com"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &bodyRecorder{}
			api := connectorAPI(t, rec, map[string]stubResponse{
				"POST " + testConnectorsPath: {status: http.StatusCreated, body: tc.response},
			})

			run := runServerlessAgainst(t, api, append([]string{testConnectorsGroup, createCommand}, tc.args...)...)

			if run.err != nil {
				t.Fatal(run.err)
			}
			if got, want := rec.body(t, "POST "+testConnectorsPath), decodeJSONObject(t, tc.want); !reflect.DeepEqual(got, want) {
				t.Fatalf("body = %v\nwant %v", got, want)
			}
			if !strings.Contains(run.stdout, decodeJSONObject(t, tc.response)[nameKey].(string)) {
				t.Fatalf("output does not show the created connector:\n%s", run.stdout)
			}
		})
	}
}

func TestConnectorsCreateRefusesWhatTheKindCannotTake(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no kind",
			args: []string{"x", roleARNFlag, testRoleARN},
			want: `required flag(s) "kind" not set`,
		},
		{
			name: "unknown kind lists the valid ones",
			args: []string{"x", kindFlag, "azure"},
			want: "must be one of: aws, gcp, generic",
		},
		{
			name: "aws without a role",
			args: []string{"x", kindFlag, kindAws},
			want: "--role-arn is required for an aws connector",
		},
		{
			name: "gcp without a provider",
			args: []string{"x", kindFlag, kindGcp, "--service-account-email", testServiceAccount},
			want: "--workload-identity-provider is required for a gcp connector",
		},
		{
			name: "generic without an audience",
			args: []string{"x", kindFlag, kindGeneric},
			want: "--audience is required for a generic connector",
		},
		{
			name: "aws with a gcp flag",
			args: []string{"x", kindFlag, kindAws, roleARNFlag, testRoleARN, wipFlag, testWIP},
			want: "--workload-identity-provider do not apply to a aws connector",
		},
		{
			name: "generic with a role",
			args: []string{"x", kindFlag, kindGeneric, audienceFlag, "a", roleARNFlag, testRoleARN},
			want: "--role-arn do not apply to a generic connector",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := runServerless(t, append([]string{testConnectorsGroup, createCommand}, tc.args...)...)

			if run.err == nil || !strings.Contains(run.err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", run.err, tc.want)
			}
			if len(run.requests) != 0 {
				t.Fatalf("requests = %v, want none", run.requests)
			}
		})
	}
}

func TestConnectorsCreateSurfacesTheAPIRefusal(t *testing.T) {
	api := connectorAPI(t, &bodyRecorder{}, map[string]stubResponse{
		"POST " + testConnectorsPath: {status: http.StatusConflict, body: `{"type":"about:blank","title":"Conflict","status":409,"detail":"connector name is already in use"}`},
	})

	run := runServerlessAgainst(t, api, testConnectorsGroup, createCommand, "aws-prod", kindFlag, kindAws, roleARNFlag, testRoleARN)

	if run.err == nil || !strings.Contains(run.err.Error(), "connector name is already in use") {
		t.Fatalf("err = %v, want the API's conflict detail", run.err)
	}
}

func TestConnectorsUpdateKeepsWhatTheFlagsLeaveOut(t *testing.T) {
	cases := []struct {
		name    string
		current string
		args    []string
		want    string
	}{
		{
			name:    "aws role only keeps the audience",
			current: testAWSConnector,
			args:    []string{testConnectorName, roleARNFlag, testNewRoleARN},
			want:    `{"audience":"runware:org-1","aws":{"roleArn":"` + testNewRoleARN + `"}}`,
		},
		{
			name:    "aws audience only keeps the role",
			current: testAWSConnector,
			args:    []string{testConnectorName, audienceFlag, "runware:other"},
			want:    `{"audience":"runware:other","aws":{"roleArn":"` + testRoleARN + `"}}`,
		},
		{
			name:    "gcp service account only keeps the provider",
			current: testGCPConnector,
			args:    []string{testConnectorName, "--service-account-email", "other@my-project.iam.gserviceaccount.com"},
			want: `{"audience":"//iam.googleapis.com/` + testWIP + `","gcp":{"workloadIdentityProvider":"` + testWIP +
				`","serviceAccountEmail":"other@my-project.iam.gserviceaccount.com"}}`,
		},
		{
			name:    "gcp clear removes the service account and keeps the provider",
			current: testGCPConnector,
			args:    []string{testConnectorName, clearFlagName, clearSAField},
			want:    `{"audience":"//iam.googleapis.com/` + testWIP + `","gcp":{"workloadIdentityProvider":"` + testWIP + `"}}`,
		},
		{
			name:    "gcp clear with a new provider recomputes the audience",
			current: testGCPConnector,
			args:    []string{testConnectorName, clearFlagName, clearSAField, wipFlag, testNewWIP},
			want:    `{"audience":"//iam.googleapis.com/` + testNewWIP + `","gcp":{"workloadIdentityProvider":"` + testNewWIP + `"}}`,
		},
		{
			name:    "gcp clear with a new provider goes through when there is no service account to clear",
			current: testGCPWithoutSA,
			args:    []string{testConnectorName, clearFlagName, clearSAField, wipFlag, testNewWIP},
			want:    `{"audience":"//iam.googleapis.com/` + testNewWIP + `","gcp":{"workloadIdentityProvider":"` + testNewWIP + `"}}`,
		},
		{
			name:    "generic audience",
			current: testGenericConnector,
			args:    []string{testConnectorName, audienceFlag, "https://other.example.com"},
			want:    `{"audience":"https://other.example.com"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &bodyRecorder{}
			api := connectorAPI(t, rec, map[string]stubResponse{
				"GET " + testConnectorPath: {status: http.StatusOK, body: tc.current},
				"PUT " + testConnectorPath: {status: http.StatusOK, body: tc.current},
			})

			run := runServerlessAgainst(t, api, append([]string{testConnectorsGroup, testUpdateCommand}, tc.args...)...)

			if run.err != nil {
				t.Fatal(run.err)
			}
			if got, want := rec.body(t, "PUT "+testConnectorPath), decodeJSONObject(t, tc.want); !reflect.DeepEqual(got, want) {
				t.Fatalf("body = %v\nwant %v", got, want)
			}
		})
	}
}

func TestConnectorsUpdateRefusals(t *testing.T) {
	cases := []struct {
		name    string
		current string   // the connector GET answers
		args    []string // after "connectors update"
		want    string   // a substring of the refusal
		gets    int      // lookups sent before the refusal; no update is ever sent
	}{
		{name: "no flag sends nothing", current: testAWSConnector, args: []string{testConnectorName}, want: "at least one of the flags in the group"},
		{name: "a flag of another kind is not sent", current: testGenericConnector, args: []string{testConnectorName, roleARNFlag, testNewRoleARN}, want: "--role-arn do not apply to a generic connector", gets: 1},
		{name: "clear of a missing service account on its own", current: testGCPWithoutSA, args: []string{testConnectorName, clearFlagName, clearSAField}, want: "the connector has no service account, so there is nothing to change", gets: 1},
		{name: "clear on an aws connector", current: testAWSConnector, args: []string{testConnectorName, clearFlagName, clearSAField}, want: "--clear service-account-email do not apply to a aws connector", gets: 1},
		{name: "clear on a generic connector", current: testGenericConnector, args: []string{testConnectorName, clearFlagName, clearSAField}, want: "--clear service-account-email do not apply to a generic connector", gets: 1},
		{name: "clear with its own flag", current: testGCPConnector, args: []string{testConnectorName, clearFlagName, clearSAField, serviceAccountFlag, testServiceAccount}, want: "both change service-account-email"},
		{name: "clear of an unknown field", current: testGCPConnector, args: []string{testConnectorName, clearFlagName, "audience"}, want: `--clear takes service-account-email; got "audience"`},
		{name: "clear with no field", current: testGCPConnector, args: []string{testConnectorName, clearFlagName, ""}, want: "--clear takes service-account-email; got nothing"},
		{name: "clear with no field next to another change", current: testGCPConnector, args: []string{testConnectorName, clearFlagName, "", wipFlag, testNewWIP}, want: "--clear takes service-account-email; got nothing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := connectorAPI(t, &bodyRecorder{}, map[string]stubResponse{
				"GET " + testConnectorPath: {status: http.StatusOK, body: tc.current},
			})

			run := runServerlessAgainst(t, api, append([]string{testConnectorsGroup, testUpdateCommand}, tc.args...)...)

			if run.err == nil || !strings.Contains(run.err.Error(), tc.want) {
				t.Fatalf("err = %v, want one containing %q", run.err, tc.want)
			}
			if len(run.requests) != tc.gets {
				t.Fatalf("requests = %v, want %d lookup(s) and no update", run.requests, tc.gets)
			}
			for _, r := range run.requests {
				if r.method != http.MethodGet {
					t.Fatalf("requests = %v, want no write", run.requests)
				}
			}
		})
	}
}

func TestConnectorsDeleteAndShow(t *testing.T) {
	t.Run("delete", func(t *testing.T) {
		api := connectorAPI(t, &bodyRecorder{}, map[string]stubResponse{
			"DELETE " + testConnectorPath: {status: http.StatusNoContent},
		})

		run := runServerlessAgainst(t, api, testConnectorsGroup, "delete", testConnectorName)

		if run.err != nil {
			t.Fatal(run.err)
		}
		if !strings.Contains(run.stdout, testConnectorName) {
			t.Fatalf("output does not name the deleted connector:\n%s", run.stdout)
		}
	})
	t.Run("delete of an attached connector surfaces the conflict", func(t *testing.T) {
		api := connectorAPI(t, &bodyRecorder{}, map[string]stubResponse{
			"DELETE " + testConnectorPath: {status: http.StatusConflict, body: `{"type":"about:blank","title":"Conflict","status":409,"detail":"connector is attached to 1 app"}`},
		})

		run := runServerlessAgainst(t, api, testConnectorsGroup, "delete", testConnectorName)

		if run.err == nil || !strings.Contains(run.err.Error(), "connector is attached to 1 app") {
			t.Fatalf("err = %v", run.err)
		}
		if run.stdout != "" {
			t.Fatalf("stdout = %q, want nothing on failure", run.stdout)
		}
	})
	t.Run("show of a missing connector fails", func(t *testing.T) {
		api := connectorAPI(t, &bodyRecorder{}, map[string]stubResponse{
			"GET " + testConnectorPath: {status: http.StatusNotFound, body: `{"type":"about:blank","title":"Not Found","status":404,"detail":"no connector aws-prod"}`},
		})

		run := runServerlessAgainst(t, api, testConnectorsGroup, "show", testConnectorName)

		if run.err == nil || !strings.Contains(run.err.Error(), "no connector aws-prod") {
			t.Fatalf("err = %v", run.err)
		}
	})
	t.Run("show prints the role and issuer", func(t *testing.T) {
		api := connectorAPI(t, &bodyRecorder{}, map[string]stubResponse{
			"GET " + testConnectorPath: {status: http.StatusOK, body: testAWSConnector},
		})

		run := runServerlessAgainst(t, api, testConnectorsGroup, "show", testConnectorName)

		if run.err != nil {
			t.Fatal(run.err)
		}
		for _, want := range []string{testRoleARN, "https://oidc.runware.example", testAudience} {
			if !strings.Contains(run.stdout, want) {
				t.Errorf("output missing %q:\n%s", want, run.stdout)
			}
		}
	})
}

func TestConnectorsListPaging(t *testing.T) {
	var got recordedRequest
	api := func(w http.ResponseWriter, r *http.Request) {
		got = recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.Query()}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[` + testAWSConnector + `,` + testGenericConnector + `],"nextCursor":"abc"}`))
	}

	table := runServerlessAgainst(t, api, testConnectorsGroup, connectorsListUse, "--limit", "2", "--cursor", "prev")

	if table.err != nil {
		t.Fatal(table.err)
	}
	if got.path != testConnectorsPath || got.query.Get("limit") != "2" || got.query.Get("cursor") != "prev" || got.query.Has("environment") {
		t.Fatalf("request = %v %v, want limit and cursor and no environment", got.path, got.query)
	}
	for _, want := range []string{testConnectorName, "vault", testRoleARN} {
		if !strings.Contains(table.stdout, want) {
			t.Errorf("table missing %q:\n%s", want, table.stdout)
		}
	}

	asJSON := runServerlessAgainst(t, api, "--format", formatJSON, testConnectorsGroup, connectorsListUse)
	var page struct {
		Data       []map[string]any `json:"data"`
		NextCursor string           `json:"nextCursor"`
	}
	if err := json.Unmarshal([]byte(asJSON.stdout), &page); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, asJSON.stdout)
	}
	if len(page.Data) != 2 || page.NextCursor != "abc" {
		t.Fatalf("page = %+v, want two connectors and the cursor", page)
	}
}

func TestAppsConnectorsAttachPrintsTheSubject(t *testing.T) {
	rec := &bodyRecorder{}
	var query map[string][]string
	api := func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		connectorAPI(t, rec, map[string]stubResponse{
			"POST " + testAppConnectors: {status: http.StatusCreated, body: testAttachedAWS},
		})(w, r)
	}

	run := runServerlessAgainst(t, api, testAppsGroup, testConnectorsGroup, "attach", testAppID, testConnectorName, "--env", testEnvironment)

	if run.err != nil {
		t.Fatal(run.err)
	}
	if got, want := rec.body(t, "POST "+testAppConnectors), map[string]any{"connectorName": testConnectorName}; !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(query["environment"], []string{testEnvironment}) {
		t.Fatalf("environment = %v, want [staging]", query["environment"])
	}
	for _, want := range []string{testSubject, testSubjectPattern} {
		if !strings.Contains(run.stdout, want) {
			t.Errorf("output missing %q:\n%s", want, run.stdout)
		}
	}
}

func TestAppsConnectorsListShowsEachSubjectOfItsEnvironment(t *testing.T) {
	subjects := map[string]string{
		"":              testSubject,
		testEnvironment: testStagingSubject,
	}
	api := func(w http.ResponseWriter, r *http.Request) {
		subject, ok := subjects[r.URL.Query().Get("environment")]
		if r.Method != http.MethodGet || r.URL.Path != testAppConnectors || !ok {
			t.Errorf("unexpected %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			writeNotFound(w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[` + strings.Replace(testAttachedAWS, testSubject, subject, 1) + `]}`))
	}

	for env, subject := range subjects {
		args := []string{testAppsGroup, testConnectorsGroup, connectorsListUse, testAppID}
		if env != "" {
			args = append(args, "--env", env)
		}
		run := runServerlessAgainst(t, api, args...)

		if run.err != nil {
			t.Fatalf("env %q: %v", env, run.err)
		}
		if !strings.Contains(run.stdout, subject) {
			t.Errorf("env %q: output missing %q:\n%s", env, subject, run.stdout)
		}
	}
}

func TestAppsConnectorsDetach(t *testing.T) {
	var got recordedRequest
	api := func(w http.ResponseWriter, r *http.Request) {
		got = recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.Query()}
		w.WriteHeader(http.StatusNoContent)
	}

	run := runServerlessAgainst(t, api, "--format", formatJSON, testAppsGroup, testConnectorsGroup, "detach", testAppID, testConnectorName, "--env", testEnvironment)

	if run.err != nil {
		t.Fatal(run.err)
	}
	if got.method != http.MethodDelete || got.path != testAppConnectors+"/"+testConnectorName || got.query.Get("environment") != testEnvironment {
		t.Fatalf("request = %v %v?%v", got.method, got.path, got.query)
	}
	if want := (map[string]any{"appId": testAppID, nameKey: testConnectorName}); !reflect.DeepEqual(decodeJSONObject(t, run.stdout), want) {
		t.Fatalf("stdout = %s, want %v", run.stdout, want)
	}
}

// TestConnectorCommandsPrintJSONAndYAML runs every command that prints a
// connector in both machine formats and checks the fields a script reads.
func TestConnectorCommandsPrintJSONAndYAML(t *testing.T) {
	responses := map[string]stubResponse{
		"GET " + testConnectorsPath:                             {status: http.StatusOK, body: `{"data":[` + testAWSConnector + `],"nextCursor":"abc"}`},
		"GET " + testConnectorPath:                              {status: http.StatusOK, body: testAWSConnector},
		"POST " + testConnectorsPath:                            {status: http.StatusCreated, body: testAWSConnector},
		"PUT " + testConnectorPath:                              {status: http.StatusOK, body: testAWSConnector},
		"GET " + testAppConnectors:                              {status: http.StatusOK, body: `{"data":[` + testAttachedAWS + `]}`},
		"POST " + testAppConnectors:                             {status: http.StatusCreated, body: testAttachedAWS},
		"DELETE " + testConnectorPath:                           {status: http.StatusNoContent},
		"DELETE " + testAppConnectors + "/" + testConnectorName: {status: http.StatusNoContent},
	}
	commands := []struct {
		name string
		args []string
		// list is true when the connectors are under a data key (a page) or a bare array.
		subject bool
	}{
		{name: connectorsListUse, args: []string{testConnectorsGroup, connectorsListUse}},
		{name: "show", args: []string{testConnectorsGroup, "show", testConnectorName}},
		{name: createCommand, args: []string{testConnectorsGroup, createCommand, testConnectorName, kindFlag, kindAws, roleARNFlag, testRoleARN}},
		{name: testUpdateCommand, args: []string{testConnectorsGroup, testUpdateCommand, testConnectorName, audienceFlag, testAudience}},
		{name: "apps list", args: []string{testAppsGroup, testConnectorsGroup, connectorsListUse, testAppID}, subject: true},
		{name: "apps attach", args: []string{testAppsGroup, testConnectorsGroup, "attach", testAppID, testConnectorName}, subject: true},
		{name: testDeleteCommand, args: []string{testConnectorsGroup, testDeleteCommand, testConnectorName}},
		{name: "apps detach", args: []string{testAppsGroup, testConnectorsGroup, "detach", testAppID, testConnectorName}},
	}
	for _, format := range []string{formatJSON, formatYAML} {
		for _, tc := range commands {
			t.Run(format+"/"+tc.name, func(t *testing.T) {
				run := runServerlessAgainst(t, environmentAPI(t, &bodyRecorder{}, responses), append([]string{"--format", format}, tc.args...)...)

				if run.err != nil {
					t.Fatal(run.err)
				}
				var doc any
				if format == formatJSON {
					if err := json.Unmarshal([]byte(run.stdout), &doc); err != nil {
						t.Fatalf("stdout is not JSON: %v\n%s", err, run.stdout)
					}
				} else {
					if err := yaml.Unmarshal([]byte(run.stdout), &doc); err != nil {
						t.Fatalf("stdout is not YAML: %v\n%s", err, run.stdout)
					}
					if tc.name != testDeleteCommand && tc.name != "apps detach" {
						assertYAMLEqualsJSON(t, environmentAPI(t, &bodyRecorder{}, responses), tc.args...)
					}
				}
				if tc.name == "delete" || tc.name == "apps detach" {
					return
				}
				connector := firstConnector(t, doc)
				for field, want := range map[string]any{"id": "11111111-1111-1111-1111-111111111111", nameKey: testConnectorName, "kind": kindAws, "audience": testAudience} {
					if connector[field] != want {
						t.Errorf("%s = %v, want %v", field, connector[field], want)
					}
				}
				if tc.subject && (connector["subject"] != testSubject || connector["subjectPattern"] != testSubjectPattern) {
					t.Errorf("subject = %v, subjectPattern = %v", connector["subject"], connector["subjectPattern"])
				}
			})
		}
	}
}

// firstConnector returns the connector a command printed, whether it printed
// one object, a page of them or a bare list.
func firstConnector(t *testing.T, doc any) map[string]any {
	t.Helper()
	switch v := doc.(type) {
	case map[string]any:
		if data, ok := v["data"].([]any); ok && len(data) > 0 {
			return data[0].(map[string]any)
		}
		return v
	case []any:
		if len(v) > 0 {
			return v[0].(map[string]any)
		}
	}
	t.Fatalf("no connector in %v", doc)
	return nil
}

func TestEnvironmentFlagAliasSendsTheSameQuery(t *testing.T) {
	for _, flag := range []string{"--env", "-e", "--environment"} {
		t.Run(flag, func(t *testing.T) {
			var got recordedRequest
			api := func(w http.ResponseWriter, r *http.Request) {
				got = recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.Query()}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[]}`))
			}

			run := runServerlessAgainst(t, api, "apps", testConnectorsGroup, connectorsListUse, testAppID, flag, testEnvironment)

			if run.err != nil {
				t.Fatal(run.err)
			}
			if got.query.Get("environment") != testEnvironment {
				t.Fatalf("query = %v, want environment=%s", got.query, testEnvironment)
			}
		})
	}
	t.Run("invalid name is refused like --env", func(t *testing.T) {
		run := runServerless(t, "apps", testConnectorsGroup, "list", testAppID, "--environment", "a=b")
		if run.err == nil {
			t.Fatal("want an error")
		}
	})
}

func TestConnectorsUpdateGcpProviderChangeRecomputesTheAudience(t *testing.T) {
	const newWIP = "projects/987654321/locations/global/workloadIdentityPools/other/providers/other"
	rec := &bodyRecorder{}
	api := connectorAPI(t, rec, map[string]stubResponse{
		"GET " + testConnectorPath: {status: http.StatusOK, body: testGCPConnector},
		"PUT " + testConnectorPath: {status: http.StatusOK, body: testGCPConnector},
	})

	run := runServerlessAgainst(t, api, testConnectorsGroup, testUpdateCommand, testConnectorName, wipFlag, newWIP)

	if run.err != nil {
		t.Fatal(run.err)
	}
	want := `{"audience":"//iam.googleapis.com/` + newWIP + `","gcp":{"workloadIdentityProvider":"` + newWIP + `","serviceAccountEmail":"` + testServiceAccount + `"}}`
	if got := rec.body(t, "PUT "+testConnectorPath); !reflect.DeepEqual(got, decodeJSONObject(t, want)) {
		t.Fatalf("body = %v\nwant %v", got, decodeJSONObject(t, want))
	}
}

func TestConnectorsRefuseWhatWouldChangeNothingOrFailInTheAPI(t *testing.T) {
	cases := []struct {
		name string
		args []string
		get  bool
		want string
	}{
		{name: "update with an empty role", args: []string{testUpdateCommand, testConnectorName, roleARNFlag, ""}, want: "--role-arn must not be empty"},
		{name: "update with an empty audience", args: []string{testUpdateCommand, testConnectorName, audienceFlag, ""}, want: "--audience must not be empty"},
		{name: "create with an empty audience", args: []string{createCommand, "x", kindFlag, kindAws, roleARNFlag, testRoleARN, audienceFlag, ""}, want: "--audience must not be empty"},
		{name: "create gcp with an audience", args: []string{createCommand, "x", kindFlag, kindGcp, wipFlag, testWIP, audienceFlag, "a"}, want: "--audience do not apply to a gcp connector"},
		{name: "update gcp with an audience", args: []string{testUpdateCommand, testConnectorName, audienceFlag, "a"}, get: true, want: "--audience do not apply to a gcp connector"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := connectorAPI(t, &bodyRecorder{}, map[string]stubResponse{
				"GET " + testConnectorPath: {status: http.StatusOK, body: testGCPConnector},
			})

			run := runServerlessAgainst(t, api, append([]string{testConnectorsGroup}, tc.args...)...)

			if run.err == nil || !strings.Contains(run.err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", run.err, tc.want)
			}
			for _, r := range run.requests {
				if r.method != http.MethodGet {
					t.Fatalf("requests = %v, want no write", run.requests)
				}
			}
			if !tc.get && len(run.requests) != 0 {
				t.Fatalf("requests = %v, want none", run.requests)
			}
		})
	}
}

func TestEnvFlagAndAliasTogetherAreRefused(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "destructive app command", args: []string{testAppsGroup, testDeleteCommand, testAppID, "--env", testEnvironment, "--environment", "production", "--yes"}},
		{name: "connector command", args: []string{testAppsGroup, testConnectorsGroup, connectorsListUse, testAppID, "-e", testEnvironment, "--environment", "production"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := runServerless(t, tc.args...)

			if run.err == nil || !strings.Contains(run.err.Error(), "none of the others can be") {
				t.Fatalf("err = %v, want a mutually exclusive refusal", run.err)
			}
			if len(run.requests) != 0 {
				t.Fatalf("requests = %v, want none", run.requests)
			}
		})
	}
}

func TestUsageRefusesHourGrouping(t *testing.T) {
	cases := [][]string{
		{testUsageCommand, groupByFlag, "hour"},
		{testUsageCommand, groupByFlag, app + ",hour"},
		{testAppsGroup, testUsageCommand, testAppID, groupByFlag, "hour"},
		{testAppsGroup, testUsageCommand, testAppID, groupByFlag, app + ",hour"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			run := runServerless(t, args...)

			if run.err == nil || !strings.Contains(run.err.Error(), `invalid --group-by "hour"`) {
				t.Fatalf("err = %v, want the hour refusal", run.err)
			}
			if len(run.requests) != 0 {
				t.Fatalf("requests = %v, want none", run.requests)
			}
		})
	}
	for _, dim := range []string{app, "gpuType", "day", "coverage"} {
		t.Run("still accepts "+dim, func(t *testing.T) {
			run := runServerless(t, testUsageCommand, groupByFlag, dim)

			if len(run.requests) != 1 {
				t.Fatalf("err = %v, requests = %v, want the dimension to reach the API", run.err, run.requests)
			}
		})
	}
}

func TestConnectorsRefuseForeignAndEmptyProviderFlags(t *testing.T) {
	const email = "--service-account-email"
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"create aws with a service account", []string{createCommand, "x", kindFlag, kindAws, roleARNFlag, testRoleARN, email, testServiceAccount}, email + " do not apply to a aws connector"},
		{"create gcp with a role", []string{createCommand, "x", kindFlag, kindGcp, wipFlag, testWIP, roleARNFlag, testRoleARN}, roleARNFlag + " do not apply to a gcp connector"},
		{"create generic with a provider", []string{createCommand, "x", kindFlag, kindGeneric, audienceFlag, "a", wipFlag, testWIP}, wipFlag + " do not apply to a generic connector"},
		{"create with an empty provider", []string{createCommand, "x", kindFlag, kindGcp, wipFlag, ""}, wipFlag + " must not be empty"},
		{"create with an empty service account", []string{createCommand, "x", kindFlag, kindGcp, wipFlag, testWIP, email, ""}, email + " must not be empty"},
		{"update with an empty provider", []string{testUpdateCommand, testConnectorName, wipFlag, ""}, wipFlag + " must not be empty"},
		{"update with an empty service account", []string{testUpdateCommand, testConnectorName, email, ""}, email + " must not be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := runServerless(t, append([]string{testConnectorsGroup}, tc.args...)...)

			if run.err == nil || !strings.Contains(run.err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", run.err, tc.want)
			}
			if len(run.requests) != 0 {
				t.Fatalf("requests = %v, want none", run.requests)
			}
		})
	}
}

// assertYAMLEqualsJSON runs a command in both formats against the same stub and fails
// when the YAML document is not the JSON document: same keys, same values, same shape.
func assertYAMLEqualsJSON(t *testing.T, handler http.HandlerFunc, args ...string) {
	t.Helper()
	run := func(format string) string {
		r := runServerlessAgainst(t, handler, append([]string{"--format", format}, args...)...)
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.stdout
	}
	var fromJSON, fromYAML, normalized any
	if err := json.Unmarshal([]byte(run(formatJSON)), &fromJSON); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(run(formatYAML)), &fromYAML); err != nil {
		t.Fatal(err)
	}
	// YAML reads whole numbers as int and JSON as float64: compare through JSON text.
	asJSON, err := json.Marshal(fromYAML)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(asJSON, &normalized); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromJSON, normalized) {
		t.Errorf("%v: YAML document differs from JSON\nJSON: %v\nYAML: %v", args, fromJSON, normalized)
	}
}

const testConnectorETag = `"1763337600000123"`

// etagConnectorAPI answers GET with the connector and etag (no header when etag
// is empty) and PUT with putStatus. It keeps the If-Match header of every PUT,
// and whether one was present at all.
type etagConnectorAPI struct {
	etag      string
	putStatus int
	putBody   string
	getBody   string // the connector GET answers; testAWSConnector when empty

	mu       sync.Mutex
	ifMatch  []string
	hasMatch []bool
}

func (a *etagConnectorAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		if a.etag != "" {
			w.Header().Set("ETag", a.etag)
		}
		_, _ = io.WriteString(w, cmp.Or(a.getBody, testAWSConnector))
	case http.MethodPut:
		a.mu.Lock()
		_, has := r.Header["If-Match"]
		a.hasMatch = append(a.hasMatch, has)
		a.ifMatch = append(a.ifMatch, r.Header.Get("If-Match"))
		a.mu.Unlock()
		contentType := "application/json"
		if a.putStatus >= http.StatusBadRequest {
			contentType = "application/problem+json"
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(a.putStatus)
		_, _ = io.WriteString(w, a.putBody)
	default:
		http.Error(w, "unexpected", http.StatusMethodNotAllowed)
	}
}

func TestConnectorsUpdateSendsTheETagItReadAsIfMatch(t *testing.T) {
	api := &etagConnectorAPI{etag: testConnectorETag, putStatus: http.StatusOK, putBody: testAWSConnector}

	run := runServerlessAgainst(t, api.ServeHTTP, testConnectorsGroup, testUpdateCommand, testConnectorName, roleARNFlag, testNewRoleARN)

	if run.err != nil {
		t.Fatal(run.err)
	}
	if !reflect.DeepEqual(api.ifMatch, []string{testConnectorETag}) {
		t.Fatalf("If-Match on the PUTs = %q, want exactly the GET's ETag %s", api.ifMatch, testConnectorETag)
	}
}

func TestConnectorsUpdateRefusedWhenTheConnectorChangedSinceItWasRead(t *testing.T) {
	api := &etagConnectorAPI{
		etag:      testConnectorETag,
		putStatus: http.StatusPreconditionFailed,
		putBody:   `{"type":"https://docs.runware.ai/serverless/errors#precondition-failed","title":"Precondition Failed","status":412,"detail":"the OIDC connector changed since the ETag in If-Match; read it again and retry"}`,
	}

	run := runServerlessAgainst(t, api.ServeHTTP, testConnectorsGroup, testUpdateCommand, testConnectorName, roleARNFlag, testNewRoleARN)

	want := "connector aws-prod was changed by someone else since it was read; nothing was changed. Run the command again"
	if run.err == nil || run.err.Error() != want {
		t.Fatalf("err = %v, want %q", run.err, want)
	}
	re, ok := errors.AsType[*transport.RunwareError](run.err)
	if !ok || re.StatusCode != http.StatusPreconditionFailed || re.Retryable || !strings.HasSuffix(re.ProblemType, "#precondition-failed") {
		t.Fatalf("err = %#v, want a non-retryable *transport.RunwareError for the 412 so --format json keeps the code and the precondition-failed problem type", run.err)
	}
	if run.stdout != "" {
		t.Fatalf("stdout = %q, want no connector printed", run.stdout)
	}
	if len(api.ifMatch) != 1 {
		t.Fatalf("PUTs = %d, want exactly one and no retry", len(api.ifMatch))
	}
}

func TestConnectorsUpdateSendsAWeakenedETagAsTheStrongOne(t *testing.T) {
	api := &etagConnectorAPI{etag: "W/" + testConnectorETag, putStatus: http.StatusOK, putBody: testAWSConnector}

	run := runServerlessAgainst(t, api.ServeHTTP, testConnectorsGroup, testUpdateCommand, testConnectorName, roleARNFlag, testNewRoleARN)

	if run.err != nil {
		t.Fatal(run.err)
	}
	if !reflect.DeepEqual(api.ifMatch, []string{testConnectorETag}) {
		t.Fatalf("If-Match on the PUTs = %q, want the strong tag %s a CDN weakened to W/", api.ifMatch, testConnectorETag)
	}
}

func TestConnectorsUpdateSendsNoIfMatchWhenTheAPIGaveNoETag(t *testing.T) {
	api := &etagConnectorAPI{putStatus: http.StatusOK, putBody: testAWSConnector}

	run := runServerlessAgainst(t, api.ServeHTTP, testConnectorsGroup, testUpdateCommand, testConnectorName, roleARNFlag, testNewRoleARN)

	if run.err != nil {
		t.Fatal(run.err)
	}
	if !reflect.DeepEqual(api.hasMatch, []bool{false}) {
		t.Fatalf("PUT carried an If-Match header = %v, want one PUT without it", api.hasMatch)
	}
}

func TestConnectorsUpdateKeepsTheAPIsOwnRefusalForOtherStatuses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "conflict", status: http.StatusConflict, body: `{"type":"about:blank","title":"Conflict","status":409,"detail":"connector is being deleted"}`, want: "connector is being deleted"},
		{name: "bad request", status: http.StatusBadRequest, body: `{"type":"about:blank","title":"Bad Request","status":400,"detail":"If-Match is not a valid entity-tag"}`, want: "If-Match is not a valid entity-tag"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := &etagConnectorAPI{etag: testConnectorETag, putStatus: tc.status, putBody: tc.body}

			run := runServerlessAgainst(t, api.ServeHTTP, testConnectorsGroup, testUpdateCommand, testConnectorName, roleARNFlag, testNewRoleARN)

			if run.err == nil || !strings.Contains(run.err.Error(), tc.want) {
				t.Fatalf("err = %v, want the API's own text %q", run.err, tc.want)
			}
			if strings.Contains(run.err.Error(), "changed by someone else") {
				t.Fatalf("err = %v, a %d must not read as a concurrent change", run.err, tc.status)
			}
		})
	}
}

func TestConnectorsUpdateClearSendsTheETagAsIfMatch(t *testing.T) {
	api := &etagConnectorAPI{etag: testConnectorETag, putStatus: http.StatusOK, putBody: testGCPWithoutSA}
	api.getBody = testGCPConnector

	run := runServerlessAgainst(t, api.ServeHTTP, testConnectorsGroup, testUpdateCommand, testConnectorName, clearFlagName, clearSAField)

	if run.err != nil {
		t.Fatal(run.err)
	}
	if !reflect.DeepEqual(api.ifMatch, []string{testConnectorETag}) {
		t.Fatalf("If-Match on the PUTs = %q, want exactly the GET's ETag %s", api.ifMatch, testConnectorETag)
	}
}
