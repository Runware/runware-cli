package transport

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// ErrorCode is a stable category for a RunwareError. Use it for switch/if statements.
type ErrorCode string

const (
	CodeValidation  ErrorCode = "validation"
	CodeAuth        ErrorCode = "auth"
	CodeQuota       ErrorCode = "quota"
	CodeRateLimit   ErrorCode = "rateLimit"
	CodeSafety      ErrorCode = "safety"
	CodeProvider    ErrorCode = "provider"
	CodeTimeout     ErrorCode = "timeout"
	CodeNotFound    ErrorCode = "notFound"
	CodeServerError ErrorCode = "serverError"
	CodeConnection  ErrorCode = "connection"
	CodeAborted     ErrorCode = "aborted"
	CodeUnknown     ErrorCode = "unknown"
)

// ErrNoAPIKey is returned when no API key is present in the local configuration.
var ErrNoAPIKey = errors.New("no API key configured")

var safetyCodes = map[string]struct{}{
	"contentPolicyViolation":         {},
	"providerContentPolicyViolation": {},
	"sensitiveContentDetected":       {},
	"unsafeContentDetected":          {},
	"nsfwContentDetected":            {},
	"promptBlocked":                  {},
	"imageBlocked":                   {},
	"moderationFailed":               {},
}

var authCodes = map[string]struct{}{
	"unauthorized":            {},
	"forbidden":               {},
	"permissionDenied":        {},
	"insufficientPermissions": {},
	"authenticationFailed":    {},
	"authFailed":              {},
	"authTimeout":             {},
	"invalidAuthentication":   {},
	"invalidCredentials":      {},
	"missingAuthentication":   {},
	"tokenExpired":            {},
	"tokenInvalid":            {},
	"tokenMissing":            {},
	"tokenRevoked":            {},
	"accountSuspended":        {},
	"accountDisabled":         {},
	"organizationSuspended":   {},
	"organizationDisabled":    {},
	"workspaceSuspended":      {},
	"workspaceDisabled":       {},
}

var serverErrorCodes = map[string]struct{}{
	"internalServerError":              {},
	"serviceUnavailable":               {},
	"serverUnavailable":                {},
	"standardError":                    {},
	"unknownError":                     {},
	"undefinedError":                   {},
	"defaultError":                     {},
	"unrecognizedResponse":             {},
	"errorRetrievingAccountManagement": {},
}

var notFoundCodes = map[string]struct{}{
	"taskCancelled":        {},
	"taskFailedOrNotFound": {},
	"unknownModel":         {},
}

var providerCodes = map[string]struct{}{
	"inferenceError":                  {},
	"processingFailed":                {},
	"taskFailed":                      {},
	"downloadFailed":                  {},
	"uploadFailed":                    {},
	"noAvailableServer":               {},
	"modelUnavailable":                {},
	"modelDisabled":                   {},
	"modelNotReady":                   {},
	"mediaStorageFileCouldNotBeMoved": {},
}

var retryableCodes = map[ErrorCode]struct{}{
	CodeProvider:    {},
	CodeTimeout:     {},
	CodeConnection:  {},
	CodeRateLimit:   {},
	CodeServerError: {},
}

// IsRetryable reports whether the given error code represents a transient failure
// that may succeed if the request is retried.
func IsRetryable(code ErrorCode) bool {
	_, ok := retryableCodes[code]
	return ok
}

// DeriveCode maps a raw server-side error code string to a stable ErrorCode.
// The mapping mirrors the TypeScript SDK's deriveCode function.
func DeriveCode(raw string) ErrorCode {
	if raw == "aborted" {
		return CodeAborted
	}
	if raw == "connectionFailed" || raw == "notConnected" || raw == "notOpen" || raw == "reconnectionFailed" {
		return CodeConnection
	}

	if strings.Contains(raw, "Credits") || strings.Contains(raw, "Quota") || strings.Contains(raw, "Balance") || raw == "quotaExceeded" || raw == "paymentRequired" {
		return CodeQuota
	}
	if strings.Contains(raw, "RateLimit") || raw == "rateLimitExceeded" {
		return CodeRateLimit
	}

	if _, ok := safetyCodes[raw]; ok {
		return CodeSafety
	}

	_, inAuth := authCodes[raw]
	if inAuth || strings.Contains(raw, "ApiKey") {
		return CodeAuth
	}

	if strings.Contains(raw, "Timeout") || raw == "timeout" {
		return CodeTimeout
	}

	_, inNotFound := notFoundCodes[raw]
	if inNotFound || strings.HasSuffix(raw, "NotFound") || strings.HasSuffix(raw, "Expired") {
		return CodeNotFound
	}

	if _, ok := serverErrorCodes[raw]; ok {
		return CodeServerError
	}

	// Provider auth = upstream auth (Runware's keys, not the user's). Transient;
	// treat as provider rather than user-facing auth failure.
	_, inProvider := providerCodes[raw]
	if inProvider || strings.HasPrefix(raw, "provider") {
		return CodeProvider
	}

	// Validation catch-all for request-shape problems.
	if strings.HasPrefix(raw, "invalid") ||
		strings.HasPrefix(raw, "missing") ||
		strings.HasPrefix(raw, "conflict") ||
		strings.HasPrefix(raw, "duplicate") ||
		strings.HasPrefix(raw, "unsupported") ||
		strings.HasPrefix(raw, "value") ||
		strings.HasPrefix(raw, "array") ||
		raw == "unknownParameter" ||
		raw == "incompatibleParameters" ||
		raw == "mismatchProviderSettingsProvider" ||
		raw == "unknownProviderSettingsProvider" ||
		raw == "transparentModelMismatch" ||
		raw == "modelOwnershipValidationError" ||
		raw == "modelAlreadyExists" ||
		strings.HasPrefix(raw, "max") ||
		raw == "validationFailed" {
		return CodeValidation
	}

	return CodeUnknown
}

