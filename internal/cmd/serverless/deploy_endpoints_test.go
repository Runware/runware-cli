package serverless

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

// activeAppBody renders a getApp response for an active app pinning
// activeVersionId, or pinning none when it is empty.
func activeAppBody(activeVersionID string) string {
	pin := "null"
	if activeVersionID != "" {
		pin = fmt.Sprintf("%q", activeVersionID)
	}
	return fmt.Sprintf(`{
		"appId":"my-app","appName":"My App","status":"active","activeVersionId":%s,
		"configuration":{"maxWorkers":1,"idleTtlSecs":60,"scalingDelaySecs":10,"minWorkers":0,"gpusPerWorker":1,"gracefulStopTtlSecs":120,"computeType":"gpu"},
		"environmentVariables":[],"secrets":[],
		"createdAt":"2026-07-30T12:00:00Z","updatedAt":"2026-07-30T12:00:00Z"
	}`, pin)
}

const (
	oldVersionID = "019c7654-8b21-7abc-9123-aaaaaaaaaaaa"
	newVersionID = "019c7654-8b21-7abc-9123-bbbbbbbbbbbb"
)

// TestEndpointComparisonBaseNeedsBothReads: a nil pin from a failed read looks
// exactly like an app that has never activated a version, and activationMoved
// counts that as a move — so half a base would send the comparison straight at
// the outgoing endpoint set rather than waiting for the new one.
func TestEndpointComparisonBaseNeedsBothReads(t *testing.T) {
	cases := []struct {
		name    string
		appFail bool
		epFail  bool
	}{
		{name: "both succeed"},
		{name: "the app read fails", appFail: true},
		{name: "the endpoint read fails", epFail: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				endpoints := strings.Contains(r.URL.Path, "/endpoints")
				if (endpoints && tc.epFail) || (!endpoints && tc.appFail) {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if endpoints {
					_, _ = w.Write([]byte(`{"data":[{"id":"019c7654-8b21-7abc-9123-abcdef123456","appId":"my-app","path":"generate"}]}`))
					return
				}
				_, _ = w.Write([]byte(activeAppBody(oldVersionID)))
			}))
			defer server.Close()

			client := serverlessapi.NewClient("test-key", server.URL, slog.Default())
			paths, pin, ok := endpointComparisonBase(context.Background(), client, testAppID)
			wantOK := !tc.appFail && !tc.epFail
			if ok != wantOK {
				t.Fatalf("ok = %v, want %v", ok, wantOK)
			}
			if !ok {
				if paths != nil || pin != nil {
					t.Errorf("half a base escaped: paths=%v pin=%v", paths, pin)
				}
				return
			}
			if pin == nil || pin.String() != oldVersionID {
				t.Errorf("pin = %v, want the version the app serves now", pin)
			}
			if !slices.Equal(paths, []string{testEndpointPath}) {
				t.Errorf("paths = %v, want the live set", paths)
			}
		})
	}
}

// TestWaitForSubmittedVersionWaitsThroughAnActiveBuild is the defect this guard
// exists for: a source update on an app that is already active answers `active`
// while its new build runs, so a reader keyed on status alone compares the
// outgoing version's endpoint rows and reports that nothing moved.
func TestWaitForSubmittedVersionWaitsThroughAnActiveBuild(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/builds") {
			_, _ = w.Write([]byte(`{"data":[{"id":"019c7654-8b21-7abc-9123-abcdef123456","appId":"my-app","status":"building","phases":[]}]}`))
			return
		}
		calls++
		// Still rolling the first two times, pinned the third.
		if calls < 3 {
			_, _ = w.Write([]byte(activeAppBody(oldVersionID)))
			return
		}
		_, _ = w.Write([]byte(activeAppBody(newVersionID)))
	}))
	defer server.Close()

	previous := uuid.MustParse(oldVersionID)
	client := serverlessapi.NewClient("test-key", server.URL, slog.Default())
	app, err := waitForSubmittedVersion(context.Background(), client, testAppID, &previous, time.Millisecond)
	if err != nil {
		t.Fatalf("waitForSubmittedVersion: %v", err)
	}
	if app.ActiveVersionId == nil || app.ActiveVersionId.String() != newVersionID {
		t.Errorf("pinned version = %v, want the submitted one", app.ActiveVersionId)
	}
	if calls < 3 {
		t.Errorf("getApp calls = %d, want it to have kept polling past the unchanged pin", calls)
	}
}

