package serverless

import (
	"fmt"
	"os"
	"time"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/output"
)

const (
	colID      = "ID"
	colName    = "Name"
	colStatus  = "Status"
	colCreated = "Created"
)

// deploymentResult wraps a single deployment for table/json/yaml display.
type deploymentResult serverlessapi.Deployment

func (r deploymentResult) Headers() []string {
	return []string{colID, colName, colStatus, colCreated}
}

func (r deploymentResult) Rows() [][]any {
	return [][]any{{
		r.DeploymentId,
		r.DeploymentName,
		string(r.Status),
		r.CreatedAt.Format(time.RFC3339),
	}}
}

// deploymentsResult wraps a deployment list for table display.
type deploymentsResult []serverlessapi.Deployment

func (r deploymentsResult) Headers() []string {
	return []string{colID, colName, colStatus, colCreated}
}

func (r deploymentsResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		d := &r[i]
		rows[i] = []any{
			d.DeploymentId,
			d.DeploymentName,
			string(d.Status),
			d.CreatedAt.Format(time.RFC3339),
		}
	}
	return rows
}

// endpointsResult wraps endpoint lists for table display.
type endpointsResult []serverlessapi.Endpoint

func (r endpointsResult) Headers() []string {
	return []string{"Path", colID, colCreated}
}

func (r endpointsResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		e := &r[i]
		rows[i] = []any{
			e.Path,
			e.Id.String(),
			formatOptionalTime(e.CreatedAt),
		}
	}
	return rows
}

// versionsResult wraps version lists for table display.
type versionsResult []serverlessapi.Version

func (r versionsResult) Headers() []string {
	return []string{"Version", colID, "Build", colCreated}
}

func (r versionsResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		v := &r[i]
		rows[i] = []any{
			v.VersionNumber,
			v.Id.String(),
			v.BuildId.String(),
			v.CreatedAt.Format(time.RFC3339),
		}
	}
	return rows
}

// workersResult wraps worker lists for table display.
type workersResult []serverlessapi.Worker

func (r workersResult) Headers() []string {
	return []string{colID, colStatus, "Pod", "Node", "Last Seen"}
}

func (r workersResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		w := &r[i]
		rows[i] = []any{
			w.Id.String(),
			string(w.Status),
			w.PodName,
			w.NodeName,
			formatOptionalTime(w.LastSeenAt),
		}
	}
	return rows
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

// printPage prints a cursor-paginated list. JSON/YAML use the API page shape
// (data + nextCursor). Table format renders rows, then hints at --cursor on stderr.
func printPage[T any](format output.Format, page serverlessapi.Page[T], table output.Tabular) error {
	switch format {
	case output.FormatJSON, output.FormatYAML:
		return output.Print(format, page)
	default:
		if err := output.Print(format, table); err != nil {
			return err
		}
		printNextCursor(page.NextCursor)
		return nil
	}
}

func printNextCursor(next *string) {
	if next == nil || *next == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "\nNext page: --cursor %s\n", *next)
}
