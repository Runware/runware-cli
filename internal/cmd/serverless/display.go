package serverless

import (
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/output"
)

const (
	colID      = "ID"
	colName    = "Name"
	colStatus  = "Status"
	colCreated = "Created"
	colUpdated = "Updated"
	colType    = "Type"
	colField   = "Field"
	colValue   = "Value"
	colApp     = "App"
	colKey     = "Key"
	colEnvVar  = "Env var"
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
			formatOptionalUUID(v.BuildId),
			v.CreatedAt.Format(time.RFC3339),
		}
	}
	return rows
}

// versionResult wraps a single version for table/json/yaml display.
type versionResult serverlessapi.Version

func (r versionResult) Headers() []string {
	return []string{colField, colValue}
}

func (r versionResult) Rows() [][]any {
	return [][]any{
		{"Version", r.VersionNumber},
		{colID, r.Id.String()},
		{colApp, r.DeploymentId},
		{"Build", formatOptionalUUID(r.BuildId)},
		{colCreated, r.CreatedAt.Format(time.RFC3339)},
	}
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
			formatOptionalString(w.NodeName),
			formatOptionalTime(w.LastSeenAt),
		}
	}
	return rows
}

func formatOptionalInt32(v *int32) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%d", *v)
}

// buildsResult wraps build lists for table display. Log tail is omitted.
type buildsResult []serverlessapi.Build

func (r buildsResult) Headers() []string {
	return []string{colID, colStatus, "Error", colCreated}
}

func (r buildsResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		b := &r[i]
		rows[i] = []any{
			b.Id.String(),
			string(b.Status),
			formatOptionalString(b.Error),
			formatOptionalTime(b.CreatedAt),
		}
	}
	return rows
}

// buildResult wraps a single build for table/json/yaml display.
// Table omits logTail; printBuild appends it as a block in table format.
type buildResult serverlessapi.Build

func (r buildResult) Headers() []string {
	return []string{colField, colValue}
}

func (r buildResult) Rows() [][]any {
	return [][]any{
		{colID, r.Id.String()},
		{colStatus, string(r.Status)},
		{"Error", formatOptionalString(r.Error)},
		{"Exit code", formatOptionalInt32(r.ExitCode)},
		{colCreated, formatOptionalTime(r.CreatedAt)},
	}
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

func formatOptionalString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func formatOptionalUUID(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// secretResult wraps a single organisation secret for table/json/yaml display.
// JSON/YAML serialise the API Secret (metadata only; no value).
type secretResult serverlessapi.Secret

func (r secretResult) Headers() []string {
	return []string{colName, colType, colCreated}
}

func (r secretResult) Rows() [][]any {
	return [][]any{{
		r.Name,
		string(r.Type),
		formatOptionalTime(r.CreatedAt),
	}}
}

// secretsResult wraps an organisation secret list for table display.
type secretsResult []serverlessapi.Secret

func (r secretsResult) Headers() []string {
	return []string{colName, colType, colCreated}
}

func (r secretsResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		s := &r[i]
		rows[i] = []any{
			s.Name,
			string(s.Type),
			formatOptionalTime(s.CreatedAt),
		}
	}
	return rows
}

// secretAttachmentsResult wraps deployment secret attachments for table display.
type secretAttachmentsResult []serverlessapi.SecretAttachment

func (r secretAttachmentsResult) Headers() []string {
	return []string{colName, colEnvVar, colType, colCreated}
}

func (r secretAttachmentsResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		s := &r[i]
		rows[i] = []any{
			s.Name,
			formatOptionalString(s.EnvVarName),
			string(s.Type),
			formatOptionalTime(s.CreatedAt),
		}
	}
	return rows
}

// secretRemovedResult is the success payload for removing an organisation secret.
type secretRemovedResult struct {
	Name string `json:"name" yaml:"name"`
}

func (r secretRemovedResult) Headers() []string {
	return []string{colName}
}

func (r secretRemovedResult) Rows() [][]any {
	return [][]any{{r.Name}}
}

// secretAttachResult is the success payload for attaching a secret to an application.
type secretAttachResult struct {
	DeploymentID string `json:"deploymentId" yaml:"deploymentId"`
	Name         string `json:"name"         yaml:"name"`
	EnvVarName   string `json:"envVarName,omitempty" yaml:"envVarName,omitempty"`
}

func (r secretAttachResult) Headers() []string {
	return []string{colApp, colName, colEnvVar}
}

func (r secretAttachResult) Rows() [][]any {
	return [][]any{{r.DeploymentID, r.Name, r.EnvVarName}}
}

// secretDetachResult is the success payload for detaching a secret from an application.
type secretDetachResult struct {
	DeploymentID string `json:"deploymentId" yaml:"deploymentId"`
	Name         string `json:"name"         yaml:"name"`
}

func (r secretDetachResult) Headers() []string {
	return []string{colApp, colName}
}

func (r secretDetachResult) Rows() [][]any {
	return [][]any{{r.DeploymentID, r.Name}}
}

// envVarResult wraps a single plain-text environment variable for table/json/yaml display.
type envVarResult serverlessapi.EnvironmentVariable

func (r envVarResult) Headers() []string {
	return []string{colKey, colValue, colCreated, colUpdated}
}

func (r envVarResult) Rows() [][]any {
	return [][]any{{
		r.Key,
		r.Value,
		formatOptionalTime(r.CreatedAt),
		formatOptionalTime(r.UpdatedAt),
	}}
}

// envVarsResult wraps environment variable lists for table display.
type envVarsResult []serverlessapi.EnvironmentVariable

func (r envVarsResult) Headers() []string {
	return []string{colKey, colValue, colCreated, colUpdated}
}

func (r envVarsResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		e := &r[i]
		rows[i] = []any{
			e.Key,
			e.Value,
			formatOptionalTime(e.CreatedAt),
			formatOptionalTime(e.UpdatedAt),
		}
	}
	return rows
}

// envUnsetResult is the success payload for removing an environment variable.
type envUnsetResult struct {
	DeploymentID string `json:"deploymentId" yaml:"deploymentId"`
	Key          string `json:"key"          yaml:"key"`
}

func (r envUnsetResult) Headers() []string {
	return []string{colApp, colKey}
}

func (r envUnsetResult) Rows() [][]any {
	return [][]any{{r.DeploymentID, r.Key}}
}

// printPage prints a cursor-paginated list. JSON/YAML use the API page shape
// (data + nextCursor). Table format renders rows, then hints at --cursor on errOut
// (typically cmd.ErrOrStderr()).
func printPage[T any](format output.Format, page serverlessapi.Page[T], table output.Tabular, errOut io.Writer, extraCursorFlags string) error {
	switch format {
	case output.FormatJSON, output.FormatYAML:
		return output.Print(format, page)
	default:
		if err := output.Print(format, table); err != nil {
			return err
		}
		return printNextCursor(errOut, page.NextCursor, extraCursorFlags)
	}
}

func printNextCursor(errOut io.Writer, next *string, extraFlags string) error {
	if next == nil || *next == "" {
		return nil
	}
	hint := "--cursor " + *next
	if extraFlags != "" {
		hint = extraFlags + " " + hint
	}
	_, err := fmt.Fprintf(errOut, "\nNext page: %s\n", hint)
	return err
}
