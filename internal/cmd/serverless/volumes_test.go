package serverless

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/iotest"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"gopkg.in/yaml.v3"
)

const (
	testVolumesGroup      = "volumes"
	testVolume            = "model-weights"
	testVolumesPath       = "/v1/volumes"
	testSharedVolumePath  = testVolumesPath + "/" + testVolume
	testVolumeRefsPath    = testSharedVolumePath + "/references"
	testVolumeFilesPath   = testSharedVolumePath + "/files"
	testVolumeSettings    = "/v1/volumes-settings"
	testVolumeDigest      = "9f2c4e1a7b3d5f8e0c6a2b4d1e3f5a7c9b0d2e4f6a8c1b3d5e7f9a0c2e4b6d8f"
	testOtherDigest       = "0e1d2c3b4a5f6e7d8c9b0a1f2e3d4c5b6a7f8e9d0c1b2a3f4e5d6c7b8a9f0e1d"
	testFormatJSON        = "json"
	testFormatYAML        = "yaml"
	testConfirmParam      = "confirmRollbackLoss"
	testEnvironmentParam  = "environment"
	testReferencesCommand = "references"
	testFilesCommand      = "files"
	testCreateCommand     = "create"

	testVolumeBody = `{
		"id":"6a1f0d6e-5c1b-4f69-9a39-0f4c1c8b2d11",
		"name":"model-weights",
		"environment":"production",
		"description":"SDXL weights",
		"status":"live",
		"logicalBytes":10604643268,
		"fileCount":2,
		"manifestSha256":"` + testVolumeDigest + `",
		"createdAt":"2026-11-02T10:00:00Z",
		"updatedAt":"2026-11-02T12:30:00Z"
	}`
	testSecondVolumeBody = `{
		"id":"0b7e4c2a-9d3f-4e51-8c6a-2f1d0e9b7a44",
		"name":"reference-data",
		"environment":"production",
		"status":"live",
		"logicalBytes":0,
		"fileCount":0,
		"createdAt":"2026-11-01T09:00:00Z",
		"updatedAt":"2026-11-01T09:00:00Z"
	}`
	testNoReferences                 = `{"blocking":[],"rollbackTargets":[]}`
	testTwoRollbackReferences        = `{"blocking":[],"rollbackTargets":[{"appId":"img-gen","appVersionNumber":3,"kind":"rollback-target"},{"appId":"img-gen","appVersionNumber":4,"kind":"rollback-target"}]}`
	testRetryAfterRelease            = "retry after the API release that adds it"
	testBlockingReferences           = `{"blocking":[{"appId":"img-gen","appVersionNumber":7,"kind":"current-app-version"}],"rollbackTargets":[{"appId":"img-gen","appVersionNumber":3,"kind":"rollback-target"}]}`
	testRollbackReferences           = `{"blocking":[],"rollbackTargets":[{"appId":"img-gen","appVersionNumber":3,"kind":"rollback-target"}]}`
	testSettingsEnabled              = `{"available":true,"limits":{"maxStoredBytes":1099511627776,"maxFileBytes":268435456000,"maxFilesPerVolume":100000,"maxMountedBytesPerAppVersion":536870912000,"uploadSessionTtlSeconds":86400}}`
	testSettingsDisabled             = `{"available":false,"limits":{"maxStoredBytes":1099511627776,"maxFileBytes":268435456000,"maxFilesPerVolume":100000,"maxMountedBytesPerAppVersion":268435456000,"uploadSessionTtlSeconds":86400}}`
	testNotImplemented               = `{"type":"https://docs.runware.ai/serverless/errors#not-implemented","title":"Not Implemented","status":501,"detail":"Not Implemented"}`
	testNoRoute                      = `{"type":"https://docs.runware.ai/serverless/errors#not-found","title":"Not Found","status":404,"detail":"no route matches the request path"}`
	testVolumeNotFound               = `{"type":"https://docs.runware.ai/serverless/errors#not-found","title":"Not Found","status":404,"detail":"volume not found"}`
	testVolumeInUse                  = `{"type":"https://docs.runware.ai/serverless/errors#volume-in-use","title":"Conflict","status":409,"detail":"the Volume is in use; list its references to see where"}`
	testNameTaken                    = `{"type":"https://docs.runware.ai/serverless/errors#volume-name-taken","title":"Conflict","status":409,"detail":"a Volume named 'model-weights' already exists in this environment"}`
	testRollbackConfirmationRequired = `{"type":"https://docs.runware.ai/serverless/errors#volume-rollback-confirmation-required","title":"Conflict","status":409,"detail":"older app versions would lose their rollback; retry with confirmRollbackLoss=true"}`
	testLimitIncrease                = `{"type":"https://docs.runware.ai/serverless/errors#volume-limit-exceeded","title":"Conflict","status":409,"limit":"maxStoredBytes","cap":10,"current":8,"requested":3,"requestedIsIncrement":true}`
	testLimitExceeded                = `{"type":"https://docs.runware.ai/serverless/errors#volume-limit-exceeded","title":"Conflict","status":409,"limit":"maxStoredBytes","cap":10,"requested":11,"requestedIsIncrement":false}`
)

