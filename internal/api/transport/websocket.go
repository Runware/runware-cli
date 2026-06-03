package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/runware/runware-cli/internal/agents"
	"github.com/runware/runware-cli/internal/buildinfo"
)

const wsAuthTaskType = "authentication"
const wsKeyTaskType = "taskType"

// wsResult carries a single inbound result or an error for an in-flight Send.
type wsResult struct {
	data json.RawMessage
	err  error
}

// taskMeta holds the taskUUID and taskType extracted from a single outbound task.
type taskMeta struct {
	uuid     string
	taskType string
}

// inflightEntry associates an in-flight Send channel with its task type so we
// can clean up both lookup maps (by-UUID and by-type) atomically.
type inflightEntry struct {
	taskType string
	ch       chan wsResult
}

// WSTransport implements Transport over a persistent WebSocket connection.
//
// The first call to Send (or an explicit call to Connect) establishes the
// connection and authenticates with the API. Subsequent calls reuse the same
// connection. Call Disconnect when done to release resources.
//
// If the connection is lost, Send returns a CodeConnection RunwareError.
// Reconnection is the caller's responsibility (create a new WSTransport or
// call Connect again after Disconnect).
type WSTransport struct {
	apiKey    string
	baseURL   string
	userAgent string
	logger    *slog.Logger

	// mu guards conn, sessionUUID, connected, and done.
	mu          sync.Mutex
	conn        *websocket.Conn
	sessionUUID string // connectionSessionUUID for session resumption
	connected   bool
	done        chan struct{} // closed by Disconnect; recreated by Connect

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
func NewWSTransport(apiKey, baseURL string, logger *slog.Logger) *WSTransport {
	ua := buildinfo.UserAgent()
	if agent := agents.Detect(); agent != "" {
		ua += " agent/" + string(agent)
	}
	return &WSTransport{
		apiKey:         apiKey,
		baseURL:        baseURL,
		userAgent:      ua,
		logger:         logger,
		inflight:       make(map[string]inflightEntry),
		inflightByType: make(map[string][]string),
		done:           make(chan struct{}),
	}
}

// Connect establishes the WebSocket connection and authenticates with the API.
// It is safe to call multiple times; subsequent calls are no-ops if already connected.
// If a previous connectionSessionUUID is held, it is included in the auth request
// so the server can replay any buffered results.
func (t *WSTransport) Connect(ctx context.Context) error {
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

	// Read the auth response synchronously before starting the reader goroutine.
	_, msg, err := conn.ReadMessage()
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

	// Reset the done channel for this connection's lifecycle so that a
	// previous Disconnect + reconnect cycle works correctly.
	t.done = make(chan struct{})

	t.conn = conn
	t.connected = true

	go t.reader(conn)

	return nil
}

// Disconnect closes the WebSocket connection and unblocks any in-flight Send
// calls. Safe to call if Connect was never called.
func (t *WSTransport) Disconnect() error {
	t.mu.Lock()
	if !t.connected {
		t.mu.Unlock()
		return nil
	}
	conn := t.conn
	done := t.done
	t.connected = false
	t.conn = nil
	t.mu.Unlock()

	// Unblock any Send calls waiting on done. This must happen before
	// conn.Close() to avoid a race: closing done signals Send immediately,
	// then conn.Close() causes the reader goroutine to exit cleanly.
	close(done)

	// Close the connection without sending a close frame so we do not race
	// with a concurrent writeMu-protected Send write.
	conn.Close() //nolint:errcheck,gosec

	return nil
}

// Send marshals tasks, writes them to the WebSocket connection, and waits for
// a response for each task UUID. The connection is established lazily on the
// first call.
func (t *WSTransport) Send(ctx context.Context, tasks []any) ([]json.RawMessage, error) {
	if t.apiKey == "" {
		return nil, ErrNoAPIKey
	}

	// Lazy connect.
	t.mu.Lock()
	connected := t.connected
	t.mu.Unlock()
	if !connected {
		if err := t.Connect(ctx); err != nil {
			return nil, err
		}
	}

	// Marshal once; also extract task UUIDs and task types from the same pass.
	body, metas, err := marshalAndExtractTaskMeta(tasks)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if t.logger != nil && t.logger.Enabled(ctx, slog.LevelDebug) {
		t.logger.Debug("ws send", "body", string(body))
	}

	// Capture conn and done under mu so we have stable references for
	// the write and the select below. done may be replaced by a reconnect.
	t.mu.Lock()
	conn := t.conn
	done := t.done
	t.mu.Unlock()

	// Register in-flight channels before writing so the reader can dispatch
	// results immediately after the write completes. Tasks are registered in
	// both inflight (by UUID) and inflightByType (FIFO queue per task type)
	// so that responses without a taskUUID can still be dispatched by type.
	localChans := make(map[string]chan wsResult, len(metas)) // uuid → ch (for result collection)
	t.inflightMu.Lock()
	for _, m := range metas {
		if m.uuid == "" {
			continue
		}
		ch := make(chan wsResult, 1)
		localChans[m.uuid] = ch
		t.inflight[m.uuid] = inflightEntry{taskType: m.taskType, ch: ch}
		t.inflightByType[m.taskType] = append(t.inflightByType[m.taskType], m.uuid)
	}
	t.inflightMu.Unlock()

	// Write the message; serialised with writeMu (gorilla: one concurrent writer).
	t.writeMu.Lock()
	writeErr := conn.WriteMessage(websocket.TextMessage, body)
	t.writeMu.Unlock()

	if writeErr != nil {
		t.inflightMu.Lock()
		for uuid, entry := range t.inflight {
			if ch, ok := localChans[uuid]; ok && entry.ch == ch {
				removeFromTypeQueue(t.inflightByType, entry.taskType, uuid)
				delete(t.inflight, uuid)
			}
		}
		t.inflightMu.Unlock()
		return nil, CreateRunwareError(
			"connectionFailed",
			fmt.Sprintf("WebSocket write failed: %v", writeErr),
			RunwareErrorDetails{},
		)
	}

	// Collect results for all task UUIDs.
	results := make([]json.RawMessage, 0, len(metas))
	for _, m := range metas {
		if m.uuid == "" {
			continue
		}
		ch := localChans[m.uuid]
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
			t.inflightMu.Lock()
			for uuid, entry := range t.inflight {
				if ch2, ok := localChans[uuid]; ok && entry.ch == ch2 {
					removeFromTypeQueue(t.inflightByType, entry.taskType, uuid)
					delete(t.inflight, uuid)
				}
			}
			t.inflightMu.Unlock()
			return nil, ctx.Err()
		case <-done:
			return nil, CreateRunwareError(
				"connectionFailed",
				"WebSocket connection closed while waiting for response",
				RunwareErrorDetails{},
			)
		}
	}

	return results, nil
}

