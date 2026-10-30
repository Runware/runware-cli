package cmd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/config"
)

const testServerlessGroup = "serverless"

func TestRootLongMatchesReadme(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	const prefix = "Run image generation"
	var blurb string
	for line := range strings.SplitSeq(string(readme), "\n") {
		if strings.HasPrefix(line, prefix) {
			blurb = line
			break
		}
	}
	if blurb == "" {
		t.Fatalf("README has no opening description starting with %q", prefix)
	}
	cmd := NewRootCmd(newLogger())
	if !strings.Contains(cmd.Long, blurb) {
		t.Fatalf("root Long drifted from README:\nLong:\n%s\nREADME:\n%s", cmd.Long, blurb)
	}
}

// TestServerlessInitHelpNamesInit pins the text that the Serverless install
// scripts in public/serverless look for. They decide whether an installed CLI
// has init from whether 'runware serverless init --help' prints
// "serverless init", because a CLI from before init prints the help of the
// serverless group and also exits 0.
func TestServerlessInitHelpNamesInit(t *testing.T) {
	cmd := NewRootCmd(newLogger())
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		testServerlessGroup,
		"init",
		"--help",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("serverless init --help: %v\n%s", err, stderr.String())
	}
	if want := "runware serverless init"; !strings.Contains(stdout.String(), want) {
		t.Errorf("stdout does not contain %q:\n%s", want, stdout.String())
	}
}

func TestRootRefusesAnInvalidDefaultEnvironmentBeforeAnyRequest(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)
	t.Setenv(config.EnvironmentFromEnvVar, "Staging")

	cmd := NewRootCmd(log.New(io.Discard))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		testServerlessGroup,
		"apps",
		"list",
	})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "RUNWARE_ENV") {
		t.Fatalf("err = %v, want the RUNWARE_ENV name refused", err)
	}
	if n := requests.Load(); n != 0 {
		t.Fatalf("sent %d requests before refusing the environment", n)
	}
}

// TestRootChecksAServerlessCommandsFlags pins that the serverless group's own
// hook runs after the root's, not instead of it.
func TestRootChecksAServerlessCommandsFlags(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)
	t.Setenv(config.EnvironmentFromEnvVar, "")
	cmd := NewRootCmd(log.New(io.Discard))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		testServerlessGroup,
		"apps",
		"list",
		"--format",
		"xml",
	})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `invalid format "xml"`) {
		t.Fatalf("err = %v, want the format refused", err)
	}
	if n := requests.Load(); n != 0 {
		t.Fatalf("sent %d requests before refusing the format", n)
	}
}

func TestRootAnnouncesTheDefaultEnvironment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_SERVERLESS_BASE_URL", srv.URL)
	t.Setenv(config.EnvironmentFromEnvVar, "staging")
	var logs bytes.Buffer

	cmd := NewRootCmd(log.New(&logs))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		testServerlessGroup,
		"apps",
		"list",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if want := "Using environment environment=staging source=RUNWARE_ENV"; !strings.Contains(logs.String(), want) {
		t.Fatalf("logs = %q, want %q", logs.String(), want)
	}
}
