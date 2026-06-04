package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/runware/runware-cli/internal/agents"
	"github.com/runware/runware-cli/internal/buildinfo"
)

const wsAuthTaskType = "authentication"
const wsKeyTaskType = "taskType"
const wsPingTaskType = "ping"

const (
	wsAuthTimeout        = 30 * time.Second
	wsPingInterval       = 30 * time.Second
	wsInactivityTimeout  = 100 * time.Second
	wsReconnectBaseDelay = time.Second
	wsReconnectMaxDelay  = 30 * time.Second
)

// WSOption is a functional option for configuring a WSTransport.
type WSOption func(*WSTransport)

// WithMaxReconnectAttempts sets the maximum number of reconnection attempts
// before the transport gives up and fails all in-flight requests.
// Default is 10.
func WithMaxReconnectAttempts(n int) WSOption {
	return func(t *WSTransport) { t.maxReconnectAttempts = n }
}

// WithPingInterval overrides the keepAlive ping interval. Intended for testing;
// production code should rely on the default (30s).
func WithPingInterval(d time.Duration) WSOption {
	return func(t *WSTransport) { t.pingInterval = d }
}

// WithReconnectBaseDelay overrides the base delay between reconnect attempts.
// Intended for testing; production code should rely on the default (1s).
func WithReconnectBaseDelay(d time.Duration) WSOption {
	return func(t *WSTransport) { t.reconnectBaseDelay = d }
}

// wsResult carries a single inbound result or an error for an in-flight Send.
type wsResult struct {
	data json.RawMessage
	err  error
}

// taskMeta holds metadata extracted from a single outbound task.
type taskMeta struct {
	uuid     string
	taskType string
	expected int // numberResults (default 1)
}

// inflightEntry associates an in-flight Send channel with its task type and
// expected result count so we can support multi-result tasks (numberResults > 1)
// and clean up both lookup maps atomically.
type inflightEntry struct {
	taskType string
	ch       chan wsResult
	expected int // total results expected
	received int // results dispatched so far
}

// WSTransport implements Transport over a persistent WebSocket connection.
//
// The first call to Send (or an explicit call to Connect) establishes the
// connection and authenticates with the API. Subsequent calls reuse the same
// connection.
//
// On connection loss the transport reconnects automatically with exponential
// backoff. In-flight Send calls are preserved across reconnections; the server
// replays buffered results via the connectionSessionUUID mechanism. Only when
// the maximum reconnect attempts are exhausted are in-flight calls failed.
//
// Disconnect is terminal — Connect cannot be called again after Disconnect.
type WSTransport struct {
	apiKey               string
	baseURL              string
	userAgent            string
	logger               *slog.Logger
	maxReconnectAttempts int
	pingInterval         time.Duration
	reconnectBaseDelay   time.Duration

	// mu guards conn, connCancel, sessionUUID, connected, shouldReconnect,
	// reconnectAttempt, reconnectCh, and lastActivity.
	mu           sync.Mutex
	conn         *websocket.Conn
	connCancel   context.CancelFunc // cancels reader + keepAlive for current conn
	sessionUUID  string             // connectionSessionUUID for session resumption
	connected    bool
	lastActivity time.Time

	// shouldReconnect is set true by Connect and false by Disconnect.
	shouldReconnect  bool
	reconnectAttempt int
	// reconnectCh is non-nil while a reconnect is in progress; it is closed
	// (and reset to nil) when the reconnect cycle finishes (success or exhausted).
	reconnectCh chan struct{}

	// disconnected is closed exactly once by Disconnect. It permanently signals
	// all current and future Send callers that the transport is shut down.
	disconnected   chan struct{}
	disconnectOnce sync.Once

	// writeMu serialises writes; gorilla requires one concurrent writer.
	writeMu sync.Mutex

	// inflightMu guards both inflight maps.
	//
	// inflight maps taskUUID → inflightEntry.
	// inflightByType maps taskType → []taskUUID (FIFO queue); used to dispatch
	// server frames that carry a taskType but no taskUUID (e.g. ping responses).
	inflightMu     sync.RWMutex
	inflight       map[string]inflightEntry // taskUUID → entry
	inflightByType map[string][]string      // taskType → []taskUUID (FIFO)
}

