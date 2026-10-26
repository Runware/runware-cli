package serverless

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/output"
)

const (
	colID             = "ID"
	colName           = "Name"
	colStatus         = "Status"
	colStatusReason   = "Status reason"
	colHealth         = "Health"
	colHealthReason   = "Health reason"
	colHealthSince    = "Health since"
	colHealthObserved = "Health observed"
	colActiveVersion  = "Active version ID"
	colVersion        = "Version"
	colCreated        = "Created"
	colUpdated        = "Updated"
	colType           = "Type"
	colField          = "Field"
	colValue          = "Value"
	colApp            = "App"
	colEnvironment    = "Environment"
	colKey            = "Key"
	colEnvVar         = "Env var"
	colError          = "Error"
	colCompleted      = "Completed"
	colTime           = "Time"
	colMessage        = "Message"
	colWorker         = "Worker"
	colEndpoint       = "Endpoint"
	colDay            = "Day"
	colCoverage       = "Coverage"
	colGPUTime        = "GPU time"
	colPAYGSpend      = "PAYG spend"
	colPAYGValue      = "PAYG-equivalent value"
	colEvent          = "Event"
	colGPUs           = "GPUs"
	colPricePerGPU    = "Price/s"
	colDefault        = "Default"
	colMaxGPUs        = "Max GPUs"
	colMonthlyBudget  = "Monthly budget"
	colBudgetAlerts   = "Budget alerts"
	colPauseAtBudget  = "Pause at budget"

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

// redactedEnvValue replaces a plaintext environment variable value in JSON and
// YAML app output. apps env-vars list is the command that prints the real value.
const redactedEnvValue = "[redacted]"

// appResult wraps a single app for table/json/yaml display.
type appResult serverlessapi.App

func (r appResult) Headers() []string {
	return []string{colField, colValue}
}

func (r appResult) Rows() [][]any {
	cfg := r.Configuration
	rows := [][]any{
		{colID, r.AppId},
		{colEnvironment, r.Environment},
		{colName, r.AppName},
		{colStatus, string(r.Status)},
		{colHealth, formatAppHealth(r.Health)},
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
	if r.Health != nil {
		rows = append(rows,
			[]any{colHealthReason, string(r.Health.Reason)},
			[]any{colHealthSince, formatOptionalTime(r.Health.Since)},
			[]any{colHealthObserved, formatOptionalTime(r.Health.ObservedAt)},
		)
	}
	return rows
}

// MarshalJSON redacts plaintext environment variable values. The table already omits
// them; JSON would otherwise print the value field from the app payload.
func (r appResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.withRedactedEnv())
}

// MarshalYAML redacts plaintext environment variable values, matching MarshalJSON.
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
	return []string{colID, colName, colStatus, colHealth, colCreated}
}

func (r appsResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		d := &r[i]
		rows[i] = []any{
			d.AppId,
			d.AppName,
			string(d.Status),
			formatAppHealth(d.Health),
			d.CreatedAt.Format(time.RFC3339),
		}
	}
	return rows
}

// An absent verdict never means healthy. Older servers and lifecycle transitions
// can omit health, so keep that uncertainty visible in table output.
func formatAppHealth(health *serverlessapi.AppHealth) string {
	if health == nil {
		return "unknown"
	}
	return string(health.State)
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
	return []string{colID, colStatus, colStatusReason, "Pod", "Node", "Last Seen"}
}