// volumeRequest is one request the Volume stub received.
type volumeRequest struct {
	method string
	path   string
	query  url.Values
	body   string
}

// volumeAPI answers each "METHOD /path" with the next of its responses,
// repeating the last, and records every request.
type volumeAPI struct {
	t         *testing.T
	mu        sync.Mutex
	responses map[string][]stubResponse
	requests  []volumeRequest
}

// newVolumeAPI copies responses, so a table case can serve several runs.
func newVolumeAPI(t *testing.T, responses map[string][]stubResponse) *volumeAPI {
	queues := make(map[string][]stubResponse, len(responses))
	for key, queue := range responses {
		queues[key] = slices.Clone(queue)
	}
	return &volumeAPI{
		t:         t,
		responses: queues,
	}
}

func (a *volumeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		a.t.Error(err)
	}
	key := r.Method + " " + r.URL.Path
	a.mu.Lock()
	a.requests = append(a.requests, volumeRequest{
		method: r.Method,
		path:   r.URL.Path,
		query:  r.URL.Query(),
		body:   string(raw),
	})
	queue, ok := a.responses[key]
	var resp stubResponse
	if ok && len(queue) > 0 {
		resp = queue[0]
		if len(queue) > 1 {
			a.responses[key] = queue[1:]
		}
	}
	a.mu.Unlock()
	if !ok {
		a.t.Errorf("unexpected %s", key)
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

// sent returns the requests to method and path.
func (a *volumeAPI) sent(method, path string) []volumeRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []volumeRequest
	for _, r := range a.requests {
		if r.method == method && r.path == path {
			out = append(out, r)
		}
	}
	return out
}

func ok(body string) []stubResponse {
	return answer(http.StatusOK, body)
}

func answer(status int, body string) []stubResponse {
	return []stubResponse{
		{
			status: status,
			body:   body,
		},
	}
}

// volumesRun is what one volumes command did.
type volumesRun struct {
	stdout string
	stderr string
	err    error
}

// runVolumes runs `serverless volumes <args>` in format against api, with
// stdin answering prompts, and no default environment.
func runVolumes(t *testing.T, api *volumeAPI, format string, stdin io.Reader, args ...string) volumesRun {
	t.Helper()
	withoutDefaultEnvironment(t)
	srv := httptest.NewServer(api)
	defer srv.Close()
	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)

	cmd := NewCmd(log.New(io.Discard))
	cmd.PersistentFlags().String("format", format, "")
	var stderr bytes.Buffer
	cmd.SetOut(&stderr)
	cmd.SetErr(&stderr)
	cmd.SetIn(stdin)
	cmd.SetArgs(append([]string{testVolumesGroup}, args...))

	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = f
	execErr := cmd.Execute()
	os.Stdout = old
	_ = f.Close()
	out, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return volumesRun{
		stdout: string(out),
		stderr: stderr.String(),
		err:    execErr,
	}
}

func runVolumesTable(t *testing.T, api *volumeAPI, args ...string) volumesRun {
	t.Helper()
	return runVolumes(t, api, "", strings.NewReader(""), args...)
}

func requireOutput(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
}

// requireRow fails unless one line of out has every string of want, so a
// label cannot pass with another row's value.
func requireRow(t *testing.T, out string, want ...string) {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if containsAll(line, want) {
			return
		}
	}
	t.Errorf("no line has all of %q:\n%s", want, out)
}

func containsAll(s string, want []string) bool {
	for _, w := range want {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

func requireErr(t *testing.T, run volumesRun, want ...string) {
	t.Helper()
	if run.err == nil {
		t.Fatalf("err = nil, want %q; stdout:\n%s", want, run.stdout)
	}
	if strings.Contains(run.err.Error(), "\n") {
		t.Errorf("err spans several lines: %q", run.err)
	}
	for _, w := range want {
		if !strings.Contains(run.err.Error(), w) {
			t.Errorf("err = %q, want %q", run.err, w)
		}
	}
}

func TestVolumesGroupIsHiddenAndExplainsTheLifecycle(t *testing.T) {
	cmd := newVolumesCmd(log.New(io.Discard))
	if !cmd.Hidden {
		t.Fatal("volumes is visible before release")
	}
	for _, want := range []string{"volumes create", "volumes put", "deploy --mount", "additive", "latest files", "does not bring back older files", "create a new Volume", "deploy --volume"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("group help missing %q", want)
		}
	}
	lower := strings.ToLower(cmd.Long)
	for _, promise := range []string{"download", "version history", "delete a file", "delete files"} {
		if strings.Contains(lower, promise) {
			t.Errorf("group help promises %q", promise)
		}
	}
	for _, sub := range cmd.Commands() {
		if sub.Short == "" || sub.Long == "" || sub.Example == "" {
			t.Errorf("%s lacks help or an example", sub.Name())
		}
	}
}

func TestVolumesListFollowsEveryPage(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumesPath: {
			{
				status: http.StatusOK,
				body:   `{"data":[` + testVolumeBody + `],"nextCursor":"c1"}`,
			},
			{
				status: http.StatusOK,
				body:   `{"data":[` + testSecondVolumeBody + `],"nextCursor":null}`,
			},
		},
	})

	run := runVolumesTable(t, api, appHealthListCommand)

	if run.err != nil {
		t.Fatal(run.err)
	}
	requireOutput(t, run.stdout, testVolume, "reference-data", "9.9 GiB", "6a1f0d6e ")
	if strings.Contains(run.stdout, "6a1f0d6e-5c1b") {
		t.Errorf("table shows a full id:\n%s", run.stdout)
	}
	sent := api.sent(http.MethodGet, testVolumesPath)
	if len(sent) != 2 || sent[1].query["cursor"][0] != "c1" || sent[0].query["limit"][0] != "100" {
		t.Fatalf("requests = %+v, want two pages of 100", sent)
	}
}

