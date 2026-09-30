package serverless

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const appHealthListCommand = "list"

const appHealthActiveStatus = "active"

type appHealthCase struct {
	name   string
	state  string
	reason string
}

// Exercise the public command boundary with raw server responses: a missing
// generated field silently disappears even when the backend reports a failure.
func TestAppsHealthFromAPI(t *testing.T) {
	cases := []appHealthCase{
		{
			name:   "min zero cannot serve demand",
			state:  "unavailable",
			reason: "demand_unserved",
		},
		{
			name:   "idle min zero is healthy",
			state:  "healthy",
			reason: "workload_present",
		},
		{
			name:   "partial capacity",
			state:  "degraded",
			reason: "capacity_below_floor",
		},
		{
			name: "verdict absent",
		},
	}
	for _, tc := range cases {
		for _, command := range []string{"show", appHealthListCommand} {
			for _, format := range []string{"table", "json", "yaml"} {
				t.Run(tc.name+"/"+command+"/"+format, func(t *testing.T) {
					body := appHealthResponse(tc.state, tc.reason)
					if command == appHealthListCommand {
						body = `{"data":[` + body + `]}`
					}
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						path := "/v1/apps"
						if command == "show" {
							path += "/" + testAppID
						}
						if r.Method != http.MethodGet || r.URL.Path != path {
							t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
							w.WriteHeader(http.StatusNotFound)
							return
						}
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, body)
					}))
					defer srv.Close()
					t.Setenv("RUNWARE_API_KEY", "test-key")
					t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)
					out := runAppHealthCommand(t, command, format)
					assertAppHealthOutput(t, out, command, format, tc.state, tc.reason)
				})
			}
		}
	}
}

func TestAppsHealthRecovery(t *testing.T) {
	state, reason := "unavailable", "demand_unserved"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, appHealthResponse(state, reason))
	}))
	defer srv.Close()
	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)
	assertAppHealthOutput(t, runAppHealthCommand(t, "show", "json"), "show", "json", state, reason)
	state, reason = "healthy", "workload_present"
	assertAppHealthOutput(t, runAppHealthCommand(t, "show", "json"), "show", "json", state, reason)
}

func appHealthResponse(state, reason string) string {
	base := activeAppBody("")
	if state == "" {
		return base
	}
	return strings.Replace(base, `"status":"active"`, `"status":"active","health":{"state":"`+state+`","reason":"`+reason+`","since":"2026-09-28T16:13:14Z","observedAt":"2026-09-28T16:13:30Z"}`, 1)
}

func runAppHealthCommand(t *testing.T, command, format string) string {
	t.Helper()
	var cmd *cobra.Command
	if command == appHealthListCommand {
		cmd = newAppsListCmd(log.New(io.Discard))
	} else {
		cmd = newAppsShowCmd(log.New(io.Discard))
		cmd.SetArgs([]string{testAppID})
	}
	cmd.PersistentFlags().String("format", format, "")
	cmd.SetErr(io.Discard)
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("close stdout: %v", err)
		}
	}()
	old := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = old }()
	if err := cmd.Execute(); err != nil {
		t.Fatalf("%s: %v", command, err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertAppHealthOutput(t *testing.T, out, command, format, state, reason string) {
	t.Helper()
	if format == "table" {
		if state == "" {
			state = "unknown"
		}
		for _, text := range []string{"health", appHealthActiveStatus, state, reason} {
			if !strings.Contains(strings.ToLower(out), text) {
				t.Fatalf("table missing %q: %s", text, out)
			}
		}
		if command == "show" && reason != "" {
			for _, stamp := range []string{"2026-09-28T16:13:14Z", "2026-09-28T16:13:30Z"} {
				if !strings.Contains(out, stamp) {
					t.Fatalf("table missing %q: %s", stamp, out)
				}
			}
		}
		return
	}
	var app map[string]any
	var err error
	if format == "json" {
		err = json.Unmarshal([]byte(out), &app)
	} else {
		err = yaml.Unmarshal([]byte(out), &app)
	}
	if err != nil {
		t.Fatalf("decode %s: %v: %s", format, err, out)
	}
	if command == appHealthListCommand {
		data, ok := app["data"].([]any)
		if !ok || len(data) != 1 {
			t.Fatalf("unexpected page: %s", out)
		}
		app, ok = data[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected app: %s", out)
		}
	}
	health, ok := app["health"].(map[string]any)
	if state == "" {
		if ok {
			t.Fatalf("absent verdict became a health object: %s", out)
		}
		return
	}
	if !ok || health["state"] != state || health["reason"] != reason {
		t.Fatalf("health lost or changed: %s", out)
	}
	observedKey := "observedAt"
	if format == "yaml" {
		observedKey = "observedat"
	}
	stamps := map[string]string{
		"since":     "2026-09-28T16:13:14Z",
		observedKey: "2026-09-28T16:13:30Z",
	}
	for key, want := range stamps {
		got := health[key]
		if stamp, ok := got.(time.Time); ok {
			got = stamp.Format(time.RFC3339)
		}
		if got != want {
			t.Fatalf("health %s = %v, want %s: %s", key, got, want, out)
		}
	}
}
