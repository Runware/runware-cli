package serverless

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
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
// next of polls (the last one repeats), except that the first refusedPolls
// polls get pollRefusal, and counts the polls.
type connectorTestStub struct {
	mu           sync.Mutex
	start        stubResponse
	polls        []string
	pollRefusal  stubResponse
	refusedPolls int
	header       http.Header
	gets         int
	query        map[string][]string
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
			if s.gets <= s.refusedPolls {
				w.Header().Set("Content-Type", testProblemJSON)
				w.WriteHeader(s.pollRefusal.status)
				_, _ = w.Write([]byte(s.pollRefusal.body))
				return
			}
			body := s.polls[min(s.gets-1-s.refusedPolls, len(s.polls)-1)]
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

// printedError is what the CLI writes to the terminal for err: for an API
// error, its message alone, so a hint kept only in err.Error() never shows.
func printedError(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("err = nil, want an error")
	}
	var text bytes.Buffer
	cmdutil.PrintErrorTo(log.New(&text), io.Discard, output.FormatTable, err)
	return text.String()
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
			want:   []string{"a test of connector " + testConnectorName + " on " + testAppID + " is already running", "run the test again", "a test of this connector is running"},
		},
		{
			// A 422 is also any request the API's schema refuses, so the CLI adds
			// no diagnosis of its own: the API's detail says what was wrong.
			name:   "unprocessable",
			status: http.StatusUnprocessableEntity,
			want:   []string{"only aws and gcp connectors can be tested"},
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
		http.StatusUnprocessableEntity: "a generic OIDC connector has no cloud to test the trust setup against; only aws and gcp connectors can be tested",
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

			printed := printedError(t, run.err)
			for _, want := range tc.want {
				if !strings.Contains(printed, want) {
					t.Errorf("printed error missing %q:\n%s", want, printed)
				}
			}
			if run.stdout != "" || stub.gets != 0 {
				t.Errorf("stdout = %q, polls = %d, want neither after a refusal", run.stdout, stub.gets)
			}
		})
	}
}

// The not-found hint's list command addresses the environment the test did.
func TestAppsConnectorsTestNotFoundHintKeepsTheEnvironment(t *testing.T) {
	stub := &connectorTestStub{
		start: stubResponse{
			status: http.StatusNotFound,
			body:   problemBody(http.StatusNotFound, "OIDC connector attachment not found"),
		},
	}

	run := runConnectorTest(t, stub, "--env", testEnvironment)

	want := "runware serverless apps connectors list " + testAppID + " --env " + testEnvironment
	if printed := printedError(t, run.err); !strings.Contains(printed, want) {
		t.Fatalf("printed error = %q, want it to suggest %q", printed, want)
	}
}

// A 422 the API's schema raised is not about the connector's kind.
func TestAppsConnectorsTestAddsNoDiagnosisToASchemaRefusal(t *testing.T) {
	const detail = "appId: string doesn't match the regular expression"
	stub := &connectorTestStub{
		start: stubResponse{
			status: http.StatusUnprocessableEntity,
			body:   problemBody(http.StatusUnprocessableEntity, detail),
		},
	}

	run := runConnectorTest(t, stub)

	printed := printedError(t, run.err)
	if !strings.Contains(printed, detail) {
		t.Fatalf("printed error = %q, want the API's detail", printed)
	}
	if strings.Contains(printed, "generic") || strings.Contains(printed, "aws and gcp") {
		t.Fatalf("printed error = %q, want no connector-kind diagnosis", printed)
	}
}

func TestAppsConnectorsTestStopsWaitingAtTheTimeout(t *testing.T) {
	stub := &connectorTestStub{
		start: pendingStart(),
		polls: []string{connectorTestBody("pending", "")},
	}

	begin := time.Now()
	run := runConnectorTest(t, stub, "--timeout", "100ms")

	printed := printedError(t, run.err)
	for _, want := range []string{"timed out waiting for the connector test after 100ms", "test " + testConnectorTestID} {
		if !strings.Contains(printed, want) {
			t.Errorf("printed error = %q, want %q", printed, want)
		}
	}
	if time.Since(begin) > 5*time.Second {
		t.Fatalf("took %s, want the command to stop at the timeout", time.Since(begin))
	}
}

// A poll the API refuses for good ends the wait; the printed error keeps the
// API's detail and names the test.
func TestAppsConnectorsTestNamesTheTestWhenAPollIsRefused(t *testing.T) {
	stub := &connectorTestStub{
		start: pendingStart(),
		pollRefusal: stubResponse{
			status: http.StatusNotFound,
			body:   problemBody(http.StatusNotFound, "OIDC connector test not found"),
		},
		refusedPolls: 1,
	}

	run := runConnectorTest(t, stub)

	printed := printedError(t, run.err)
	for _, want := range []string{"OIDC connector test not found", "test " + testConnectorTestID} {
		if !strings.Contains(printed, want) {
			t.Errorf("printed error missing %q:\n%s", want, printed)
		}
	}
	if stub.gets != 1 {
		t.Errorf("polls = %d, want 1", stub.gets)
	}
}

