package serverless

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/output"
)

const (
	colID            = "ID"
	colName          = "Name"
	colStatus        = "Status"
	colActiveVersion = "Active version ID"
	colVersion       = "Version"
	colCreated       = "Created"
	colUpdated       = "Updated"
	colType          = "Type"
	colField         = "Field"
	colValue         = "Value"
	colApp           = "App"
	colKey           = "Key"
	colEnvVar        = "Env var"
	colError         = "Error"
	colCompleted     = "Completed"
	colTime          = "Time"
	colMessage       = "Message"
	colWorker        = "Worker"
	colEndpoint      = "Endpoint"
	colDay           = "Day"
	colCoverage      = "Coverage"
	colGPUTime       = "GPU time"
	colPAYGSpend     = "PAYG spend"
	colPAYGValue     = "PAYG-equivalent value"

	colComputeType         = "Compute type"
	colGPUType             = "GPU type"
	colFallbackGPUType     = "Fallback GPU type"
	colGPUsPerWorker       = "GPUs per worker"
	colMinWorkers          = "Min workers"
	colMaxWorkers          = "Max workers"
	colMinAvailableWorkers = "Min available workers"
	colAvailableWorkersPct = "Available workers %"
	colIdleTTL             = "Idle TTL (s)"
	colScalingDelay        = "Scaling delay (s)"
	colEffectiveMaxWorkers = "Effective max workers"
	colActiveWorkers       = "Active workers"
	colQueueDepth          = "Queue depth"
	colRequests24h         = "Requests (24h)"
	colReqPerMin           = "Req/min"
	colP95                 = "p95 (s)"
	colP99                 = "p99 (s)"
)

// redactedEnvValue replaces a plaintext environment value in JSON and YAML
// app output. apps env list is the command that prints the real value.
const redactedEnvValue = "[redacted]"

// appResult wraps a single app for table/json/yaml display.
type appResult serverlessapi.App

func (r appResult) Headers() []string {
	return []string{colField, colValue}
}

func (r appResult) Rows() [][]any {
	cfg := r.Configuration
	return [][]any{
		{colID, r.AppId},
		{colName, r.AppName},
		{colStatus, string(r.Status)},
		{colActiveVersion, formatOptionalUUID(r.ActiveVersionId)},
		{colCreated, r.CreatedAt.Format(time.RFC3339)},
		{colUpdated, r.UpdatedAt.Format(time.RFC3339)},
		{colComputeType, string(cfg.ComputeType)},
		{colGPUType, formatOptionalString(cfg.GpuType)},
		{colFallbackGPUType, formatOptionalString(cfg.FallbackGpuType)},
		{colGPUsPerWorker, cfg.GpusPerWorker},
		{colMinWorkers, cfg.MinWorkers},
		{colMaxWorkers, cfg.MaxWorkers},
		{colMinAvailableWorkers, formatOptionalInt32(cfg.MinAvailableWorkers)},
		{colAvailableWorkersPct, formatOptionalInt32(cfg.AvailableWorkersPct)},
		{colIdleTTL, cfg.IdleTtlSecs},
		{colScalingDelay, cfg.ScalingDelaySecs},
		{colEffectiveMaxWorkers, formatOptionalInt32(r.EffectiveMaxWorkers)},
		{colActiveWorkers, r.Runtime.ActiveWorkers},
		{colQueueDepth, formatOptionalInt64(r.Runtime.QueueDepth)},
		{colRequests24h, formatOptionalInt64(r.Runtime.Requests24h)},
	}
}

// MarshalJSON redacts plaintext environment values. The table already omits
// them; JSON would otherwise print the value field from the app payload.
func (r appResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.withRedactedEnv())
}

// MarshalYAML redacts plaintext environment values, matching MarshalJSON.
func (r appResult) MarshalYAML() (any, error) {
	return r.withRedactedEnv(), nil
}

func (r appResult) withRedactedEnv() serverlessapi.App {
	app := serverlessapi.App(r)
	if len(app.EnvironmentVariables) == 0 {
		return app
	}
	env := make([]serverlessapi.EnvironmentVariable, len(app.EnvironmentVariables))
	copy(env, app.EnvironmentVariables)
	for i := range env {
		env[i].Value = redactedEnvValue
	}
	app.EnvironmentVariables = env
	return app
}

// appsResult wraps an app list for table display.
type appsResult []serverlessapi.App

func (r appsResult) Headers() []string {
	return []string{colID, colName, colStatus, colCreated}
}