func TestVolumesListStopsOnARepeatedCursor(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumesPath: ok(`{"data":[],"nextCursor":"c1"}`),
	})

	run := runVolumesTable(t, api, appHealthListCommand)

	if !errors.Is(run.err, errRepeatedCursor) {
		t.Fatalf("err = %v, want %v", run.err, errRepeatedCursor)
	}
}

func TestVolumesStructuredOutputIsTheAPIObject(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		responses map[string][]stubResponse
		key       string
		// items is how many entries data must hold, or 0 to skip the check.
		items int
	}{
		{
			name: appHealthListCommand,
			args: []string{appHealthListCommand},
			responses: map[string][]stubResponse{
				"GET " + testVolumesPath: {
					{
						status: http.StatusOK,
						body:   `{"data":[` + testVolumeBody + `],"nextCursor":"c1"}`,
					},
					{
						status: http.StatusOK,
						body:   `{"data":[` + testSecondVolumeBody + `]}`,
					},
				},
			},
			key:   "data",
			items: 2,
		},
		{
			name: appHealthShowCommand,
			args: []string{appHealthShowCommand, testVolume},
			responses: map[string][]stubResponse{
				"GET " + testSharedVolumePath: ok(testVolumeBody),
			},
			key: "logicalBytes",
		},
		{
			name: "references",
			args: []string{testReferencesCommand, testVolume},
			responses: map[string][]stubResponse{
				"GET " + testVolumeRefsPath: ok(testBlockingReferences),
			},
			key: "rollbackTargets",
		},
		{
			name: "files",
			args: []string{testFilesCommand, testVolume},
			responses: map[string][]stubResponse{
				"GET " + testVolumeFilesPath: ok(`{"manifestSha256":"` + testVolumeDigest + `","data":[{"path":"a.bin","mode":292,"size":10,"sha256":"` + testOtherDigest + `"}]}`),
			},
			key: "manifestSha256",
		},
		{
			name: "settings",
			args: []string{"settings"},
			responses: map[string][]stubResponse{
				"GET " + testVolumeSettings: ok(testSettingsEnabled),
			},
			key: "limits",
		},
	}
	for _, tc := range cases {
		for _, format := range []string{testFormatJSON, testFormatYAML} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				api := newVolumeAPI(t, tc.responses)

				run := runVolumes(t, api, format, strings.NewReader(""), tc.args...)

				if run.err != nil {
					t.Fatal(run.err)
				}
				var got map[string]any
				var err error
				if format == testFormatJSON {
					err = json.Unmarshal([]byte(run.stdout), &got)
				} else {
					err = yaml.Unmarshal([]byte(run.stdout), &got)
				}
				if err != nil {
					t.Fatalf("decode: %v\n%s", err, run.stdout)
				}
				if _, ok := got[tc.key]; !ok {
					t.Fatalf("%s output lacks the API field %q:\n%s", format, tc.key, run.stdout)
				}
				if tc.items > 0 {
					data, isList := got["data"].([]any)
					if !isList || len(data) != tc.items {
						t.Fatalf("%s output data = %v, want %d items:\n%s", format, got["data"], tc.items, run.stdout)
					}
				}
				if _, ok := got["nextCursor"]; ok {
					t.Fatalf("output keeps a next cursor after following every page:\n%s", run.stdout)
				}
			})
		}
	}
}

func TestVolumesShowJSONKeepsBytesAndFullIDs(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testSharedVolumePath: ok(testVolumeBody),
	})

	run := runVolumes(t, api, testFormatJSON, strings.NewReader(""), appHealthShowCommand, testVolume)

	if run.err != nil {
		t.Fatal(run.err)
	}
	got := decodeJSONObject(t, run.stdout)
	want := decodeJSONObject(t, testVolumeBody)
	if !jsonEqual(got, want) {
		t.Fatalf("json = %v\nwant %v", got, want)
	}
	if len(api.sent(http.MethodGet, testVolumeRefsPath)) != 0 {
		t.Fatal("show --format json read the references it does not print")
	}
}

func jsonEqual(a, b map[string]any) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return bytes.Equal(ra, rb)
}

