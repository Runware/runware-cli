package serverless

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/runware/runware-cli/internal/api/transport"
)

// wireError is the Serverless API error envelope: {"code":int,"message":string}.
type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// parseError converts a non-2xx Serverless API response into a
// *transport.RunwareError so the CLI's shared error rendering (including the
// auth hint on 401/403) applies uniformly.
func parseError(body []byte, statusCode int) error {
	var w wireError
	if err := json.Unmarshal(body, &w); err != nil || w.Message == "" {
		return transport.CreateRunwareError(
			rawCodeForStatus(statusCode),
			fmt.Sprintf("HTTP %d: %s", statusCode, http.StatusText(statusCode)),
			transport.RunwareErrorDetails{StatusCode: statusCode},
		)
	}
	return transport.CreateRunwareError(
		rawCodeForStatus(statusCode),
		w.Message,
		transport.RunwareErrorDetails{StatusCode: statusCode},
	)
}

// rawCodeForStatus maps an HTTP status to a raw error code string that
// transport.DeriveCode categorises correctly (notably 401/403 -> auth).
func rawCodeForStatus(statusCode int) string {
	switch statusCode {
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "notFound"
	case http.StatusConflict:
		return "conflict"
	default:
		return "serverError"
	}
}
