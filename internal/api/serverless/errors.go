package serverless

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

// problemToError converts an RFC 9457 ProblemDetails (or a bare status) into a
// *transport.RunwareError so the CLI's shared error rendering (including the
// auth hint on 401/403) applies uniformly.
func problemToError(p *gen.ProblemDetails, statusCode int) error {
	msg := fmt.Sprintf("HTTP %d: %s", statusCode, http.StatusText(statusCode))
	if p != nil {
		if p.Status != 0 {
			statusCode = int(p.Status)
		}
		switch {
		case p.Detail != nil && *p.Detail != "":
			msg = *p.Detail
		case p.Title != "":
			msg = p.Title
		}
	}
	return transport.CreateRunwareError(
		rawCodeForStatus(statusCode),
		msg,
		transport.RunwareErrorDetails{StatusCode: statusCode},
	)
}

// problemFromBody attempts to decode an RFC 9457 ProblemDetails from a response
// body when the generated client did not bind a typed problem for this status.
// Falls back to a status-only error when the body is empty or not a problem.
func problemFromBody(body []byte, statusCode int) error {
	if len(body) > 0 {
		var p gen.ProblemDetails
		if err := json.Unmarshal(body, &p); err == nil && isProblemDetails(p) {
			return problemToError(&p, statusCode)
		}
	}
	return problemToError(nil, statusCode)
}

func isProblemDetails(p gen.ProblemDetails) bool {
	return p.Title != "" || p.Type != "" || p.Status != 0 || (p.Detail != nil && *p.Detail != "")
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
		return "resourceNotFound"
	case http.StatusConflict:
		return "conflict"
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "validationFailed"
	default:
		// Must be a key in transport.serverErrorCodes so DeriveCode returns
		// CodeServerError rather than CodeUnknown.
		return "internalServerError"
	}
}
