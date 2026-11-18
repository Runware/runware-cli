package serverless

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
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
	return redactJSONKeys(body, valueKeys)
}

// redactJSONKeys replaces the value of every object member named in keys,
// at any depth, with "[redacted]". Returns the original bytes if the body is
// not JSON or has no such member.
func redactJSONKeys(body []byte, keys map[string]struct{}) []byte {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return body
	}
	if !redactKeys(v, keys) {
		return body
	}
	out, err := json.Marshal(v)
	if err != nil {
		return body
	}
	return out
}

func redactKeys(v any, keys map[string]struct{}) bool {
	switch t := v.(type) {
	case map[string]any:
		changed := false
		for k, child := range t {
			if _, secret := keys[k]; secret {
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
