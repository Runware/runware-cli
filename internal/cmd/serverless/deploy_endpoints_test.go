package serverless

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

func TestCompareEndpointSets(t *testing.T) {
	cases := []struct {
		name        string
		before      []string
		after       []string
		wantAdded   []string
		wantRemoved []string
	}{
		{
			name:   "an unchanged set moves nothing",
			before: []string{testOtherEndpointPath, testEndpointPath},
			after:  []string{testOtherEndpointPath, testEndpointPath},
		},
		{
			// The rename this whole report exists for: run_upscale became
			// upscale_image, so the public path moved with it.
			name:        "a rename is a removal and an addition",
			before:      []string{testOtherEndpointPath, "run-upscale"},
			after:       []string{testOtherEndpointPath, "upscale-image"},
			wantAdded:   []string{"upscale-image"},
			wantRemoved: []string{"run-upscale"},
		},
		{
			name:      "a first deploy adds everything",
			after:     []string{testEndpointPath, testOtherEndpointPath},
			wantAdded: []string{testOtherEndpointPath, testEndpointPath},
		},
		{
			name:        "a handler deleted outright",
			before:      []string{testOtherEndpointPath, testEndpointPath},
			after:       []string{testOtherEndpointPath},
			wantRemoved: []string{testEndpointPath},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			change := compareEndpointSets(tc.before, tc.after)
			if !slices.Equal(change.added, tc.wantAdded) {
				t.Errorf("added = %v, want %v", change.added, tc.wantAdded)
			}
			if !slices.Equal(change.removed, tc.wantRemoved) {
				t.Errorf("removed = %v, want %v", change.removed, tc.wantRemoved)
			}
			if got := change.empty(); got != (len(tc.wantAdded) == 0 && len(tc.wantRemoved) == 0) {
				t.Errorf("empty() = %v, disagrees with the reported change", got)
			}
		})
	}
}

// TestReportEndpointSetChangeNamesTheBreak: the removal is the half that 404s
// the customer's callers, so it leads and it is called out on its own line.
func TestReportEndpointSetChangeNamesTheBreak(t *testing.T) {
	var out bytes.Buffer
	reportEndpointSetChange(&out, endpointSetChange{
		added:   []string{"upscale-image"},
		removed: []string{"run-upscale"},
	})

	got := out.String()
	for _, want := range []string{
		"This deploy removes 'run-upscale' and adds 'upscale-image'.",
		"Callers of 'run-upscale' will receive 404s",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report = %q, want it to contain %q", got, want)
		}
	}
}

// TestReportEndpointSetChangeAdditionOnly: publishing a new endpoint breaks
// nobody, so it is reported without the 404 warning.
func TestReportEndpointSetChangeAdditionOnly(t *testing.T) {
	var out bytes.Buffer
	reportEndpointSetChange(&out, endpointSetChange{added: []string{testOtherEndpointPath}})

	got := out.String()
	if !strings.Contains(got, "This deploy adds 'embed'.") {
		t.Errorf("report = %q, want the addition", got)
	}
	if strings.Contains(got, "404") {
		t.Errorf("report = %q, warns about callers when nothing was removed", got)
	}
}

// TestReportEndpointSetChangeSaysNothingWhenNothingMoved keeps the ordinary
// deploy quiet: most deploys change code behind an unchanged endpoint set.
func TestReportEndpointSetChangeSaysNothingWhenNothingMoved(t *testing.T) {
	var out bytes.Buffer
	reportEndpointSetChange(&out, endpointSetChange{})
	if out.Len() != 0 {
		t.Errorf("report = %q, want nothing for an unchanged set", out.String())
	}
}

// TestShouldReportEndpointChange guards the two ways this report can lie: with
// no reading from before the deploy it would call the app's whole existing set
// new, and before the rollout activates it would compare the old set to itself.
func TestShouldReportEndpointChange(t *testing.T) {
	cases := []struct {
		name       string
		readBefore bool
		status     serverlessapi.AppStatus
		want       bool
	}{
		{
			name:       "read before and rolled out",
			readBefore: true,
			status:     serverlessapi.AppStatusActive,
			want:       true,
		},
		{
			name:       "the before-read failed, so the whole set would look new",
			readBefore: false,
			status:     serverlessapi.AppStatusActive,
		},
		{
			name:       "the deploy failed, so nothing moved",
			readBefore: true,
			status:     serverlessapi.AppStatusFailed,
		},
		{
			name:       "still rolling out, so the rows are still the old version's",
			readBefore: true,
			status:     serverlessapi.AppStatusInitializing,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldReportEndpointChange(tc.readBefore, tc.status); got != tc.want {
				t.Errorf("shouldReportEndpointChange(%v, %q) = %v, want %v",
					tc.readBefore, tc.status, got, tc.want)
			}
		})
	}
}

func TestDeployEndpointPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Returned out of order, to prove the reading is sorted before it is diffed.
		_, _ = w.Write([]byte(`{"data":[
			{"id":"019c7654-8b21-7abc-9123-abcdef123456","appId":"my-app","path":"generate"},
			{"id":"019c7654-8b21-7abc-9123-abcdef123457","appId":"my-app","path":"embed"}
		]}`))
	}))
	defer server.Close()

	client := serverlessapi.NewClient("test-key", server.URL, slog.Default())
	paths, err := deployEndpointPaths(context.Background(), client, testAppID)
	if err != nil {
		t.Fatalf("deployEndpointPaths: %v", err)
	}
	if !slices.Equal(paths, []string{testOtherEndpointPath, testEndpointPath}) {
		t.Errorf("paths = %v, want [embed generate]", paths)
	}
}

// TestDeployEndpointPathsSurfacesTheError: the caller decides a failed read
// costs the warning rather than the deploy, so this must not swallow it here.
func TestDeployEndpointPathsSurfacesTheError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := serverlessapi.NewClient("test-key", server.URL, slog.Default())
	if _, err := deployEndpointPaths(context.Background(), client, testAppID); err == nil {
		t.Fatal("expected an error for 500")
	}
}