func TestVolumesShowTableAddsTheUses(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testSharedVolumePath: ok(testVolumeBody),
		"GET " + testVolumeRefsPath:   ok(testBlockingReferences),
	})

	run := runVolumesTable(t, api, appHealthShowCommand, testVolume)

	if run.err != nil {
		t.Fatal(run.err)
	}
	requireOutput(t, run.stdout, testVolumeDigest, "9.9 GiB")
	requireRow(t, run.stdout, colUsedBy, "img-gen v7 (current app version)")
	requireRow(t, run.stdout, colRollbackFor, "img-gen v3")
}

func TestVolumesShowSurvivesUnimplementedReferences(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testSharedVolumePath: ok(testVolumeBody),
		"GET " + testVolumeRefsPath:   answer(http.StatusNotImplemented, testNotImplemented),
	})

	run := runVolumesTable(t, api, appHealthShowCommand, testVolume)

	if run.err != nil {
		t.Fatal(run.err)
	}
	requireOutput(t, run.stdout, testVolumeDigest, "not implemented by the API yet")
}

func TestVolumesCreateSendsNameAndDescription(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"POST " + testVolumesPath: answer(http.StatusCreated, testVolumeBody),
	})

	run := runVolumesTable(t, api, testCreateCommand, testVolume, "--description", "SDXL weights", testEnvFlag, testEnvironment)

	if run.err != nil {
		t.Fatal(run.err)
	}
	sent := api.sent(http.MethodPost, testVolumesPath)
	if len(sent) != 1 {
		t.Fatalf("requests = %+v", sent)
	}
	body := decodeJSONObject(t, sent[0].body)
	if body["name"] != testVolume || body["description"] != "SDXL weights" {
		t.Fatalf("body = %v", body)
	}
	if sent[0].query.Get(testEnvironmentParam) != testEnvironment {
		t.Fatalf("query = %v, want environment=%s", sent[0].query, testEnvironment)
	}
	requireOutput(t, run.stdout, testVolume)
}

func TestVolumesCreateLeavesOutAnUnsetDescription(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"POST " + testVolumesPath: answer(http.StatusCreated, testSecondVolumeBody),
	})

	run := runVolumesTable(t, api, testCreateCommand, "reference-data")

	if run.err != nil {
		t.Fatal(run.err)
	}
	body := decodeJSONObject(t, api.sent(http.MethodPost, testVolumesPath)[0].body)
	if _, ok := body["description"]; ok {
		t.Fatalf("body = %v, want no description", body)
	}
}

func TestVolumesRefuseANameTheAPIRejects(t *testing.T) {
	api := newVolumeAPI(t, nil)

	run := runVolumesTable(t, api, appHealthShowCommand, "Model_Weights")

	if run.err == nil || !strings.Contains(run.err.Error(), "volume name") {
		t.Fatalf("err = %v, want a name error", run.err)
	}
	if len(api.requests) != 0 {
		t.Fatalf("requests = %+v, want none", api.requests)
	}
}

func TestVolumeNamesFollowTheAPIPattern(t *testing.T) {
	longest := "a" + strings.Repeat("b", 62)
	for name, valid := range map[string]bool{
		longest:       true,
		longest + "c": false,
		"a.b_c-1":     true,
		".weights":    false,
		"-weights":    false,
		"_weights":    false,
		"":            false,
	} {
		err := volumeNameArg(nil, []string{name})
		if (err == nil) != valid {
			t.Errorf("%q: err = %v, want valid %v", name, err, valid)
		}
	}
}