const (
	docsBase        = "https://runware.ai/docs"
	sdkErrorDocPath = "getting-started/errors"
)

var utilityDocPaths = map[string]string{
	"modelSearch":       "platform/model-search",
	"modelUpload":       "platform/model-upload",
	"imageUpload":       "platform/image-upload",
	"getResponse":       "platform/get-response",
	"accountManagement": "platform/account-management",
}

// cliInternalRawCodes are raw codes originating from the CLI transport layer
// rather than the API; they map to the general errors documentation page.
var cliInternalRawCodes = map[string]struct{}{
	"missingApiKey": {},
}

func paramAnchor(parameter string) string {
	return "#request-" + strings.ToLower(strings.ReplaceAll(parameter, ".", "-"))
}

// buildDocumentationURL derives a documentation URL from the error context.
// It maps known task types to their documentation pages and appends a parameter
// anchor when the parameter field is set. Model-specific URL resolution
// (available in the TypeScript SDK) is omitted in the CLI.
func buildDocumentationURL(taskType, parameter, rawCode string) string {
	if path, ok := utilityDocPaths[taskType]; ok {
		base := docsBase + "/" + path
		if parameter != "" {
			return base + paramAnchor(parameter)
		}
		return base
	}
	if _, ok := cliInternalRawCodes[rawCode]; ok {
		return docsBase + "/" + sdkErrorDocPath
	}
	return ""
}

// RunwareError is a structured error returned by the Runware API or transport layer.
type RunwareError struct {
	// Code is a stable category for this error. Use it for switch/if statements.
	Code ErrorCode
	// RawCode is the original error code string from the API.
	RawCode string
	// Message is the human-readable error description.
	Message string
	// Retryable is true if retrying the same request might succeed.
	Retryable bool
	// Parameter is the request field that caused the error, if applicable.
	Parameter string
	// AllowedValues lists the accepted values for Parameter, if provided.
	AllowedValues []string
	// TaskType is the task type of the request that failed.
	TaskType string
	// TaskUUID is the unique identifier of the failed request.
	TaskUUID string
	// Documentation is a link to relevant documentation, if available.
	Documentation string
	// StatusCode is the HTTP status code when the error originated from an HTTP response.
	StatusCode int
}

func (e *RunwareError) Error() string { return e.Message }

