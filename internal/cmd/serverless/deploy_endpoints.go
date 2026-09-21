package serverless

import (
	"context"
	"fmt"
	"io"
	"slices"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

// endpointPageLimit is the per-page size deployEndpointPaths asks for: the
// contract's maximum, so the common app takes one round trip.
const endpointPageLimit = 100

// endpointSetChange is what a deploy did to the app's public endpoint set: the
// paths it published and the ones it retired, each sorted.
type endpointSetChange struct {
	added   []string
	removed []string
}

func (c endpointSetChange) empty() bool {
	return len(c.added) == 0 && len(c.removed) == 0
}

// shouldReportEndpointChange guards the two ways the comparison would lie:
// without a reading from before the deploy the app's whole existing set looks
// new, and before the rollout activates the rows are still the old version's.
func shouldReportEndpointChange(readBefore bool, status serverlessapi.AppStatus) bool {
	return readBefore && status == serverlessapi.AppStatusActive
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
		_, _ = fmt.Fprintf(w,
			"Callers of %s will receive 404s. Rename the handler back if this was not intended.\n",
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
