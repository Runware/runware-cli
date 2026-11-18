package serverless

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testConnectorTestID    = "0190a6a8-0000-7000-8000-0000000000c1"
	testConnectorTestStart = testAppConnectors + "/" + testConnectorName + "/test"
	testConnectorTestGet   = testAppConnectors + "/" + testConnectorName + "/tests/" + testConnectorTestID
	testConnectorTestRole  = "arn:aws:sts::123456789012:assumed-role/runware-worker/runware-connector-test-" + testConnectorTestID
	testPollFast           = "--poll-interval=5ms"
	testProblemJSON        = "application/problem+json"
)

func connectorTestBody(status, extra string) string {
	body := `{"id":"` + testConnectorTestID + `","status":"` + status + `","createdAt":"2026-11-18T10:00:00Z","deadlineAt":"2026-11-18T10:02:00Z"`
	if extra != "" {
		body += "," + extra
	}
	return body + "}"
}

// connectorTestStub answers a test start with start, then each poll with the
// next of polls (the last one repeats), or with pollRefusal when it has a
// status, and counts the polls.
type connectorTestStub struct {
	mu          sync.Mutex
	start       stubResponse
	polls       []string
	pollRefusal stubResponse
	header      http.Header
	gets        int
	query       map[string][]string
}

func (s *connectorTestStub) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "POST " + testConnectorTestStart:
			s.query = r.URL.Query()
			maps.Copy(w.Header(), s.header)
			status := s.start.status
			if status == http.StatusAccepted {
				w.Header().Set("Content-Type", "application/json")
			} else {
				w.Header().Set("Content-Type", testProblemJSON)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(s.start.body))
		case "GET " + testConnectorTestGet:
			s.gets++
			if s.pollRefusal.status != 0 {
				w.Header().Set("Content-Type", testProblemJSON)
				w.WriteHeader(s.pollRefusal.status)
				_, _ = w.Write([]byte(s.pollRefusal.body))
				return
			}
			body := s.polls[min(s.gets-1, len(s.polls)-1)]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			writeNotFound(w)
		}
	}
}

// pendingStart is the API's answer to a test start it accepted.
func pendingStart() stubResponse {
	return stubResponse{
		status: http.StatusAccepted,
		body:   connectorTestBody("pending", ""),
	}
}

func problemBody(status int, detail string) string {
	return fmt.Sprintf(`{"type":"about:blank","title":%q,"status":%d,"detail":%q}`, http.StatusText(status), status, detail)
}

func runConnectorTest(t *testing.T, stub *connectorTestStub, extra ...string) serverlessRun {
	t.Helper()
	args := append([]string{testAppsGroup, testConnectorsGroup, "test", testAppID, testConnectorName, testPollFast}, extra...)
	return runServerlessAgainst(t, stub.handler(t), args...)
}

func TestAppsConnectorsTestPollsUntilTheResultIsIn(t *testing.T) {
	stub := &connectorTestStub{
		start: pendingStart(),
		polls: []string{
			connectorTestBody("pending", ""),
			connectorTestBody("pending", ""),
			connectorTestBody("ok", `"identity":"`+testConnectorTestRole+`","finishedAt":"2026-11-18T10:00:09Z"`),
		},
	}

	run := runConnectorTest(t, stub, "--env", testEnvironment)

	if run.err != nil {
		t.Fatal(run.err)
	}
	if stub.gets != 3 {
		t.Fatalf("polls = %d, want 3 (two pending, then ok)", stub.gets)
	}
	if got := stub.query["environment"]; len(got) != 1 || got[0] != testEnvironment {
		t.Fatalf("environment = %v, want [%s]", got, testEnvironment)
	}
	for _, want := range []string{"ok", testConnectorTestRole, testConnectorTestID} {
		if !strings.Contains(run.stdout, want) {
			t.Errorf("output missing %q:\n%s", want, run.stdout)
		}
	}
}

type connectorTestOutcomeCase struct {
	name       string
	final      string
	wantErr    string
	wantStdout []string
}

func TestAppsConnectorsTestOutcomes(t *testing.T) {
	cases := []connectorTestOutcomeCase{
		{
			name:       "cloud error shows the code, message, stage and hint",
			final:      connectorTestBody("cloud_error", `"stage":"assume_role","code":"AccessDenied","message":"Not authorized to perform sts:AssumeRoleWithWebIdentity","troubleshooting":"The role's trust policy does not match the token's subject."`),
			wantErr:    "AccessDenied at assume_role",
			wantStdout: []string{"cloud_error", "AccessDenied", "assume_role", "Not authorized to perform sts:AssumeRoleWithWebIdentity", "trust policy does not match"},
		},
		{
			name:       "platform error shows the reason",
			final:      connectorTestBody("platform_error", `"reason":"image_pull","message":"probe image unavailable"`),
			wantErr:    "image_pull",
			wantStdout: []string{"platform_error", "image_pull", "probe image unavailable"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &connectorTestStub{
				start: pendingStart(),
				polls: []string{tc.final},
			}

			run := runConnectorTest(t, stub)

			if run.err == nil || !strings.Contains(run.err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", run.err, tc.wantErr)
			}
			for _, want := range tc.wantStdout {
				if !strings.Contains(run.stdout, want) {
					t.Errorf("stdout missing %q:\n%s", want, run.stdout)
				}
			}
		})
	}
}

