package serverless

import (
	"context"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/google/uuid"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

// endpointPageLimit is the per-page size deployEndpointPaths asks for: the
// contract's maximum, so the common app takes one round trip.
const endpointPageLimit = 100

// endpointComparisonBase reads what a comparison after the deploy needs: the set
// the app serves now, and the version it serves it from.
//
// Both or neither. A nil pin from a failed read is indistinguishable from an app
// that has never activated a version, and activationMoved counts the second as a
// move — so a half-captured base would make waitForSubmittedVersion return on its
// first poll, against the outgoing endpoint set, which is the silent miss this
// whole path exists to avoid.
func endpointComparisonBase(
	ctx context.Context,
	client *serverlessapi.Client,
	appID string,
) (paths []string, pin *uuid.UUID, ok bool) {
	paths, err := deployEndpointPaths(ctx, client, appID)
	if err != nil {
		return nil, nil, false
	}
	app, err := client.GetApp(ctx, appID)
	if err != nil {
		return nil, nil, false
	}
	return paths, app.ActiveVersionId, true
}

// waitForSubmittedVersion polls until the app pins a version other than previous,
// and returns the app it saw last.
//
// The app's status cannot answer this on its own. A source update on an app that
// is already active leaves it active while the new build runs, so `--wait` sees a
// terminal status immediately and the endpoint rows it would read are still the
// outgoing version's. The pin is what moves when the submitted version activates.
//
// It gives up when no activation can still arrive: the app left the states a roll
// can land in, or the roll failed, which on a live app leaves the status active
// and the pin where it was — the case that would otherwise poll forever.
func waitForSubmittedVersion(
	ctx context.Context,
	client *serverlessapi.Client,
	appID string,
	previous *uuid.UUID,
	interval time.Duration,
) (*serverlessapi.App, error) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	for {
		app, err := client.GetApp(ctx, appID)
		if err != nil {
			return nil, err
		}
		if activationMoved(previous, app.ActiveVersionId) {
			return app, nil
		}
		switch app.Status {
		case serverlessapi.AppStatusActive, serverlessapi.AppStatusInitializing:
			if buildFailed(ctx, client, appID) {
				return app, nil
			}
		default:
			return app, nil
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// activationMoved reports that the app pins a different version than it did.
// A first deploy moves from no pin at all, which counts.
func activationMoved(previous, current *uuid.UUID) bool {
	if current == nil {
		return false
	}
	return previous == nil || *previous != *current
}

// buildFailed reports that the app's newest build gave up, so no activation is
// coming. Newest first, as listBuilds returns them; an unreadable list is not a
// failure, and the poll simply continues.
func buildFailed(ctx context.Context, client *serverlessapi.Client, appID string) bool {
	page, err := client.ListBuilds(ctx, appID, nil)
	if err != nil || len(page.Data) == 0 {
		return false
	}
	return page.Data[0].Status == serverlessapi.BuildStatusFailed
}

// endpointSetChange is what a deploy did to the app's public endpoint set: the
// paths it published and the ones it retired, each sorted.
type endpointSetChange struct {
	added   []string
	removed []string
}

func (c endpointSetChange) empty() bool {
	return len(c.added) == 0 && len(c.removed) == 0
}

// deployEndpointPaths reads every live endpoint path on the app, sorted.
//
// It follows the cursor rather than trusting one page. The limit defaults to 20
// and an app may declare 20 endpoints, so a full app already sits exactly on the
// page boundary; a short read would report the endpoints it did not see as
// removed, and tell the customer their callers are about to 404 on paths that
// never moved.
func deployEndpointPaths(ctx context.Context, client *serverlessapi.Client, appID string) ([]string, error) {
	var (
		paths  []string
		cursor string
	)
	for {
		params := &serverlessapi.ListEndpointsParams{}
		params.Limit, params.Cursor = listPageParams(endpointPageLimit, cursor)
		page, err := client.ListEndpoints(ctx, appID, params)
		if err != nil {
			return nil, err
		}
		for i := range page.Data {
			paths = append(paths, page.Data[i].Path)
		}
		if page.NextCursor == nil || *page.NextCursor == "" {
			break
		}
		cursor = *page.NextCursor
	}
	slices.Sort(paths)
	return paths, nil
}

func compareEndpointSets(before, after []string) endpointSetChange {
	beforeSet := make(map[string]struct{}, len(before))
	for _, path := range before {
		beforeSet[path] = struct{}{}
	}
	afterSet := make(map[string]struct{}, len(after))
	for _, path := range after {
		afterSet[path] = struct{}{}
	}

	var change endpointSetChange
	for _, path := range after {
		if _, ok := beforeSet[path]; !ok {
			change.added = append(change.added, path)
		}
	}
	for _, path := range before {
		if _, ok := afterSet[path]; !ok {
			change.removed = append(change.removed, path)
		}
	}
	slices.Sort(change.added)
	slices.Sort(change.removed)
	return change
}

// reportEndpointSetChange writes the change, and nothing when the deploy left the
// set alone. w must be stderr: stdout carries the app record --format json
// promises, and a warning there would corrupt it.
func reportEndpointSetChange(w io.Writer, change endpointSetChange) {
	if change.empty() {
		return
	}
	var clauses []string
	if len(change.removed) > 0 {
		clauses = append(clauses, "removes "+quotedPaths(change.removed))
	}
	if len(change.added) > 0 {
		clauses = append(clauses, "adds "+quotedPaths(change.added))
	}

	message := "This deploy " + clauses[0]
	for _, clause := range clauses[1:] {
		message += " and " + clause
	}
	_, _ = fmt.Fprintf(w, "Warning: %s.\n", message)
	if len(change.removed) > 0 {
		// Source-neutral: a code app's paths come from its handler names and a
		// container app's from container.yaml, and this report covers both.
		_, _ = fmt.Fprintf(w,
			"Callers of %s will receive 404s. Restore those paths in the source if this was not intended.\n",
			quotedPaths(change.removed))
	}
}

func quotedPaths(paths []string) string {
	out := ""
	for i, path := range paths {
		if i > 0 {
			out += ", "
		}
		out += "'" + path + "'"
	}
	return out
}
