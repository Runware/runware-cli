package serverless

import (
	"context"
	"fmt"
	"io"
	"slices"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

// endpointSetChange is what a deploy did to the app's public endpoint set:
// the paths it published, and the paths it retired. Both sorted.
//
// A code app's endpoint path is its handler's method name with underscores
// turned into hyphens, so renaming a method is an ordinary local refactor that
// moves a public URL and 404s the customer's callers. Reporting the change is
// the signal that says so; nothing here refuses the deploy.
type endpointSetChange struct {
	added   []string
	removed []string
}

func (c endpointSetChange) empty() bool {
	return len(c.added) == 0 && len(c.removed) == 0
}

// deployEndpointPaths reads the app's live endpoint paths, sorted.
//
// One page is the whole set: an app may declare at most 20 endpoints and the
// list route pages at 100, so there is no cursor to follow. A miss is not an
// error to the caller — an app that does not exist yet, or one whose first
// version has not deployed, simply has no endpoints to compare against.
func deployEndpointPaths(ctx context.Context, client *serverlessapi.Client, appID string) ([]string, error) {
	page, err := client.ListEndpoints(ctx, appID, nil)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(page.Data))
	for i := range page.Data {
		paths = append(paths, page.Data[i].Path)
	}
	slices.Sort(paths)
	return paths, nil
}

// compareEndpointSets reports what moved between two readings of the set.
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

// reportEndpointSetChange writes the change to w, and nothing at all when the
// deploy left the set alone — most deploys change code behind an unchanged set,
// and a line on every one of those would bury the deploy that moves a URL.
//
// To stderr, never stdout: stdout carries the machine-readable app record that
// --format json promises, and a warning there would corrupt it.
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
