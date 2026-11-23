package serverless

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

const redactedValue = "[redacted]"

// logResponse writes a debug line for a control-plane response. Path and
// status come from the generated client's HTTPResponse so they track OpenAPI
// regen (no duplicated path literals). Success bodies have JSON "value"
// fields redacted so plaintext env vars are not persisted in debug logs.
// Error bodies (problem details) are logged as returned. A nil body logs path
// and status only (used for secrets endpoints whose metadata is opaque).
func (c *Client) logResponse(ctx context.Context, httpResp *http.Response, body []byte) {
	if c.logger == nil || !c.logger.Enabled(ctx, slog.LevelDebug) {
		return
	}
	path, status := responsePathStatus(httpResp)
	if body == nil {
		c.logger.Debug("serverless response", //nolint:errcheck,gosec
			"path", path,
			"status", status,
		)
		return
	}
	c.logger.Debug("serverless response", //nolint:errcheck,gosec
		"path", path,
		"status", status,
		"bodyBytes", len(body),
		"body", debugLogBody(status, body),
	)
}

func responsePathStatus(httpResp *http.Response) (path string, status int) {
	if httpResp == nil {
		return "", 0
	}
	status = httpResp.StatusCode
	if httpResp.Request != nil && httpResp.Request.URL != nil {
		path = httpResp.Request.URL.Path
	}
	return path, status
}

func debugLogBody(status int, body []byte) string {
	if status >= http.StatusOK && status < http.StatusBadRequest {
		return string(redactJSONValues(body))
	}
	return string(body)
}

// valueKeys is the member redactJSONValues replaces.
var valueKeys = map[string]struct{}{
	"value": {},
}

// redactJSONValues replaces every object key "value" with "[redacted]".
// Returns the original bytes if the body is not JSON or has no such key.
func redactJSONValues(body []byte) []byte {
	out, ok := redactJSONKeys(body, valueKeys)
	if !ok {
		return body
	}
	return out
}

// redactJSONKeys replaces the value of every object member named in keys,
// at any depth and in any letter case, with "[redacted]". A member name
// matches without regard to case because encoding/json decodes it that way.
// It returns the original bytes when there is no such member, and false
// when the body is not JSON and so could not be checked.
func redactJSONKeys(body []byte, keys map[string]struct{}) ([]byte, bool) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, false
	}
	if !redactKeys(v, keys) {
		return body, true
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return out, true
}

// isSecretKey reports whether name is one of keys, ignoring letter case.
func isSecretKey(name string, keys map[string]struct{}) bool {
	for k := range keys {
		if strings.EqualFold(name, k) {
			return true
		}
	}
	return false
}

func redactKeys(v any, keys map[string]struct{}) bool {
	switch t := v.(type) {
	case map[string]any:
		changed := false
		for k, child := range t {
			if isSecretKey(k, keys) {
				t[k] = redactedValue
				changed = true
				continue
			}
			if redactKeys(child, keys) {
				changed = true
			}
		}
		return changed
	case []any:
		changed := false
		for _, child := range t {
			if redactKeys(child, keys) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}