// A poll the server fails is retried: the test goes on whatever the API's
// availability, so a restart during the wait does not fail the command.
func TestAppsConnectorsTestRetriesAPollTheServerFailed(t *testing.T) {
	stub := &connectorTestStub{
		start: pendingStart(),
		pollRefusal: stubResponse{
			status: http.StatusServiceUnavailable,
			body:   problemBody(http.StatusServiceUnavailable, "the API is restarting"),
		},
		refusedPolls: 2,
		polls:        []string{connectorTestBody("ok", `"identity":"`+testConnectorTestRole+`"`)},
	}

	run := runConnectorTest(t, stub)

	if run.err != nil {
		t.Fatalf("err = %v, want the result after the failed polls", run.err)
	}
	if stub.gets != 3 {
		t.Errorf("polls = %d, want 3 (two refused, then ok)", stub.gets)
	}
	if !strings.Contains(run.stdout, testConnectorTestRole) {
		t.Errorf("stdout missing the identity:\n%s", run.stdout)
	}
}

// Polls the server keeps failing are retried until --timeout, and the timeout
// says what the last one answered.
func TestAppsConnectorsTestTimeoutNamesTheLastFailedPoll(t *testing.T) {
	stub := &connectorTestStub{
		start: pendingStart(),
		pollRefusal: stubResponse{
			status: http.StatusServiceUnavailable,
			body:   problemBody(http.StatusServiceUnavailable, "the API is restarting"),
		},
		refusedPolls: 1 << 20,
	}

	run := runConnectorTest(t, stub, "--timeout", "100ms")

	printed := printedError(t, run.err)
	for _, want := range []string{"timed out waiting for the connector test after 100ms", "the last poll failed: the API is restarting", "test " + testConnectorTestID} {
		if !strings.Contains(printed, want) {
			t.Errorf("printed error = %q, want %q", printed, want)
		}
	}
	if stub.gets < 2 {
		t.Errorf("polls = %d, want the failed poll retried", stub.gets)
	}
}

// A poll the API refuses for the credential is not retried.
func TestAppsConnectorsTestDoesNotRetryAnUnauthorizedPoll(t *testing.T) {
	stub := &connectorTestStub{
		start: pendingStart(),
		pollRefusal: stubResponse{
			status: http.StatusUnauthorized,
			body:   problemBody(http.StatusUnauthorized, "Missing or invalid credentials"),
		},
		refusedPolls: 1 << 20,
	}

	run := runConnectorTest(t, stub)

	if run.err == nil {
		t.Fatal("err = nil, want the refusal")
	}
	if stub.gets != 1 {
		t.Errorf("polls = %d, want 1", stub.gets)
	}
}

// An answer the client cannot decode is not retried: the next would be the same.
func TestAppsConnectorsTestDoesNotRetryAnUndecodablePoll(t *testing.T) {
	stub := &connectorTestStub{
		start: pendingStart(),
		polls: []string{"{not json"},
	}

	run := runConnectorTest(t, stub, "--timeout", "5s")

	if run.err == nil {
		t.Fatal("err = nil, want the decode failure")
	}
	if stub.gets != 1 {
		t.Errorf("polls = %d, want 1", stub.gets)
	}
}

// What the error says of a test the command stopped waiting for depends on
// its deadline: still running before it, overdue past it.
func TestConnectorTestRunningNote(t *testing.T) {
	deadline := time.Date(2026, 11, 18, 10, 2, 0, 0, time.UTC)
	test := &serverlessapi.OidcConnectorTest{
		DeadlineAt: deadline,
	}

	before := connectorTestRunningNote(test, deadline.Add(-time.Second))
	if !strings.Contains(before, "may still be running; the API ends it by 2026-11-18T10:02:00Z at the latest") {
		t.Errorf("before the deadline = %q, want it possibly running until the deadline", before)
	}
	after := connectorTestRunningNote(test, deadline.Add(30*time.Second))
	if !strings.Contains(after, "its deadline 2026-11-18T10:02:00Z has passed, so the API should have ended it") {
		t.Errorf("past the deadline = %q, want the deadline passed", after)
	}
	if strings.Contains(after, "may still be running") {
		t.Errorf("past the deadline = %q, want no claim that it still runs", after)
	}
	none := connectorTestRunningNote(&serverlessapi.OidcConnectorTest{}, deadline)
	if strings.Contains(none, "deadline") {
		t.Errorf("without a deadline = %q, want no deadline claimed", none)
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