// TestWaitForSubmittedVersionGivesUpOnAFailedBuild: a roll that fails on a live
// app leaves it active on the version that kept serving, so the pin never moves
// and nothing but the build says the wait is over.
func TestWaitForSubmittedVersionGivesUpOnAFailedBuild(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/builds") {
			_, _ = w.Write([]byte(`{"data":[{"id":"019c7654-8b21-7abc-9123-abcdef123456","appId":"my-app","status":"failed","phases":[]}]}`))
			return
		}
		_, _ = w.Write([]byte(activeAppBody(oldVersionID)))
	}))
	defer server.Close()

	previous := uuid.MustParse(oldVersionID)
	client := serverlessapi.NewClient("test-key", server.URL, slog.Default())
	done := make(chan struct{})
	go func() {
		defer close(done)
		app, err := waitForSubmittedVersion(context.Background(), client, testAppID, &previous, time.Millisecond)
		if err != nil {
			t.Errorf("waitForSubmittedVersion: %v", err)
			return
		}
		if activationMoved(&previous, app.ActiveVersionId) {
			t.Errorf("reported an activation for a failed build")
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("waitForSubmittedVersion did not give up on a failed build")
	}
}

func TestActivationMoved(t *testing.T) {
	oldID := uuid.MustParse(oldVersionID)
	newID := uuid.MustParse(newVersionID)
	cases := []struct {
		name              string
		previous, current *uuid.UUID
		want              bool
	}{
		{name: "pin unchanged", previous: &oldID, current: &oldID},
		{name: "pin moved", previous: &oldID, current: &newID, want: true},
		{name: "first ever activation", current: &newID, want: true},
		{name: "nothing pinned yet", previous: &oldID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := activationMoved(tc.previous, tc.current); got != tc.want {
				t.Errorf("activationMoved() = %v, want %v", got, tc.want)
			}
		})
	}
}

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

// TestDeployEndpointPathsFollowsTheCursor: an app may declare as many endpoints
// as a default page holds, so a reader that stops at the first page would call
// the endpoints it never saw removed and warn about 404s that are not coming.
func TestDeployEndpointPathsFollowsTheCursor(t *testing.T) {
	var gotCursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCursors = append(gotCursors, r.URL.Query().Get("cursor"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "" {
			_, _ = w.Write([]byte(`{"data":[
				{"id":"019c7654-8b21-7abc-9123-abcdef123456","appId":"my-app","path":"generate"}
			],"nextCursor":"page2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[
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
		t.Errorf("paths = %v, want both pages", paths)
	}
	if !slices.Equal(gotCursors, []string{"", "page2"}) {
		t.Errorf("cursors requested = %v, want the second page to be fetched once", gotCursors)
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

func TestReportDeployEndpoints_SaysNothingWhenThereAreNoPaths(t *testing.T) {
	var out bytes.Buffer
	reportDeployEndpoints(&out, testAppID, nil)
	if out.Len() != 0 {
		t.Fatalf("empty set wrote %q", out.String())
	}
}

func TestReportDeployEndpoints_PrintsPathsAndAnInvokeExample(t *testing.T) {
	var out bytes.Buffer
	reportDeployEndpoints(&out, testAppID, []string{testEndpointPath, testOtherEndpointPath})
	got := out.String()
	if !strings.Contains(got, "'"+testEndpointPath+"'") || !strings.Contains(got, "'"+testOtherEndpointPath+"'") {
		t.Fatalf("paths missing: %q", got)
	}
	wantInvoke := "Invoke: runware serverless apps invoke " + testAppID + " " + testEndpointPath + " -f payload.json\n"
	if !strings.Contains(got, wantInvoke) {
		t.Fatalf("invoke example missing: %q", got)
	}
}