// reader is the background goroutine that reads all inbound WebSocket frames
// and dispatches each result item to the appropriate in-flight Send call.
// conn is passed by value so the goroutine holds its own reference and is
// not affected by Disconnect setting t.conn to nil.
func (t *WSTransport) reader(conn *websocket.Conn) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			// Connection closed or errored; fail all in-flight requests.
			connErr := CreateRunwareError(
				"connectionFailed",
				fmt.Sprintf("WebSocket read error: %v", err),
				RunwareErrorDetails{},
			)
			t.inflightMu.Lock()
			chs := make([]chan wsResult, 0, len(t.inflight))
			for _, entry := range t.inflight {
				chs = append(chs, entry.ch)
			}
			t.inflight = make(map[string]inflightEntry)
			t.inflightByType = make(map[string][]string)
			t.inflightMu.Unlock()
			for _, ch := range chs {
				ch <- wsResult{err: connErr}
			}

			// Mark as disconnected so the next Send triggers a reconnect,
			// but only if Disconnect hasn't already done so.
			t.mu.Lock()
			if t.conn == conn {
				t.connected = false
				t.conn = nil
			}
			t.mu.Unlock()
			return
		}

		if t.logger != nil {
			t.logger.Debug("ws recv", "body", string(msg)) //nolint:errcheck,gosec
		}

		var envelope wsEnvelope
		if err := json.Unmarshal(msg, &envelope); err != nil {
			if t.logger != nil {
				t.logger.Debug("ws recv: failed to parse frame", "err", err, "body", string(msg))
			}
			continue
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

// dispatchByUUID sends r to the in-flight channel registered for uuid, then
// removes it from both the uuid and type-keyed maps.
func (t *WSTransport) dispatchByUUID(uuid string, r wsResult) {
	t.inflightMu.Lock()
	entry, ok := t.inflight[uuid]
	if ok {
		delete(t.inflight, uuid)
		removeFromTypeQueue(t.inflightByType, entry.taskType, uuid)
	}
	t.inflightMu.Unlock()
	if ok {
		entry.ch <- r
	}
}

// dispatchByType dequeues the oldest in-flight request of the given task type
// (FIFO) and sends r to its channel.
func (t *WSTransport) dispatchByType(taskType string, r wsResult) {
	t.inflightMu.Lock()
	queue := t.inflightByType[taskType]
	if len(queue) == 0 {
		t.inflightMu.Unlock()
		return
	}
	uuid := queue[0]
	if len(queue) == 1 {
		delete(t.inflightByType, taskType)
	} else {
		t.inflightByType[taskType] = queue[1:]
	}
	entry := t.inflight[uuid]
	delete(t.inflight, uuid)
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
// alongside the taskUUID and taskType of each task (in the same order).
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
			TaskUUID string `json:"taskUUID"`
			TaskType string `json:"taskType"`
		}
		if err := json.Unmarshal(raw, &item); err == nil && item.TaskUUID != "" {
			metas = append(metas, taskMeta{uuid: item.TaskUUID, taskType: item.TaskType})
		}
	}
	return body, metas, nil
}