func TestVolumesUpdateSetsOrClearsTheDescription(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "set",
			args: []string{"--description", "fp16"},
			want: `{"description":"fp16"}`,
		},
		{
			name: "clear",
			args: []string{testClearFlag, "description"},
			want: `{"description":null}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newVolumeAPI(t, map[string][]stubResponse{
				"PATCH " + testSharedVolumePath: ok(testVolumeBody),
			})

			run := runVolumesTable(t, api, append([]string{testUpdateCommand, testVolume}, tc.args...)...)

			if run.err != nil {
				t.Fatal(run.err)
			}
			sent := api.sent(http.MethodPatch, testSharedVolumePath)
			if len(sent) != 1 || sent[0].body != tc.want {
				t.Fatalf("requests = %+v, want body %s", sent, tc.want)
			}
		})
	}
}

func TestVolumesUpdateNeedsOneChange(t *testing.T) {
	cases := map[string][]string{
		"nothing": nil,
		"both":    {"--description", "x", testClearFlag, "description"},
		"unknown": {testClearFlag, "name"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			api := newVolumeAPI(t, nil)

			run := runVolumesTable(t, api, append([]string{testUpdateCommand, testVolume}, args...)...)

			if run.err == nil {
				t.Fatal("err = nil")
			}
			if len(api.requests) != 0 {
				t.Fatalf("requests = %+v, want none", api.requests)
			}
		})
	}
}

func TestVolumesDeleteRefusesAVolumeInUse(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeRefsPath: ok(testBlockingReferences),
	})

	run := runVolumesTable(t, api, testDeleteCommand, testVolume, testYesFlag)

	requireErr(t, run, "in use", "img-gen v7 (current app version)", "nothing was deleted")
	requireRow(t, run.stdout, "current app version", effectBlocks)
	requireRow(t, run.stdout, "rollback target", effectLosesRollback)
	if len(api.sent(http.MethodDelete, testSharedVolumePath)) != 0 {
		t.Fatal("a Volume in use was deleted")
	}
}

func TestVolumesDeleteConfirmsTheRollbackLoss(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeRefsPath:      ok(testRollbackReferences),
		"DELETE " + testSharedVolumePath: answer(http.StatusNoContent, ""),
	})

	run := runVolumes(t, api, "", terminalInput{strings.NewReader("y\n")}, testDeleteCommand, testVolume)

	if run.err != nil {
		t.Fatal(run.err)
	}
	requireOutput(t, run.stderr, "removes the rollback of img-gen v3", "Delete Volume model-weights in production?")
	requireRow(t, run.stdout, colLostRollback, "img-gen v3")
	sent := api.sent(http.MethodDelete, testSharedVolumePath)
	if len(sent) != 1 || sent[0].query.Get(testConfirmParam) != "true" {
		t.Fatalf("requests = %+v, want one confirmed delete", sent)
	}
	if reads := len(api.sent(http.MethodGet, testVolumeRefsPath)); reads != 2 {
		t.Fatalf("references read %d times, want before and after the prompt", reads)
	}
}

func TestVolumesDeleteAsksAgainWhenTheRollbackTargetsChange(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeRefsPath:      append(ok(testRollbackReferences), ok(testTwoRollbackReferences)...),
		"DELETE " + testSharedVolumePath: answer(http.StatusNoContent, ""),
	})

	// One byte per read, as a terminal hands over one line per answer.
	stdin := terminalInput{iotest.OneByteReader(strings.NewReader("y\ny\n"))}
	run := runVolumes(t, api, "", stdin, testDeleteCommand, testVolume)

	if run.err != nil {
		t.Fatal(run.err)
	}
	requireOutput(t, run.stderr, "changed while the prompt waited", "removes the rollback of img-gen v3, img-gen v4")
	if prompts := strings.Count(run.stderr, "Delete Volume model-weights"); prompts != 2 {
		t.Fatalf("prompts = %d, want 2:\n%s", prompts, run.stderr)
	}
	requireRow(t, run.stdout, colLostRollback, "img-gen v3, img-gen v4")
	if reads := len(api.sent(http.MethodGet, testVolumeRefsPath)); reads != 3 {
		t.Fatalf("references read %d times, want 3", reads)
	}
	if len(api.sent(http.MethodDelete, testSharedVolumePath)) != 1 {
		t.Fatal("want one delete after the second confirmation")
	}
}

func TestVolumesDeleteRefusesAUseThatAppearedDuringThePrompt(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeRefsPath: append(ok(testRollbackReferences), ok(testBlockingReferences)...),
	})

	run := runVolumes(t, api, "", terminalInput{strings.NewReader("y\n")}, testDeleteCommand, testVolume)

	requireErr(t, run, "in use", "img-gen v7 (current app version)", "nothing was deleted")
	if len(api.sent(http.MethodDelete, testSharedVolumePath)) != 0 {
		t.Fatal("a Volume that came into use during the prompt was deleted")
	}
}

func TestVolumesDeleteYesSkipsThePrompt(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeRefsPath:      ok(testRollbackReferences),
		"DELETE " + testSharedVolumePath: answer(http.StatusNoContent, ""),
	})

	run := runVolumes(t, api, testFormatJSON, strings.NewReader(""), testDeleteCommand, testVolume, "-y")

	if run.err != nil {
		t.Fatal(run.err)
	}
	got := decodeJSONObject(t, run.stdout)
	lost, _ := got["lostRollback"].([]any)
	if got["name"] != testVolume || len(lost) != 1 {
		t.Fatalf("json = %v, want the version that lost its rollback", got)
	}
	if reads := len(api.sent(http.MethodGet, testVolumeRefsPath)); reads != 1 {
		t.Fatalf("references read %d times, want once without a prompt", reads)
	}
}

func TestVolumesDeleteDeclinedDeletesNothing(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeRefsPath: ok(testRollbackReferences),
	})

	run := runVolumes(t, api, "", terminalInput{strings.NewReader("n\n")}, testDeleteCommand, testVolume)

	if !errors.Is(run.err, errDeleteCancelled) {
		t.Fatalf("err = %v, want %v", run.err, errDeleteCancelled)
	}
	if len(api.sent(http.MethodDelete, testSharedVolumePath)) != 0 {
		t.Fatal("a declined delete was sent")
	}
}

func TestVolumesDeleteWithoutATerminalNeedsYes(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeRefsPath: ok(testRollbackReferences),
	})

	run := runVolumesTable(t, api, testDeleteCommand, testVolume)

	if !errors.Is(run.err, errDeleteNeedsConfirm) {
		t.Fatalf("err = %v, want %v", run.err, errDeleteNeedsConfirm)
	}
	requireOutput(t, run.stderr, "removes the rollback of img-gen v3")
	if len(api.sent(http.MethodDelete, testSharedVolumePath)) != 0 {
		t.Fatal("an unconfirmed delete was sent")
	}
}

func TestVolumesDeleteWithoutATerminalStillPrintsTheUses(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeRefsPath: ok(testBlockingReferences),
	})

	run := runVolumesTable(t, api, testDeleteCommand, testVolume)

	requireErr(t, run, "in use", "nothing was deleted")
	requireRow(t, run.stdout, "current app version", effectBlocks)
	if len(api.sent(http.MethodDelete, testSharedVolumePath)) != 0 {
		t.Fatal("a Volume in use was deleted")
	}
}

func TestVolumesDeleteUnusedSendsNoConfirmation(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeRefsPath:      ok(testNoReferences),
		"DELETE " + testSharedVolumePath: answer(http.StatusNoContent, ""),
	})

	run := runVolumesTable(t, api, testDeleteCommand, testVolume, testYesFlag)

	if run.err != nil {
		t.Fatal(run.err)
	}
	sent := api.sent(http.MethodDelete, testSharedVolumePath)
	if len(sent) != 1 || sent[0].query.Has(testConfirmParam) {
		t.Fatalf("requests = %+v, want one unconfirmed delete", sent)
	}
	requireOutput(t, run.stdout, deletedLogically)
}

func TestVolumesDeleteNeverReportsARefusalAsDeleted(t *testing.T) {
	cases := []struct {
		name   string
		refs   []stubResponse
		delete []stubResponse
		want   []string
	}{
		{
			name:   "delete not implemented",
			refs:   ok(testNoReferences),
			delete: answer(http.StatusNotImplemented, testNotImplemented),
			want:   []string{"not available yet", "501", "the Volume was not deleted", testRetryAfterRelease},
		},
		{
			name: "references not implemented",
			refs: answer(http.StatusNotImplemented, testNotImplemented),
			want: []string{"not available yet", "nothing was deleted"},
		},
		{
			name:   "an older version started using it after the references were read",
			refs:   ok(testNoReferences),
			delete: answer(http.StatusConflict, testRollbackConfirmationRequired),
			want:   []string{"older app version started using", "nothing was deleted"},
		},
		{
			name:   "in use after the references were read",
			refs:   ok(testNoReferences),
			delete: answer(http.StatusConflict, testVolumeInUse),
			want:   []string{"came into use", "nothing was deleted", "volumes references model-weights"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			responses := map[string][]stubResponse{
				"GET " + testVolumeRefsPath: tc.refs,
			}
			if tc.delete != nil {
				responses["DELETE "+testSharedVolumePath] = tc.delete
			}
			api := newVolumeAPI(t, responses)

			run := runVolumesTable(t, api, testDeleteCommand, testVolume, testYesFlag)

			requireErr(t, run, tc.want...)
			if run.stdout != "" {
				t.Fatalf("stdout = %q, want nothing reported as deleted", run.stdout)
			}
		})
	}
}

func TestVolumesReferencesListsEachUse(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeRefsPath: ok(testBlockingReferences),
	})

	run := runVolumesTable(t, api, testReferencesCommand, testVolume)

	if run.err != nil {
		t.Fatal(run.err)
	}
	requireRow(t, run.stdout, "current app version", effectBlocks)
	requireRow(t, run.stdout, "rollback target", effectLosesRollback)
}

func TestVolumesFilesFollowsEveryPageOfOneFileList(t *testing.T) {
	page := func(digest, path, next string) stubResponse {
		return stubResponse{
			status: http.StatusOK,
			body:   `{"manifestSha256":"` + digest + `","data":[{"path":"` + path + `","mode":365,"size":2048,"sha256":"` + testOtherDigest + `"}],"nextCursor":` + next + `}`,
		}
	}
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeFilesPath: {
			page(testVolumeDigest, "a.bin", `"c1"`),
			// an upload completed: the listing starts again
			page(testOtherDigest, "b.bin", `null`),
			page(testOtherDigest, "a.bin", `"c1"`),
			page(testOtherDigest, "c.bin", `null`),
		},
	})

	run := runVolumes(t, api, testFormatJSON, strings.NewReader(""), testFilesCommand, testVolume)

	if run.err != nil {
		t.Fatal(run.err)
	}
	got := decodeJSONObject(t, run.stdout)
	data, _ := got["data"].([]any)
	if got["manifestSha256"] != testOtherDigest || len(data) != 2 {
		t.Fatalf("json = %v, want the two files of the newer list", got)
	}
	if sent := api.sent(http.MethodGet, testVolumeFilesPath); sent[0].query.Get("limit") != "1000" {
		t.Fatalf("limit = %v, want 1000", sent[0].query)
	}
}

func TestVolumesFilesGivesUpWhenTheFilesKeepChanging(t *testing.T) {
	var responses []stubResponse
	for i := range volumeFileListAttempts {
		digest := testVolumeDigest
		if i%2 == 1 {
			digest = testOtherDigest
		}
		responses = append(responses,
			stubResponse{
				status: http.StatusOK,
				body:   `{"manifestSha256":"` + digest + `","data":[],"nextCursor":"c1"}`,
			},
			stubResponse{
				status: http.StatusOK,
				body:   `{"manifestSha256":"changed","data":[],"nextCursor":null}`,
			},
		)
	}
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeFilesPath: responses,
	})

	run := runVolumesTable(t, api, testFilesCommand, testVolume)

	if !errors.Is(run.err, errVolumeFilesKeptChanging) {
		t.Fatalf("err = %v, want %v", run.err, errVolumeFilesKeptChanging)
	}
}

func TestVolumesFilesSaysAnEmptyVolumeHasNoFiles(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeFilesPath: ok(`{"data":[],"nextCursor":null}`),
	})

	run := runVolumesTable(t, api, testFilesCommand, testVolume)

	if run.err != nil {
		t.Fatal(run.err)
	}
	requireOutput(t, run.stderr, "has no files yet")
}

func TestVolumesFilesTableShowsHumanSizesAndModes(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeFilesPath: ok(`{"manifestSha256":"` + testVolumeDigest + `","data":[{"path":"unet/model.safetensors","mode":292,"size":10270000000,"sha256":"` + testOtherDigest + `"}]}`),
	})

	run := runVolumesTable(t, api, testFilesCommand, testVolume)

	if run.err != nil {
		t.Fatal(run.err)
	}
	requireOutput(t, run.stdout, "unet/model.safetensors", "9.6 GiB", "0444", "0e1d2c3b")
}

func TestVolumesSettingsShowsAccessAndLimits(t *testing.T) {
	api := newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumeSettings: ok(testSettingsEnabled),
	})

	run := runVolumesTable(t, api, "settings")

	if run.err != nil {
		t.Fatal(run.err)
	}
	requireRow(t, run.stdout, colAvailable, "yes")
	requireRow(t, run.stdout, colMaxStored, "1.0 TiB")
	requireRow(t, run.stdout, colMaxFileSize, "250.0 GiB")
	requireRow(t, run.stdout, colMaxFiles, "100000")
	requireRow(t, run.stdout, colMaxMounted, "500.0 GiB")
	requireRow(t, run.stdout, colUploadDeadline, "24h0m0s")
}

func TestVolumesSettingsTakesNoEnvironment(t *testing.T) {
	withDefaultEnvironment(t, testEnvironment)
	cmd := newVolumesSettingsCmd(log.New(io.Discard))
	if cmd.Flags().Lookup(environmentFlag) != nil {
		t.Fatal("settings takes --env, but settings belong to the organization")
	}
}

// volumeEnvironmentCommands are the Volume commands that address one
// environment, with the requests they send.
func volumeEnvironmentCommands() map[string][]string {
	return map[string][]string{
		appHealthListCommand:  {appHealthListCommand},
		testCreateCommand:     {testCreateCommand, testVolume},
		appHealthShowCommand:  {appHealthShowCommand, testVolume},
		testUpdateCommand:     {testUpdateCommand, testVolume, "--description", "x"},
		testDeleteCommand:     {testDeleteCommand, testVolume, testYesFlag},
		testReferencesCommand: {testReferencesCommand, testVolume},
		testFilesCommand:      {testFilesCommand, testVolume},
	}
}

func volumeHappyAPI(t *testing.T) *volumeAPI {
	return newVolumeAPI(t, map[string][]stubResponse{
		"GET " + testVolumesPath:         ok(`{"data":[]}`),
		"POST " + testVolumesPath:        answer(http.StatusCreated, testVolumeBody),
		"GET " + testSharedVolumePath:    ok(testVolumeBody),
		"PATCH " + testSharedVolumePath:  ok(testVolumeBody),
		"DELETE " + testSharedVolumePath: answer(http.StatusNoContent, ""),
		"GET " + testVolumeRefsPath:      ok(testNoReferences),
		"GET " + testVolumeFilesPath:     ok(`{"data":[]}`),
	})
}

func TestVolumesSendTheEnvironmentEveryCommandAddresses(t *testing.T) {
	for name, args := range volumeEnvironmentCommands() {
		t.Run(name+"/flag", func(t *testing.T) {
			api := volumeHappyAPI(t)

			run := runVolumesTable(t, api, append(args, testEnvFlag, testEnvironment)...)

			if run.err != nil {
				t.Fatal(run.err)
			}
			requireEveryRequestIn(t, api, testEnvironment)
		})
		t.Run(name+"/production by default", func(t *testing.T) {
			api := volumeHappyAPI(t)

			run := runVolumesTable(t, api, args...)

			if run.err != nil {
				t.Fatal(run.err)
			}
			requireEveryRequestIn(t, api, "")
		})
	}
}

func requireEveryRequestIn(t *testing.T, api *volumeAPI, environment string) {
	t.Helper()
	if len(api.requests) == 0 {
		t.Fatal("no request sent")
	}
	for _, r := range api.requests {
		got := ""
		if v := r.query[testEnvironmentParam]; len(v) > 0 {
			got = v[0]
		}
		if got != environment {
			t.Errorf("%s %s environment = %q, want %q", r.method, r.path, got, environment)
		}
	}
}

func TestVolumesUseTheDefaultEnvironment(t *testing.T) {
	api := volumeHappyAPI(t)
	srv := httptest.NewServer(api)
	defer srv.Close()
	withDefaultEnvironment(t, testEnvironment)
	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)
	cmd := NewCmd(log.New(io.Discard))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{testVolumesGroup, appHealthListCommand})
	_ = captureServerlessCommandOutput(t, cmd)

	requireEveryRequestIn(t, api, testEnvironment)
}

func decodeVolume(t *testing.T, body string) serverlessapi.Volume {
	t.Helper()
	var v serverlessapi.Volume
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestVolumesErrorsSayWhatToDoNext(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		responses map[string][]stubResponse
		want      []string
	}{
		{
			name: "beta switch off",
			args: []string{appHealthListCommand},
			responses: map[string][]stubResponse{
				"GET " + testVolumesPath:    answer(http.StatusNotFound, testNoRoute),
				"GET " + testVolumeSettings: ok(testSettingsDisabled),
			},
			want: []string{"not enabled for this organization", "volumes settings"},
		},
		{
			name: "beta switch off on create",
			args: []string{testCreateCommand, testVolume},
			responses: map[string][]stubResponse{
				"POST " + testVolumesPath:   answer(http.StatusNotFound, testNoRoute),
				"GET " + testVolumeSettings: ok(testSettingsDisabled),
			},
			want: []string{"not enabled for this organization", "nothing was created"},
		},
		{
			name: "not found",
			args: []string{appHealthShowCommand, testVolume, testEnvFlag, testEnvironment},
			responses: map[string][]stubResponse{
				"GET " + testSharedVolumePath: answer(http.StatusNotFound, testVolumeNotFound),
				"GET " + testVolumeSettings:   ok(testSettingsEnabled),
			},
			want: []string{"no Volume named model-weights in environment staging", "volumes list --env staging"},
		},
		{
			name: "create refused with 404 while enabled",
			args: []string{testCreateCommand, testVolume},
			responses: map[string][]stubResponse{
				"POST " + testVolumesPath:   answer(http.StatusNotFound, testNoRoute),
				"GET " + testVolumeSettings: ok(testSettingsEnabled),
			},
			want: []string{"no route matches the request path"},
		},
		{
			name: "settings unreadable on a 404",
			args: []string{appHealthShowCommand, testVolume},
			responses: map[string][]stubResponse{
				"GET " + testSharedVolumePath: answer(http.StatusNotFound, testVolumeNotFound),
				"GET " + testVolumeSettings:   answer(http.StatusInternalServerError, `{"title":"Internal Server Error","status":500}`),
			},
			want: []string{"volume not found"},
		},
		{
			name: "beta switch off with settings unreadable",
			args: []string{appHealthShowCommand, testVolume},
			responses: map[string][]stubResponse{
				"GET " + testSharedVolumePath: answer(http.StatusNotFound, testNoRoute),
				"GET " + testVolumeSettings:   answer(http.StatusInternalServerError, `{"title":"Internal Server Error","status":500}`),
			},
			want: []string{"no route matches the request path"},
		},
		{
			name: "quota increase exceeded",
			args: []string{testCreateCommand, testVolume},
			responses: map[string][]stubResponse{
				"POST " + testVolumesPath: answer(http.StatusConflict, testLimitIncrease),
			},
			want: []string{"maxStoredBytes", "cap 10", "current 8", "requested increase 3"},
		},
		{
			name: "not implemented",
			args: []string{testFilesCommand, testVolume},
			responses: map[string][]stubResponse{
				"GET " + testVolumeFilesPath: answer(http.StatusNotImplemented, testNotImplemented),
			},
			want: []string{"Listing a Volume's files is not available yet", "501", testRetryAfterRelease},
		},
		{
			name: "name taken",
			args: []string{testCreateCommand, testVolume},
			responses: map[string][]stubResponse{
				"POST " + testVolumesPath: answer(http.StatusConflict, testNameTaken),
			},
			want: []string{"already exists in environment production", "choose another name"},
		},
		{
			name: "quota exceeded",
			args: []string{testCreateCommand, testVolume},
			responses: map[string][]stubResponse{
				"POST " + testVolumesPath: answer(http.StatusConflict, testLimitExceeded),
			},
			want: []string{"limit is exceeded", "maxStoredBytes", "cap 10", "requested 11", "volumes settings"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newVolumeAPI(t, tc.responses)

			run := runVolumesTable(t, api, tc.args...)

			requireErr(t, run, tc.want...)
			if run.stdout != "" {
				t.Fatalf("stdout = %q, want nothing printed on a refusal", run.stdout)
			}
		})
	}
}

func TestVolumesYAMLUsesTheAPIFieldNames(t *testing.T) {
	out, err := yaml.Marshal(volumeResult{
		volume: decodeVolume(t, testVolumeBody),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"logicalBytes:", "fileCount:", "manifestSha256:", "createdAt:"} {
		if !strings.Contains(string(out), key) {
			t.Errorf("yaml lacks %q:\n%s", key, out)
		}
	}
	if slices.Contains(strings.Fields(string(out)), "logicalbytes:") {
		t.Errorf("yaml lowercased a field name:\n%s", out)
	}
}
