package model

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/spf13/cobra"
)

const emptySearchReply = `{"data":[{"taskType":"modelSearch","results":[],"totalResults":0}]}`

// searchAPI records every task the CLI sends and answers each request with no results.
type searchAPI struct {
	mu    sync.Mutex
	tasks []map[string]any
}

func (s *searchAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var tasks []map[string]any
	if err := json.NewDecoder(r.Body).Decode(&tasks); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.tasks = append(s.tasks, tasks...)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, emptySearchReply)
}

func (s *searchAPI) sent() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.tasks)
}

// runSearch runs `model search` with args against the API at apiURL over HTTP,
// discarding what the command prints.
func runSearch(t *testing.T, apiURL string, args ...string) error {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNWARE_API_KEY", "test-key")
	t.Setenv("RUNWARE_BASE_URL", apiURL)

	root := &cobra.Command{Use: "runware"}
	root.PersistentFlags().String("format", "json", "")
	root.PersistentFlags().String("transport", transport.SchemeHTTP, "")
	root.AddCommand(newSearchCmd(log.New(io.Discard)))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs(append([]string{"search"}, args...))

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close() //nolint:errcheck
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = devNull, devNull
	defer func() {
		os.Stdout, os.Stderr = stdout, stderr
	}()
	return root.Execute()
}

func TestSearchRefusesAShortSearchBeforeAnyRequest(t *testing.T) {
	fake := &searchAPI{}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	err := runSearch(t, srv.URL, "--search", "f")

	if err == nil || !strings.Contains(err.Error(), "--search") {
		t.Fatalf("err = %v, want the short search refused", err)
	}
	if n := len(fake.sent()); n != 0 {
		t.Fatalf("sent %d tasks before refusing the search", n)
	}
}

func TestSearchSendsSourceSortAndCapabilities(t *testing.T) {
	fake := &searchAPI{}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	err := runSearch(t, srv.URL,
		"--source", "official",
		"--sort", "-name",
		"--capabilities", "io:text-to-image",
		"--capabilities", "op:inpaint",
	)
	if err != nil {
		t.Fatalf("model search: %v", err)
	}

	tasks := fake.sent()
	if len(tasks) != 1 {
		t.Fatalf("sent %d tasks, want 1", len(tasks))
	}
	task := tasks[0]
	if task["taskType"] != "modelSearch" {
		t.Errorf("taskType = %v, want modelSearch", task["taskType"])
	}
	if task["source"] != "official" {
		t.Errorf("source = %v, want official", task["source"])
	}
	if task["sort"] != "-name" {
		t.Errorf("sort = %v, want -name", task["sort"])
	}
	capabilities, _ := task["capabilities"].([]any)
	if want := []any{"io:text-to-image", "op:inpaint"}; !slices.Equal(capabilities, want) {
		t.Errorf("capabilities = %v, want %v", capabilities, want)
	}
}

func TestValidateSearch(t *testing.T) {
	tests := []struct {
		name    string
		search  string
		wantErr bool
	}{
		{name: "empty", search: "", wantErr: false},
		{name: "one character", search: "f", wantErr: true},
		{name: "two characters", search: "fl", wantErr: false},
		{name: "48 characters", search: strings.Repeat("a", 48), wantErr: false},
		{name: "49 characters", search: strings.Repeat("a", 49), wantErr: true},
		{name: "one multibyte character", search: "é", wantErr: true},
		{name: "48 multibyte characters", search: strings.Repeat("é", 48), wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSearch(tt.search)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSearch(%q) error = %v, wantErr %v", tt.search, err, tt.wantErr)
			}
		})
	}
}

func TestModelSearchRowsIncludeSource(t *testing.T) {
	for _, wide := range []bool{false, true} {
		r := modelSearchResults{
			models: []api.ModelResult{
				{
					Name:     "PhotoRealistic XL",
					AIR:      "civitai:249421@281440",
					Source:   "community",
					Category: "checkpoint",
				},
			},
			wide: wide,
		}
		headers := r.Headers()
		i := slices.Index(headers, colSource)
		if i < 0 {
			t.Fatalf("wide=%v: expected a %q column, got %v", wide, colSource, headers)
		}
		if got := r.Rows()[0][i]; got != "community" {
			t.Errorf("wide=%v: expected source community, got %v", wide, got)
		}
	}
}