func (r appsResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		d := &r[i]
		rows[i] = []any{
			d.AppId,
			d.AppName,
			string(d.Status),
			d.CreatedAt.Format(time.RFC3339),
		}
	}
	return rows
}

// endpointsResult wraps endpoint lists for table display.
type endpointsResult []serverlessapi.Endpoint

func (r endpointsResult) Headers() []string {
	return []string{"Path", colID, colCreated, colStatus, colReqPerMin, colP95, colP99}
}

func (r endpointsResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		e := &r[i]
		rows[i] = []any{
			e.Path,
			e.Id.String(),
			formatOptionalTime(e.CreatedAt),
			formatEndpointStatus(e.Runtime),
			formatEndpointRate(e.Runtime),
			formatEndpointP95(e.Runtime),
			formatEndpointP99(e.Runtime),
		}
	}
	return rows
}

// endpointResult wraps a single endpoint for table/json/yaml display.
type endpointResult serverlessapi.Endpoint

func (r endpointResult) Headers() []string {
	return []string{colField, colValue}
}

func (r endpointResult) Rows() [][]any {
	return [][]any{
		{"Path", r.Path},
		{colID, r.Id.String()},
		{colApp, r.AppId},
		{colCreated, formatOptionalTime(r.CreatedAt)},
		{colUpdated, formatOptionalTime(r.UpdatedAt)},
		{colStatus, formatEndpointStatus(r.Runtime)},
		{colReqPerMin, formatEndpointRate(r.Runtime)},
		{colP95, formatEndpointP95(r.Runtime)},
		{colP99, formatEndpointP99(r.Runtime)},
	}
}

// versionsResult wraps version lists for table display.
type versionsResult []serverlessapi.Version

func (r versionsResult) Headers() []string {
	return []string{colVersion, colID, "Build", colCreated}
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
		{colVersion, r.VersionNumber},
		{colID, r.Id.String()},
		{colApp, r.AppId},
		{"Build", formatOptionalUUID(r.BuildId)},
		{colCreated, r.CreatedAt.Format(time.RFC3339)},
	}
}

// versionDeletedResult is the success payload for deleting a version.
type versionDeletedResult struct {
	AppID         string `json:"appId" yaml:"appId"`
	VersionNumber int32  `json:"versionNumber" yaml:"versionNumber"`
}

func (r versionDeletedResult) Headers() []string {
	return []string{colApp, colVersion}
}

func (r versionDeletedResult) Rows() [][]any {
	return [][]any{{r.AppID, r.VersionNumber}}
}

// eventsResult wraps app event lists for table display.
type eventsResult []serverlessapi.AppEvent

func (r eventsResult) Headers() []string {
	return []string{colTime, colType, colMessage, colWorker, colEndpoint}
}

func (r eventsResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		ev := &r[i]
		rows[i] = []any{
			formatOptionalTime(ev.CreatedAt),
			string(ev.Type),
			ev.Message,
			formatOptionalUUID(ev.WorkerId),
			formatOptionalUUID(ev.EndpointId),
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
			formatOptionalString(w.NodeName),
			formatOptionalTime(w.LastSeenAt),
		}
	}
	return rows
}

// workerResult wraps a single worker for table/json/yaml display.
type workerResult serverlessapi.Worker

func (r workerResult) Headers() []string {
	return []string{colField, colValue}
}

func (r workerResult) Rows() [][]any {
	return [][]any{
		{colID, r.Id.String()},
		{colApp, r.AppId},
		{colStatus, string(r.Status)},
		{"Pod", r.PodName},
		{"Node", formatOptionalString(r.NodeName)},
		{colGPUType, formatOptionalString(r.GpuType)},
		{"GPU count", r.GpuCount},
		{"Version ID", r.VersionId.String()},
		{"Last seen", formatOptionalTime(r.LastSeenAt)},
		{colCreated, r.CreatedAt.Format(time.RFC3339)},
		{"Status occurred", r.StatusOccurredAt.Format(time.RFC3339)},
		{"Status reason", formatOptionalString(r.StatusReason)},
	}
}

// tasksResult wraps task lists for table display. Output is omitted.
type tasksResult []serverlessapi.Task

func (r tasksResult) Headers() []string {
	return []string{colID, colStatus, colEndpoint, colError, colCreated, colCompleted}
}

