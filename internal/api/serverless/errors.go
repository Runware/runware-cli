package serverless

import (
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