// NewWSTransport creates a WebSocket transport for the given API key and base URL.
// Connect is deferred until the first Send (or an explicit Connect call).
func NewWSTransport(apiKey, baseURL string, logger *slog.Logger, opts ...WSOption) *WSTransport {
	ua := buildinfo.UserAgent()
	if agent := agents.Detect(); agent != "" {
		ua += " agent/" + string(agent)
	}
	t := &WSTransport{
		apiKey:               apiKey,
		baseURL:              baseURL,
		userAgent:            ua,
		logger:               logger,
		maxReconnectAttempts: 10,
		pingInterval:         wsPingInterval,
		reconnectBaseDelay:   wsReconnectBaseDelay,
		inflight:             make(map[string]inflightEntry),
		inflightByType:       make(map[string][]string),
		disconnected:         make(chan struct{}),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Connect establishes the WebSocket connection and authenticates with the API.
// It is safe to call multiple times; subsequent calls are no-ops if already
// connected. If a previous connectionSessionUUID is held, it is sent in the
// auth request so the server can replay any buffered results.
//
// Connect returns an error if called after Disconnect.
func (t *WSTransport) Connect(ctx context.Context) error {
	// Reject Connect on a permanently disconnected transport.
	select {
	case <-t.disconnected:
		return CreateRunwareError(
			"connectionFailed",
			"transport has been permanently disconnected",
			RunwareErrorDetails{},
		)
	default:
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.connected {
		return nil
	}
	if t.apiKey == "" {
		return ErrNoAPIKey
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: websocket.DefaultDialer.HandshakeTimeout,
	}
	hdr := http.Header{"User-Agent": {t.userAgent}}
	conn, resp, err := dialer.DialContext(ctx, t.baseURL, hdr)
	if resp != nil {
		resp.Body.Close() //nolint:errcheck,gosec
	}
	if err != nil {
		return CreateRunwareError(
			"connectionFailed",
			fmt.Sprintf("WebSocket dial failed: %v", err),
			RunwareErrorDetails{},
		)
	}

	// Build auth request, optionally resuming a previous session.
	authTask := map[string]any{
		wsKeyTaskType: wsAuthTaskType,
		"apiKey":      t.apiKey,
	}
	if t.sessionUUID != "" {
		authTask["connectionSessionUUID"] = t.sessionUUID
	}

	authBody, err := json.Marshal([]any{authTask})
	if err != nil {
		conn.Close() //nolint:errcheck,gosec
		return fmt.Errorf("failed to marshal auth request: %w", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, authBody); err != nil {
		conn.Close() //nolint:errcheck,gosec
		return CreateRunwareError(
			"connectionFailed",
			fmt.Sprintf("failed to send auth request: %v", err),
			RunwareErrorDetails{},
		)
	}

	// Set a read deadline on the auth response so a silent server cannot block
	// Connect indefinitely. Use the context deadline if it is sooner.
	authDeadline := time.Now().Add(wsAuthTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(authDeadline) {
		authDeadline = ctxDeadline
	}
	conn.SetReadDeadline(authDeadline) //nolint:errcheck,gosec

	// Read the auth response synchronously before starting the reader goroutine.
	_, msg, err := conn.ReadMessage()
	conn.SetReadDeadline(time.Time{}) //nolint:errcheck,gosec // clear deadline for all subsequent reads
	if err != nil {
		conn.Close() //nolint:errcheck,gosec
		return CreateRunwareError(
			"connectionFailed",
			fmt.Sprintf("failed to read auth response: %v", err),
			RunwareErrorDetails{},
		)
	}

	if t.logger != nil && t.logger.Enabled(ctx, slog.LevelDebug) {
		t.logger.Debug("ws auth response", "body", string(msg))
	}

	var envelope wsEnvelope
	if err := json.Unmarshal(msg, &envelope); err != nil {
		conn.Close() //nolint:errcheck,gosec
		return fmt.Errorf("failed to parse auth response: %w", err)
	}

	// Normalise: if the auth response used a singular "error" shape or another
	// non-standard form, ParseAPIError surfaces it.
	if len(envelope.Errors) == 0 && len(envelope.Data) == 0 {
		if e := ParseAPIError(msg, 0); e.Code != CodeUnknown {
			envelope.Errors = []RunwareError{*e}
		}
	}

	if len(envelope.Errors) > 0 {
		conn.Close() //nolint:errcheck,gosec
		e := envelope.Errors[0]
		return &e
	}

	// Extract connectionSessionUUID from the auth data item.
	for _, raw := range envelope.Data {
		var item struct {
			TaskType              string `json:"taskType"`
			ConnectionSessionUUID string `json:"connectionSessionUUID"`
		}
		if err := json.Unmarshal(raw, &item); err == nil && item.TaskType == wsAuthTaskType {
			t.sessionUUID = item.ConnectionSessionUUID
			break
		}
	}

	// Require connectionSessionUUID; without it session resumption is impossible
	// and the authentication may not have fully succeeded.
	if t.sessionUUID == "" {
		conn.Close() //nolint:errcheck,gosec
		return CreateRunwareError(
			"authFailed",
			"authentication succeeded but missing connectionSessionUUID",
			RunwareErrorDetails{},
		)
	}

	// Create a per-connection context to stop reader and keepAlive when this
	// specific connection is closed or replaced.
	connCtx, connCancel := context.WithCancel(context.Background())
	t.connCancel = connCancel
	t.conn = conn
	t.connected = true
	t.shouldReconnect = true
	t.lastActivity = time.Now()

	go t.reader(conn, connCancel)
	go t.keepAlive(connCtx, conn)

	return nil
}

// Disconnect permanently closes the WebSocket connection and unblocks all
// in-flight Send calls. Disconnect is terminal — Connect cannot be called again
// after Disconnect. Safe to call if Connect was never called.
func (t *WSTransport) Disconnect() error {
	t.mu.Lock()
	t.shouldReconnect = false
	conn := t.conn
	connCancel := t.connCancel
	t.connected = false
	t.conn = nil
	t.connCancel = nil
	t.mu.Unlock()

	// Stop per-connection goroutines (reader, keepAlive).
	if connCancel != nil {
		connCancel()
	}
	// Close the raw connection.
	if conn != nil {
		conn.Close() //nolint:errcheck,gosec
	}

	// Permanently signal all current and future Send callers. Closing
	// disconnected unblocks every select in Send that includes <-t.disconnected.
	t.disconnectOnce.Do(func() {
		close(t.disconnected)
	})

	return nil
}

// Send marshals tasks, writes them to the WebSocket connection, and waits for
// all expected results for each task UUID. The connection is established lazily
// on the first call.
//
// If a task carries a "numberResults" field (int > 1), Send collects that many
// result items before returning. All results for all tasks are returned as a
// flat slice in task order. Defaults to 1 result per task if the field is absent.
func (t *WSTransport) Send(ctx context.Context, tasks []any) ([]json.RawMessage, error) {
	if t.apiKey == "" {
		return nil, ErrNoAPIKey
	}

	// Bail immediately if permanently disconnected.
	select {
	case <-t.disconnected:
		return nil, CreateRunwareError(
			"connectionFailed",
			"WebSocket connection disconnected by client",
			RunwareErrorDetails{},
		)
	default:
	}

	// Lazy connect / reconnect-in-progress handling.
	t.mu.Lock()
	connected := t.connected
	reconnectCh := t.reconnectCh
	t.mu.Unlock()

	if !connected {
		if reconnectCh != nil {
			// A reconnect cycle is running; wait for it to finish, then check
			// whether it succeeded before proceeding.
			select {
			case <-reconnectCh:
				t.mu.Lock()
				connected = t.connected
				t.mu.Unlock()
				if !connected {
					return nil, CreateRunwareError(
						"connectionFailed",
						"reconnection failed",
						RunwareErrorDetails{},
					)
				}
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-t.disconnected:
				return nil, CreateRunwareError(
					"connectionFailed",
					"WebSocket connection disconnected by client",
					RunwareErrorDetails{},
				)
			}
		} else {
			if err := t.Connect(ctx); err != nil {
				return nil, err
			}
		}
	}

	// Marshal once; also extract task UUIDs, task types, and expected result
	// counts from the same pass.
	body, metas, err := marshalAndExtractTaskMeta(tasks)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if t.logger != nil && t.logger.Enabled(ctx, slog.LevelDebug) {
		t.logger.Debug("ws send", "body", string(redactTasks(body)))
	}

	// Capture conn under mu so we have a stable reference for the write.
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()

	// Register in-flight channels before writing so the reader can dispatch
	// results immediately after the write completes.
	//
	// Channel buffer: expected + 1. The +1 ensures broadcastErr can always
	// enqueue one error even when all result slots are already full.
	localChans := make(map[string]chan wsResult, len(metas))
	t.inflightMu.Lock()
	for _, m := range metas {
		if m.uuid == "" {
			continue
		}
		ch := make(chan wsResult, m.expected+1)
		localChans[m.uuid] = ch
		t.inflight[m.uuid] = inflightEntry{
			taskType: m.taskType,
			ch:       ch,
			expected: m.expected,
		}
		t.inflightByType[m.taskType] = append(t.inflightByType[m.taskType], m.uuid)
	}
	t.inflightMu.Unlock()

	// Write the message; serialised with writeMu (gorilla: one concurrent writer).
	t.writeMu.Lock()
	writeErr := conn.WriteMessage(websocket.TextMessage, body)
	t.writeMu.Unlock()

	if writeErr != nil {
		t.cleanupLocalChans(localChans)
		return nil, CreateRunwareError(
			"connectionFailed",
			fmt.Sprintf("WebSocket write failed: %v", writeErr),
			RunwareErrorDetails{},
		)
	}

	// Collect all expected results for every task UUID, in task order.
	results := make([]json.RawMessage, 0, len(metas))
	for _, m := range metas {
		if m.uuid == "" {
			continue
		}
		ch := localChans[m.uuid]
		for range m.expected {
			select {
			case r, ok := <-ch:
				if !ok {
					return nil, CreateRunwareError(
						"connectionFailed",
						"WebSocket connection closed while waiting for response",
						RunwareErrorDetails{},
					)
				}
				if r.err != nil {
					return nil, r.err
				}
				results = append(results, r.data)
			case <-ctx.Done():
				t.cleanupLocalChans(localChans)
				return nil, ctx.Err()
			case <-t.disconnected:
				return nil, CreateRunwareError(
					"connectionFailed",
					"WebSocket connection disconnected by client",
					RunwareErrorDetails{},
				)
			}
		}
	}

	return results, nil
}

// cleanupLocalChans removes all entries whose channel matches a local Send
// channel from the inflight maps. Called on write failure or context cancel.
func (t *WSTransport) cleanupLocalChans(localChans map[string]chan wsResult) {
	t.inflightMu.Lock()
	for uuid, entry := range t.inflight {
		if ch, ok := localChans[uuid]; ok && entry.ch == ch {
			removeFromTypeQueue(t.inflightByType, entry.taskType, uuid)
			delete(t.inflight, uuid)
		}
	}
	t.inflightMu.Unlock()
}

// reader is the background goroutine that reads all inbound WebSocket frames
// and dispatches each result item to the appropriate in-flight Send call.
// conn is passed by value so the goroutine holds its own reference.
// connCancel is called (via defer) when the reader exits, stopping keepAlive.
func (t *WSTransport) reader(conn *websocket.Conn, connCancel context.CancelFunc) {
	defer connCancel()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			// Connection closed or errored. Mark as disconnected and, if
			// appropriate, start a reconnect cycle. In-flight channels are
			// preserved so the server can replay results after reconnection.
			t.mu.Lock()
			isOurConn := t.conn == conn
			if isOurConn {
				t.connected = false
				t.conn = nil
				t.connCancel = nil
			}
			should := t.shouldReconnect
			alreadyReconnecting := t.reconnectCh != nil
			t.mu.Unlock()

			if isOurConn && should && !alreadyReconnecting {
				t.mu.Lock()
				ch := make(chan struct{})
				t.reconnectCh = ch
				t.mu.Unlock()
				go t.reconnectLoop(ch) //nolint:contextcheck
			} else if isOurConn && !should {
				// Disconnect() was called; fail all waiters so they don't hang.
				connErr := CreateRunwareError(
					"connectionFailed",
					fmt.Sprintf("WebSocket read error: %v", err),
					RunwareErrorDetails{},
				)
				t.broadcastErr(connErr)
			}
			return
		}

		// Update inactivity tracking.
		t.mu.Lock()
		t.lastActivity = time.Now()
		t.mu.Unlock()

		if t.logger != nil {
			t.logger.Debug("ws recv", "body", string(msg))
		}

		var envelope wsEnvelope
		if err := json.Unmarshal(msg, &envelope); err != nil {
			if t.logger != nil {
				t.logger.Debug("ws recv: failed to parse frame", "err", err, "body", string(msg))
			}
			continue
		}

		// Normalise: if no standard errors or data were found, try ParseAPIError
		// for the singular "error" shape and other non-standard forms.
		if len(envelope.Errors) == 0 && len(envelope.Data) == 0 {
			if e := ParseAPIError(msg, 0); e.Code != CodeUnknown {
				envelope.Errors = []RunwareError{*e}
			}
		}

		// Dispatch error items.
		for i := range envelope.Errors {
			e := &envelope.Errors[i]
			if e.TaskUUID != "" {
				t.dispatchByUUID(e.TaskUUID, wsResult{err: e})
			} else {
				t.broadcastErr(e)
			}
		}

		// Dispatch data items. Try UUID first; fall back to task type for
		// server frames that omit taskUUID (e.g. ping responses).
		for _, raw := range envelope.Data {
			var peek struct {
				TaskUUID string `json:"taskUUID"`
				TaskType string `json:"taskType"`
			}
			if err := json.Unmarshal(raw, &peek); err != nil {
				continue
			}
			switch {
			case peek.TaskUUID != "":
				t.dispatchByUUID(peek.TaskUUID, wsResult{data: raw})
			case peek.TaskType != "":
				t.dispatchByType(peek.TaskType, wsResult{data: raw})
				// else: malformed frame, silently drop
			}
		}
	}
}

// keepAlive sends periodic pings over conn to prevent the server from dropping
// an idle connection, and triggers a reconnect if no inbound activity has been
// seen for wsInactivityTimeout. It exits when ctx is cancelled.
func (t *WSTransport) keepAlive(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(t.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.disconnected:
			return
		case <-ticker.C:
			ping, _ := json.Marshal([]any{map[string]any{wsKeyTaskType: wsPingTaskType, wsPingTaskType: true}}) //nolint:errcheck
			t.writeMu.Lock()
			writeErr := conn.WriteMessage(websocket.TextMessage, ping)
			t.writeMu.Unlock()
			if writeErr != nil {
				// Connection is dead; reader will handle reconnect.
				return
			}

			t.mu.Lock()
			idle := time.Since(t.lastActivity)
			should := t.shouldReconnect
			alreadyReconnecting := t.reconnectCh != nil
			t.mu.Unlock()

			if idle > wsInactivityTimeout && should && !alreadyReconnecting {
				if t.logger != nil {
					t.logger.Debug("ws keepAlive: inactivity timeout, triggering reconnect", "idle", idle)
				}
				t.mu.Lock()
				ch := make(chan struct{})
				t.reconnectCh = ch
				t.connected = false
				t.conn = nil
				t.connCancel = nil
				t.mu.Unlock()
				conn.Close()           //nolint:errcheck,gosec
				go t.reconnectLoop(ch) //nolint:contextcheck,gosec
				return
			}
		}
	}
}

// reconnectLoop attempts to re-establish the WebSocket connection with
// exponential backoff and jitter. In-flight channels are preserved so the
// server can replay results via connectionSessionUUID after reconnection.
// ch is closed when the loop exits (on success or after exhausting attempts),
// waking any Send callers waiting on reconnectCh.
func (t *WSTransport) reconnectLoop(ch chan struct{}) {
	defer func() {
		t.mu.Lock()
		t.reconnectCh = nil
		t.mu.Unlock()
		close(ch)
	}()

	for {
		t.mu.Lock()
		if !t.shouldReconnect {
			t.mu.Unlock()
			return
		}
		t.reconnectAttempt++
		attempt := t.reconnectAttempt
		maxAttempts := t.maxReconnectAttempts
		baseDelay := t.reconnectBaseDelay
		t.mu.Unlock()

		if attempt > maxAttempts {
			if t.logger != nil {
				t.logger.Debug("ws reconnect: giving up", "attempts", attempt-1, "max", maxAttempts)
			}
			t.broadcastErr(CreateRunwareError(
				"connectionFailed",
				fmt.Sprintf("permanently disconnected after %d reconnect attempt(s)", attempt-1),
				RunwareErrorDetails{},
			))
			return
		}

		base := float64(baseDelay) * math.Pow(2, float64(attempt-1))
		jitter := rand.Float64() * float64(baseDelay) //nolint:gosec
		delay := time.Duration(math.Min(base+jitter, float64(wsReconnectMaxDelay)))

		if t.logger != nil {
			t.logger.Debug("ws reconnect: waiting before attempt",
				"attempt", attempt, "max", maxAttempts, "delay", delay.Round(time.Millisecond))
		}

		select {
		case <-time.After(delay):
		case <-t.disconnected:
			return
		}

		if err := t.Connect(context.Background()); err != nil { //nolint:contextcheck
			if t.logger != nil {
				t.logger.Debug("ws reconnect: attempt failed", "attempt", attempt, "err", err)
			}
			continue
		}

		// Success.
		t.mu.Lock()
		t.reconnectAttempt = 0
		t.mu.Unlock()

		if t.logger != nil {
			t.logger.Debug("ws reconnect: reconnected", "attempt", attempt)
		}
		return
	}
}

// dispatchByUUID sends r to the in-flight channel registered for uuid.
// The entry is removed from both lookup maps only when all expected results
// have been received, supporting multi-result tasks (numberResults > 1).
// On error delivery the entry is always removed immediately.
func (t *WSTransport) dispatchByUUID(uuid string, r wsResult) {
	t.inflightMu.Lock()
	entry, ok := t.inflight[uuid]
	if ok {
		entry.received++
		done := entry.received >= entry.expected || r.err != nil
		if done {
			delete(t.inflight, uuid)
			removeFromTypeQueue(t.inflightByType, entry.taskType, uuid)
		} else {
			t.inflight[uuid] = entry
		}
	}
	t.inflightMu.Unlock()
	if ok {
		entry.ch <- r
	} else if t.logger != nil {
		t.logger.Debug("ws recv: no inflight entry for UUID", "uuid", uuid)
	}
}

// dispatchByType dequeues the oldest in-flight request of the given task type
// (FIFO) and sends r to its channel. The entry is removed only when all
// expected results have been received.
func (t *WSTransport) dispatchByType(taskType string, r wsResult) {
	t.inflightMu.Lock()
	queue := t.inflightByType[taskType]
	if len(queue) == 0 {
		t.inflightMu.Unlock()
		return
	}
	uuid := queue[0]
	entry := t.inflight[uuid]
	entry.received++
	done := entry.received >= entry.expected || r.err != nil
	if done {
		if len(queue) == 1 {
			delete(t.inflightByType, taskType)
		} else {
			t.inflightByType[taskType] = queue[1:]
		}
		delete(t.inflight, uuid)
	} else {
		// Keep the UUID at the front of the queue and update received count.
		t.inflight[uuid] = entry
	}
	t.inflightMu.Unlock()
	entry.ch <- r
}

// broadcastErr sends err to every registered in-flight channel, then clears
// both lookup maps.
func (t *WSTransport) broadcastErr(err error) {
	t.inflightMu.Lock()
	chs := make([]chan wsResult, 0, len(t.inflight))
	for _, entry := range t.inflight {
		chs = append(chs, entry.ch)
	}
	t.inflight = make(map[string]inflightEntry)
	t.inflightByType = make(map[string][]string)
	t.inflightMu.Unlock()
	for _, ch := range chs {
		ch <- wsResult{err: err}
	}
}

// removeFromTypeQueue removes the first occurrence of uuid from
// inflightByType[taskType], deleting the key when the slice becomes empty.
func removeFromTypeQueue(m map[string][]string, taskType, uuid string) {
	queue := m[taskType]
	for i, u := range queue {
		if u == uuid {
			if len(queue) == 1 {
				delete(m, taskType)
			} else {
				m[taskType] = append(queue[:i], queue[i+1:]...)
			}
			return
		}
	}
}

// wsEnvelope is the JSON shape of every server-to-client WebSocket frame.
type wsEnvelope struct {
	Data   []json.RawMessage `json:"data"`
	Errors []RunwareError    `json:"errors,omitempty"`
}

// marshalAndExtractTaskMeta marshals tasks to JSON and returns the raw bytes
// alongside the UUID, task type, and expected result count of each task (in the
// same order). Tasks without a taskUUID are skipped (empty uuid in taskMeta).
func marshalAndExtractTaskMeta(tasks []any) ([]byte, []taskMeta, error) {
	body, err := json.Marshal(tasks)
	if err != nil {
		return nil, nil, err
	}

	var rawTasks []json.RawMessage
	if err := json.Unmarshal(body, &rawTasks); err != nil {
		return nil, nil, err
	}

	metas := make([]taskMeta, 0, len(rawTasks))
	for _, raw := range rawTasks {
		var item struct {
			TaskUUID      string `json:"taskUUID"`
			TaskType      string `json:"taskType"`
			NumberResults int    `json:"numberResults"`
		}
		if err := json.Unmarshal(raw, &item); err == nil && item.TaskUUID != "" {
			expected := item.NumberResults
			if expected < 1 {
				expected = 1
			}
			metas = append(metas, taskMeta{
				uuid:     item.TaskUUID,
				taskType: item.TaskType,
				expected: expected,
			})
		}
	}
	return body, metas, nil
}

// redactTasks returns body with any "apiKey" field in each task object replaced
// by "[redacted]". Returns the original bytes unchanged on any parse error.
func redactTasks(body []byte) []byte {
	var tasks []json.RawMessage
	if err := json.Unmarshal(body, &tasks); err != nil {
		return body
	}
	changed := false
	for i, raw := range tasks {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if _, ok := m["apiKey"]; ok {
			m["apiKey"] = json.RawMessage(`"[redacted]"`)
			b, err := json.Marshal(m)
			if err == nil {
				tasks[i] = b
				changed = true
			}
		}
	}
	if !changed {
		return body
	}
	out, err := json.Marshal(tasks)
	if err != nil {
		return body
	}
	return out
}