func (r workersResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		w := &r[i]
		rows[i] = []any{
			w.Id.String(),
			string(w.Status),
			formatOptionalString(w.StatusReason),
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
		{colStatusReason, formatOptionalString(r.StatusReason)},
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
		rows = append(rows, []any{"Output", formatJSONValue(r.Output)})
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

// buildDeletedResult is the success payload for deleting or canceling a build.
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

// secretResult wraps a single secret for table/json/yaml display.
// JSON/YAML serialize the API Secret (metadata only; no value).
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

// secretsResult wraps a secret list for table display.
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

// secretRemovedResult is the success payload for removing a secret.
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

const (
	// noValue stands for a setting that is not set: no cap, no budget, no alert.
	noValue = "-"
	// inheritedSuffix marks a value an environment inherits from the
	// organization defaults.
	inheritedSuffix = " (org default)"
)

// environmentResult wraps a single environment for table/json/yaml display.
type environmentResult serverlessapi.Environment

func (r environmentResult) Headers() []string {
	return []string{colField, colValue}
}

func (r environmentResult) Rows() [][]any {
	return [][]any{
		{colName, r.Name},
		{colDefault, formatYesNo(r.IsDefault)},
		{colMaxWorkers, formatEnvironmentCap(r.MaxConcurrentWorkers, r.Resolved.MaxConcurrentWorkers)},
		{colMaxGPUs, formatEnvironmentCap(r.MaxConcurrentGpus, r.Resolved.MaxConcurrentGpus)},
		{colMonthlyBudget, formatBudget(r.MonthlyBudget)},
		{colBudgetAlerts, formatAlertPercents(r.BudgetAlertPercents)},
		{colPauseAtBudget, formatYesNo(r.PauseAtBudget)},
		{colCreated, r.CreatedAt.Format(time.RFC3339)},
		{colUpdated, r.UpdatedAt.Format(time.RFC3339)},
	}
}

// environmentsResult wraps an environment list for table display.
type environmentsResult []serverlessapi.Environment

func (r environmentsResult) Headers() []string {
	return []string{colName, colDefault, colMaxWorkers, colMaxGPUs, colMonthlyBudget, colBudgetAlerts, colPauseAtBudget}
}

func (r environmentsResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		e := &r[i]
		rows[i] = []any{
			e.Name,
			formatYesNo(e.IsDefault),
			formatEnvironmentCap(e.MaxConcurrentWorkers, e.Resolved.MaxConcurrentWorkers),
			formatEnvironmentCap(e.MaxConcurrentGpus, e.Resolved.MaxConcurrentGpus),
			formatBudget(e.MonthlyBudget),
			formatAlertPercents(e.BudgetAlertPercents),
			formatYesNo(e.PauseAtBudget),
		}
	}
	return rows
}

// environmentDeletedResult is the success payload for deleting an environment.
type environmentDeletedResult struct {
	Name string `json:"name" yaml:"name"`
}

func (r environmentDeletedResult) Headers() []string {
	return []string{colName}
}

func (r environmentDeletedResult) Rows() [][]any {
	return [][]any{{r.Name}}
}

// environmentDefaultsResult wraps the organization defaults for
// table/json/yaml display.
type environmentDefaultsResult serverlessapi.EnvironmentDefaults

func (r environmentDefaultsResult) Headers() []string {
	return []string{colField, colValue}
}

func (r environmentDefaultsResult) Rows() [][]any {
	return [][]any{
		{colMaxWorkers, formatCap(r.MaxConcurrentWorkers)},
		{colMaxGPUs, formatCap(r.MaxConcurrentGpus)},
	}
}

// formatEnvironmentCap is the environment's own cap, else the organization
// default it inherits marked as such, else no cap.
func formatEnvironmentCap(own, resolved *int32) string {
	if own != nil {
		return formatCap(own)
	}
	if resolved != nil {
		return formatCap(resolved) + inheritedSuffix
	}
	return noValue
}

func formatCap(v *int32) string {
	return cmp.Or(formatOptionalInt32(v), noValue)
}

func formatBudget(amount *serverlessapi.MoneyAmount) string {
	if amount == nil {
		return noValue
	}
	return formatMoney(*amount)
}

func formatAlertPercents(percents []int) string {
	if len(percents) == 0 {
		return noValue
	}
	parts := make([]string, len(percents))
	for i, p := range percents {
		parts[i] = fmt.Sprintf("%d", p)
	}
	return strings.Join(parts, ", ") + "%"
}

func formatYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
