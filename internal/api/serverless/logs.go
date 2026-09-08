package serverless

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"

	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

// LogEntry is one application log line.
type LogEntry = gen.LogEntry

// LogEntryPage is one page of log entries, newest first.
type LogEntryPage = gen.LogEntryPage

// GetLogEntriesParams narrows a log page: window, page size, cursor, app and endpoint.
type GetLogEntriesParams = gen.GetLogEntriesParams

// LogWindow is the closed set of time windows a log query accepts.
type LogWindow = gen.GetLogEntriesParamsWindow

const (
	// LogQueryRuntime is the pageable application log query.
	LogQueryRuntime = "runtime"
	// LogQueryRuntimeTail is the only log query the tail route accepts.
	LogQueryRuntimeTail = "runtime_tail"
	// DefaultLogLimit is sent when the caller sets no page size. The API
	// documents 20 but does not apply it, so an omitted limit falls through
	// to the downstream default of 100.
	DefaultLogLimit = 20
)

// ErrTailEnded reports that the server closed a log stream on purpose, at the
// end of its connection lifetime or on shutdown. The caller may reconnect.
var ErrTailEnded = errors.New("log stream ended")

// TailStreamError is the server's `error` event: the stream is over and the
// detail says why.
type TailStreamError struct {
	Detail string
}

func (e *TailStreamError) Error() string {
	if e.Detail == "" {
		return "log stream failed"
	}
	return "log stream failed: " + e.Detail
}

// TailUnavailableError reports that a gateway answered the tail request instead
// of the log store, so the stream never opened. No entry was delivered, which
// makes a reconnect free of repeated output. An app that has not written an
// entry yet answers this way: the store holds the request without writing its
// response headers, and the edge times it out before the first entry arrives.
type TailUnavailableError struct {
	StatusCode int
	Err        error
}

func (e *TailUnavailableError) Error() string {
	return "log stream unavailable: " + e.Err.Error()
}

func (e *TailUnavailableError) Unwrap() error { return e.Err }

// isGatewayStatus reports whether a proxy in front of the log store, rather
// than the store itself, ended the request. These are transient, so a tail
// reconnects on them instead of failing.
func isGatewayStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// GetLogEntries returns one page of a named log query.
func (c *Client) GetLogEntries(ctx context.Context, queryID string, params GetLogEntriesParams) (LogEntryPage, error) {
	if c.apiKey == "" {
		return LogEntryPage{}, transport.ErrNoAPIKey
	}
	if params.Limit == nil {
		limit := Limit(DefaultLogLimit)
		params.Limit = &limit
	}

	resp, err := c.inner.GetLogEntriesWithResponse(ctx, queryID, &params)
	if err != nil {
		return LogEntryPage{}, fmt.Errorf("get log entries: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return LogEntryPage{}, fmt.Errorf("get log entries: empty 200 response")
		}
		page := *resp.JSON200
		if page.Entries == nil {
			page.Entries = []LogEntry{}
		}
		return page, nil
	case http.StatusBadRequest:
		return LogEntryPage{}, problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
	case http.StatusUnauthorized:
		return LogEntryPage{}, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return LogEntryPage{}, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return LogEntryPage{}, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	case http.StatusUnprocessableEntity:
		return LogEntryPage{}, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	case http.StatusTooManyRequests:
		return LogEntryPage{}, problemToError(resp.ApplicationproblemJSON429, http.StatusTooManyRequests)
	case http.StatusBadGateway:
		return LogEntryPage{}, problemToError(resp.ApplicationproblemJSON502, http.StatusBadGateway)
	case http.StatusServiceUnavailable:
		return LogEntryPage{}, problemToError(resp.ApplicationproblemJSON503, http.StatusServiceUnavailable)
	case http.StatusGatewayTimeout:
		return LogEntryPage{}, problemToError(resp.ApplicationproblemJSON504, http.StatusGatewayTimeout)
	default:
		return LogEntryPage{}, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// TailLogs follows a named log query for one app and hands every new entry to
// emit. It returns ErrTailEnded when the server closes the stream cleanly, a
// *TailStreamError when the server reports a failure, a *TailUnavailableError
// when a gateway answers before the stream opens, ctx.Err() when the caller
// stops, and any error emit returns.
func (c *Client) TailLogs(ctx context.Context, queryID, appID string, emit func(LogEntry) error) error {
	if c.apiKey == "" {
		return transport.ErrNoAPIKey
	}

	params := gen.TailLogEntriesParams{
		Deployment: appID,
	}
	resp, err := c.streamInner().TailLogEntries(ctx, queryID, &params)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("tail logs: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxSSEFrameBytes))
		c.logResponse(ctx, resp, body)
		problem := problemFromBody(body, resp.StatusCode)
		if isGatewayStatus(resp.StatusCode) {
			return &TailUnavailableError{
				StatusCode: resp.StatusCode,
				Err:        problem,
			}
		}
		return problem
	}
	c.logResponse(ctx, resp, nil)
	if mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mediaType != "text/event-stream" {
		return fmt.Errorf("tail logs: unexpected content type %q", resp.Header.Get("Content-Type"))
	}

	err = readSSEEvents(resp.Body, func(ev sseEvent) error {
		switch ev.Event {
		case "", "message":
			var entry LogEntry
			if err := json.Unmarshal([]byte(ev.Data), &entry); err != nil {
				return fmt.Errorf("tail logs: decode entry: %w", err)
			}
			return emit(entry)
		case sseEventEnd:
			return ErrTailEnded
		case sseEventError:
			return &TailStreamError{Detail: ev.Data}
		default:
			return nil
		}
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		// The connection closed without an end or error event.
		return ErrTailEnded
	}
	return err
}

// streamInner returns the generated client without a whole-request timeout,
// which a long-lived event stream would otherwise hit.
func (c *Client) streamInner() *gen.ClientWithResponses {
	if hc, ok := c.doer.(*http.Client); !ok || hc.Timeout == 0 {
		return c.inner
	}
	return c.innerWithTimeout(0)
}