// wireError is the raw JSON shape of an error item from the Runware API.
type wireError struct {
	Code             string          `json:"code"`
	Message          string          `json:"message"`
	RawParameter     json.RawMessage `json:"parameter,omitempty"`
	Type             string          `json:"type,omitempty"`
	Documentation    string          `json:"documentation,omitempty"`
	TaskType         string          `json:"taskType,omitempty"`
	TaskUUID         string          `json:"taskUUID,omitempty"`
	RawAllowedValues json.RawMessage `json:"allowedValues,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler so RunwareError can be decoded
// directly from an API errors array element.
func (e *RunwareError) UnmarshalJSON(data []byte) error {
	var w wireError
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	e.RawCode = w.Code
	e.Code = DeriveCode(w.Code)
	e.Retryable = IsRetryable(e.Code)
	e.Message = w.Message
	e.TaskType = w.TaskType
	e.TaskUUID = w.TaskUUID

	// Normalise parameter: the API sends either a string or []string.
	if len(w.RawParameter) > 0 {
		var s string
		if err := json.Unmarshal(w.RawParameter, &s); err == nil {
			e.Parameter = s
		} else {
			var arr []string
			if err := json.Unmarshal(w.RawParameter, &arr); err == nil && len(arr) > 0 {
				e.Parameter = arr[0]
			}
		}
	}

	e.AllowedValues = normalizeAllowedValues(w.RawAllowedValues)

	// Prefer the documentation URL from the API response; fall back to derived.
	if w.Documentation != "" {
		e.Documentation = w.Documentation
	} else {
		e.Documentation = buildDocumentationURL(e.TaskType, e.Parameter, e.RawCode)
	}

	return nil
}

// normalizeAllowedValues accepts either a JSON array or a JSON object
// (e.g. {"0":"checkpoint","1":"lora"}) and returns the values as strings.
// The API sends both shapes. Unrecognized shapes yield nil; parsing never fails.
func normalizeAllowedValues(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}

	var arr []any
	if err := json.Unmarshal(raw, &arr); err == nil {
		vals := make([]string, 0, len(arr))
		for _, v := range arr {
			vals = append(vals, fmt.Sprint(v))
		}
		return vals
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		keys := slices.Collect(maps.Keys(obj))
		sortAllowedValueKeys(keys)
		vals := make([]string, 0, len(keys))
		for _, k := range keys {
			vals = append(vals, fmt.Sprint(obj[k]))
		}
		return vals
	}

	return nil
}

// sortAllowedValueKeys orders keys numerically when every key parses as an
// integer (the API's object shape uses numeric string keys), falling back to
// lexicographic order otherwise.
func sortAllowedValueKeys(keys []string) {
	nums := make(map[string]int, len(keys))
	for _, k := range keys {
		n, err := strconv.Atoi(k)
		if err != nil {
			slices.Sort(keys)
			return
		}
		nums[k] = n
	}
	slices.SortFunc(keys, func(a, b string) int { return nums[a] - nums[b] })
}

// RunwareErrorDetails carries optional context for CreateRunwareError.
type RunwareErrorDetails struct {
	Parameter  string
	TaskType   string
	TaskUUID   string
	StatusCode int
}

// CreateRunwareError constructs a RunwareError from a raw code, message, and
// optional details.
func CreateRunwareError(rawCode, message string, details RunwareErrorDetails) *RunwareError {
	e := &RunwareError{
		RawCode:    rawCode,
		Code:       DeriveCode(rawCode),
		Message:    message,
		Parameter:  details.Parameter,
		TaskType:   details.TaskType,
		TaskUUID:   details.TaskUUID,
		StatusCode: details.StatusCode,
	}
	e.Retryable = IsRetryable(e.Code)
	e.Documentation = buildDocumentationURL(e.TaskType, e.Parameter, e.RawCode)
	return e
}

// ParseAPIError parses a raw API response body into a RunwareError.
// It handles three JSON shapes:
//
//	[{...}]                        — array of error items; first element is used
//	{"errors":[{...}]}             — standard API envelope; errors[0] is used
//	{"error":{...},"taskUUID":"..."} — pre-stream HTTP error; inner object is used
func ParseAPIError(raw json.RawMessage, statusCode int) *RunwareError {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return CreateRunwareError("unknown", string(raw), RunwareErrorDetails{StatusCode: statusCode})
	}

	var errItem json.RawMessage
	switch val := v.(type) {
	case []any:
		if len(val) > 0 {
			b, _ := json.Marshal(val[0]) //nolint:errcheck
			errItem = b
		}
	case map[string]any:
		if errs, ok := val["errors"].([]any); ok && len(errs) > 0 {
			b, _ := json.Marshal(errs[0]) //nolint:errcheck
			errItem = b
		} else if errObj, ok := val["error"].(map[string]any); ok {
			// Merge taskUUID / taskType from the outer envelope if absent.
			if _, has := errObj["taskUUID"]; !has {
				if uid, ok := val["taskUUID"]; ok {
					errObj["taskUUID"] = uid
				}
			}
			if _, has := errObj["taskType"]; !has {
				if tt, ok := val["taskType"]; ok {
					errObj["taskType"] = tt
				}
			}
			b, _ := json.Marshal(errObj) //nolint:errcheck
			errItem = b
		} else {
			errItem = raw
		}
	default:
		return CreateRunwareError("unknown", fmt.Sprintf("%v", v), RunwareErrorDetails{StatusCode: statusCode})
	}

	if errItem == nil {
		return CreateRunwareError("unknown", "an unknown API error occurred", RunwareErrorDetails{StatusCode: statusCode})
	}

	var re RunwareError
	if err := json.Unmarshal(errItem, &re); err != nil {
		return CreateRunwareError("unknown", string(errItem), RunwareErrorDetails{StatusCode: statusCode})
	}
	re.StatusCode = statusCode
	return &re
}

// IsAuthError reports whether err is an authentication or authorisation error.
func IsAuthError(err error) bool {
	if errors.Is(err, ErrNoAPIKey) {
		return true
	}
	var re *RunwareError
	if errors.As(err, &re) {
		return re.Code == CodeAuth
	}
	return false
}