func (r tasksResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		task := &r[i]
		rows[i] = []any{
			task.Id,
			string(task.Status),
			task.EndpointPath,
			formatOptionalString(task.Error),
			formatTaskTime(task.CreatedAt),
			formatOptionalTime(task.CompletedAt),
		}
	}
	return rows
}

// taskResult wraps a single task for table/json/yaml display.
type taskResult serverlessapi.Task

func (r taskResult) Headers() []string {
	return []string{colField, colValue}
}

func (r taskResult) Rows() [][]any {
	rows := [][]any{
		{colID, r.Id},
		{colApp, r.AppId},
		{colStatus, string(r.Status)},
		{colEndpoint, r.EndpointPath},
		{colCreated, formatTaskTime(r.CreatedAt)},
		{colCompleted, formatOptionalTime(r.CompletedAt)},
		{colError, formatOptionalString(r.Error)},
	}
	if r.Output != nil {
		rows = append(rows, []any{"Output", formatJSONValue(*r.Output)})
	}
	return rows
}

func formatTaskTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func formatJSONValue(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func formatOptionalInt32(v *int32) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%d", *v)
}

func formatOptionalInt64(v *int64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%d", *v)
}

func formatOptionalFloat64(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", *v)
}

func formatEndpointStatus(rt *serverlessapi.EndpointRuntime) string {
	if rt == nil || rt.Status == nil {
		return ""
	}
	return string(*rt.Status)
}

func formatEndpointRate(rt *serverlessapi.EndpointRuntime) string {
	if rt == nil {
		return ""
	}
	return formatOptionalFloat64(rt.RequestsPerMinute)
}

func formatEndpointP95(rt *serverlessapi.EndpointRuntime) string {
	if rt == nil {
		return ""
	}
	return formatOptionalFloat64(rt.P95RequestDuration)
}

func formatEndpointP99(rt *serverlessapi.EndpointRuntime) string {
	if rt == nil {
		return ""
	}
	return formatOptionalFloat64(rt.P99RequestDuration)
}

// buildsResult wraps build lists for table display. Log tail is omitted.
type buildsResult []serverlessapi.Build

func (r buildsResult) Headers() []string {
	return []string{colID, colStatus, colError, colCreated}
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
		{colError, formatOptionalString(r.Error)},
		{"Exit code", formatOptionalInt32(r.ExitCode)},
		{colCreated, formatOptionalTime(r.CreatedAt)},
	}
}

// buildDeletedResult is the success payload for deleting or cancelling a build.
type buildDeletedResult struct {
	AppID   string `json:"appId" yaml:"appId"`
	BuildID string `json:"buildId" yaml:"buildId"`
}

func (r buildDeletedResult) Headers() []string {
	return []string{colApp, colID}
}

func (r buildDeletedResult) Rows() [][]any {
	return [][]any{{r.AppID, r.BuildID}}
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

// secretAttachmentsResult wraps app secret attachments for table display.
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
	AppID      string `json:"appId" yaml:"appId"`
	Name       string `json:"name"         yaml:"name"`
	EnvVarName string `json:"envVarName,omitempty" yaml:"envVarName,omitempty"`
}

func (r secretAttachResult) Headers() []string {
	return []string{colApp, colName, colEnvVar}
}

func (r secretAttachResult) Rows() [][]any {
	return [][]any{{r.AppID, r.Name, r.EnvVarName}}
}

// secretDetachResult is the success payload for detaching a secret from an application.
type secretDetachResult struct {
	AppID string `json:"appId" yaml:"appId"`
	Name  string `json:"name"  yaml:"name"`
}

func (r secretDetachResult) Headers() []string {
	return []string{colApp, colName}
}

func (r secretDetachResult) Rows() [][]any {
	return [][]any{{r.AppID, r.Name}}
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
	AppID string `json:"appId" yaml:"appId"`
	Key   string `json:"key"   yaml:"key"`
}

func (r envUnsetResult) Headers() []string {
	return []string{colApp, colKey}
}

func (r envUnsetResult) Rows() [][]any {
	return [][]any{{r.AppID, r.Key}}
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
	return printNamedCursor(errOut, "Next page", next, extraFlags)
}

func printNamedCursor(errOut io.Writer, label string, cursor *string, extraFlags string) error {
	if cursor == nil || *cursor == "" {
		return nil
	}
	hint := "--cursor " + *cursor
	if extraFlags != "" {
		hint = extraFlags + " " + hint
	}
	_, err := fmt.Fprintf(errOut, "\n%s: %s\n", label, hint)
	return err
}
