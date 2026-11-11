package serverless

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

const otherVersionID = "019c7654-8b21-7abc-9123-cccccccccccc"

func TestAppsShowServingVersion(t *testing.T) {
	readyOld := workerJSON("44444444-4444-4444-4444-444444444441", oldVersionID, "ready")
	busyOld := workerJSON("44444444-4444-4444-4444-444444444442", oldVersionID, "busy")
	readyNew := workerJSON("44444444-4444-4444-4444-444444444443", newVersionID, "ready")
	loadingOther := workerJSON("44444444-4444-4444-4444-444444444444", otherVersionID, "loading")

	cases := []struct {
		name    string
		pages   map[string]string
		want    string
		format  string
		workers bool
		fail    bool
		cancel  bool
	}{
		{
			name:    "one version that is not the rollout target",
			pages:   map[string]string{"": workersPage("", readyOld, busyOld)},
			want:    oldVersionID,
			workers: true,
		},
		{
			name:    "ready and busy on different versions",
			pages:   map[string]string{"": workersPage("", readyOld, readyNew)},
			want:    servingVersionMixed,
			workers: true,
		},
		{
			name:    "a loading worker is not serving",
			pages:   map[string]string{"": workersPage("", readyOld, loadingOther)},
			want:    oldVersionID,
			workers: true,
		},
		{
			name:    "no workers",
			pages:   map[string]string{"": workersPage("")},
			want:    noValue,
			workers: true,
		},
		{
			name: "second page brings another version",
			pages: map[string]string{
				"":            workersPage(testLogCursor, readyOld),
				testLogCursor: workersPage("", readyNew),
			},
			want:    servingVersionMixed,
			workers: true,
		},
		{
			name: "second page is the same version",
			pages: map[string]string{
				"":            workersPage(testLogCursor, readyOld),
				testLogCursor: workersPage("", busyOld),
			},
			want:    oldVersionID,
			workers: true,
		},
		{
			name: "five pages still open is unknown",
			pages: map[string]string{
				"":   workersPage("p2", readyOld),
				"p2": workersPage("p3", busyOld),
				"p3": workersPage("p4", readyOld),
				"p4": workersPage("p5", busyOld),
				"p5": workersPage("p6", readyOld),
			},
			want:    servingVersionUnknown,
			workers: true,
		},
		{
			name:    "a failed worker list",
			fail:    true,
			want:    servingVersionUnknown,
			workers: true,
		},
		{
			name:   "json does not list workers",
			format: "json",
		},
		{
			name:   "yaml does not list workers",
			format: "yaml",
		},
		{
			name:    "cancelled while listing workers",
			cancel:  true,
			workers: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sawWorkers bool
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == testAppsPath+"/"+testAppID+"/workers" {
					sawWorkers = true
					if !tc.workers {
						t.Errorf("workers list was not expected")
					}
					q := r.URL.Query()
					if q.Get("versionId") != "all" || q.Get("state") != string(serverlessapi.WorkerStateFilterLive) || q.Get("status") != "" {
						t.Errorf("query = %s, want versionId=all state=live and no status", q.Encode())
					}
					if tc.cancel {
						cancel()
						return
					}
					if tc.fail {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					body, ok := tc.pages[q.Get("cursor")]
					if !ok {
						t.Errorf("no page for cursor %q", q.Get("cursor"))
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, body)
					return
				}
				if r.Method != http.MethodGet || r.URL.Path != testAppsPath+"/"+testAppID {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, activeAppBody(newVersionID))
			}))
			t.Cleanup(srv.Close)
			t.Setenv("RUNWARE_API_KEY", "test-key")
			t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)

			format := tc.format
			if format == "" {
				format = testTableFormat
			}
			out, err := runAppsShow(ctx, t, format)
			if tc.cancel {
				if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
					t.Fatalf("err = %v, want the cancelled context", err)
				}
				if strings.Contains(out, colServingVersion) {
					t.Fatalf("stdout = %q, want no table after cancellation", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("show: %v", err)
			}
			if sawWorkers != tc.workers {
				t.Fatalf("workers called = %v, want %v", sawWorkers, tc.workers)
			}
			if format != testTableFormat {
				if strings.Contains(out, colServingVersion) {
					t.Fatalf("stdout = %q, want no serving row", out)
				}
				return
			}
			if !strings.Contains(out, colServingVersion) || !strings.Contains(out, tc.want) {
				t.Fatalf("stdout = %q, want %s %s", out, colServingVersion, tc.want)
			}
		})
	}
}

func workerJSON(id, version, status string) string {
	return fmt.Sprintf(
		`{"id":%q,"appId":%q,"status":%q,"podName":"worker-0","versionId":%q,"gpuCount":1,"createdAt":"2026-09-30T12:00:00Z","statusOccurredAt":"2026-09-30T12:01:00Z"}`,
		id, testAppID, status, version,
	)
}

func workersPage(cursor string, workers ...string) string {
	body := `{"data":[` + strings.Join(workers, ",") + `]`
	if cursor != "" {
		body += `,"nextCursor":` + fmt.Sprintf("%q", cursor)
	}
	return body + `}`
}

func runAppsShow(ctx context.Context, t *testing.T, format string) (string, error) {
	t.Helper()
	cmd := newAppsShowCmd(log.New(io.Discard))
	cmd.SetArgs([]string{testAppID})
	cmd.SetContext(ctx)
	cmd.PersistentFlags().String("format", format, "")
	cmd.SetErr(io.Discard)

	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	old := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = old }()
	runErr := cmd.Execute()
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(b), runErr
}