// A platform error at the start needs no polling: the API has already ended the test.
func TestAppsConnectorsTestStopsAtAnAnswerThatIsAlreadyFinal(t *testing.T) {
	stub := &connectorTestStub{
		start: stubResponse{
			status: http.StatusAccepted,
			body:   connectorTestBody("platform_error", `"reason":"tenancy_not_ready"`),
		},
		polls: []string{connectorTestBody("ok", "")},
	}

	run := runConnectorTest(t, stub)

	if run.err == nil || !strings.Contains(run.err.Error(), "tenancy_not_ready") {
		t.Fatalf("err = %v, want the platform reason", run.err)
	}
	if stub.gets != 0 {
		t.Fatalf("polls = %d, want none for a test that started final", stub.gets)
	}
}

func TestAppsConnectorsTestJSONIsTheAPITest(t *testing.T) {
	stub := &connectorTestStub{
		start: pendingStart(),
		polls: []string{connectorTestBody("cloud_error", `"stage":"sts","code":"invalid_target","message":"bad audience","troubleshooting":"Check the audience."`)},
	}

	run := runConnectorTest(t, stub, "--format", formatJSON)

	if run.err == nil {
		t.Fatal("err = nil, want a failure exit for cloud_error")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(run.stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, run.stdout)
	}
	for key, want := range map[string]string{"id": testConnectorTestID, "status": "cloud_error", "stage": "sts", "code": "invalid_target", "troubleshooting": "Check the audience."} {
		if got[key] != want {
			t.Errorf("%s = %v, want %q", key, got[key], want)
		}
	}
}

type connectorTestRefusalCase struct {
	name   string
	status int
	header http.Header
	want   []string
}

func TestAppsConnectorsTestRefusals(t *testing.T) {
	cases := []connectorTestRefusalCase{
		{
			name:   "running test",
			status: http.StatusConflict,
			want:   []string{"already running", "run the test again", "a test of this connector is running"},
		},
		{
			name:   "generic connector",
			status: http.StatusUnprocessableEntity,
			want:   []string{"only aws and gcp connectors", "generic connector has no cloud", "kind generic"},
		},
		{
			name:   "not attached",
			status: http.StatusNotFound,
			want:   []string{"not found on " + testAppID, "apps connectors list " + testAppID, "connector is not attached"},
		},
		{
			name:   "rate limit",
			status: http.StatusTooManyRequests,
			header: http.Header{"Retry-After": {"754"}},
			want:   []string{"as many connector tests as an hour allows", "Retry after 12m34s", "20 OIDC connector tests"},
		},
	}
	details := map[int]string{
		http.StatusConflict:            "a test of this connector is running",
		http.StatusUnprocessableEntity: "kind generic",
		http.StatusNotFound:            "connector is not attached",
		http.StatusTooManyRequests:     "the organization started 20 OIDC connector tests in the last hour",
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &connectorTestStub{
				start: stubResponse{
					status: tc.status,
					body:   problemBody(tc.status, details[tc.status]),
				},
				header: tc.header,
				polls:  []string{connectorTestBody("ok", "")},
			}

			run := runConnectorTest(t, stub)

			if run.err == nil {
				t.Fatal("err = nil, want a refusal")
			}
			for _, want := range tc.want {
				if !strings.Contains(run.err.Error(), want) {
					t.Errorf("error missing %q:\n%s", want, run.err)
				}
			}
			if run.stdout != "" || stub.gets != 0 {
				t.Errorf("stdout = %q, polls = %d, want neither after a refusal", run.stdout, stub.gets)
			}
		})
	}
}

func TestAppsConnectorsTestStopsWaitingAtTheTimeout(t *testing.T) {
	stub := &connectorTestStub{
		start: pendingStart(),
		polls: []string{connectorTestBody("pending", "")},
	}

	begin := time.Now()
	run := runConnectorTest(t, stub, "--timeout", "100ms")

	if run.err == nil || !strings.Contains(run.err.Error(), "timed out waiting for the connector test after 100ms") {
		t.Fatalf("err = %v, want the timeout", run.err)
	}
	if !strings.Contains(run.err.Error(), "test "+testConnectorTestID+" keeps running") {
		t.Errorf("err = %v, want it to name the test that keeps running", run.err)
	}
	if time.Since(begin) > 5*time.Second {
		t.Fatalf("took %s, want the command to stop at the timeout", time.Since(begin))
	}
}

// A poll the API refuses ends the wait; the error keeps the API's detail and
// names the test, which goes on without the command.
func TestAppsConnectorsTestNamesTheTestWhenAPollFails(t *testing.T) {
	stub := &connectorTestStub{
		start: pendingStart(),
		pollRefusal: stubResponse{
			status: http.StatusServiceUnavailable,
			body:   problemBody(http.StatusServiceUnavailable, "the API is restarting"),
		},
	}

	run := runConnectorTest(t, stub)

	if run.err == nil {
		t.Fatal("err = nil, want the failed poll")
	}
	for _, want := range []string{"the API is restarting", "test " + testConnectorTestID + " keeps running"} {
		if !strings.Contains(run.err.Error(), want) {
			t.Errorf("error missing %q:\n%s", want, run.err)
		}
	}
	if stub.gets != 1 {
		t.Errorf("polls = %d, want 1", stub.gets)
	}
}

func TestAppsConnectorsTestRejectsANonPositiveTimeout(t *testing.T) {
	run := runConnectorTest(t, &connectorTestStub{}, "--timeout", "0s")

	if run.err == nil || !strings.Contains(run.err.Error(), "--timeout must be positive") {
		t.Fatalf("err = %v, want the timeout refused", run.err)
	}
	if len(run.requests) != 0 {
		t.Fatalf("requests = %v, want none", run.requests)
	}
}
