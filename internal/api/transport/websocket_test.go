package transport

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Compile-time check: WSTransport implements Transport.
var _ Transport = (*WSTransport)(nil)

const (
	testTaskTypePing      = "ping"
	testTaskTypeInference = "inference"
)

// testTask is a typed outbound task for transport tests.
type testTask struct {
	TaskType      string `json:"taskType"`
	TaskUUID      string `json:"taskUUID"`
	NumberResults int    `json:"numberResults,omitempty"`
	Ping          bool   `json:"ping,omitempty"`
}

// testResultItem is a generic typed item for test server data frames.
type testResultItem struct {
	TaskType string `json:"taskType"`
	TaskUUID string `json:"taskUUID,omitempty"`
	Pong     bool   `json:"pong,omitempty"`
	Seq      int    `json:"seq,omitempty"`
	V        int    `json:"v,omitempty"`
	Done     bool   `json:"done,omitempty"`
}

// testErrorItem is a wire-format error item for test server error frames.
// RunwareError cannot be used here: it has no JSON marshal tags and no MarshalJSON.
type testErrorItem struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	TaskType string `json:"taskType,omitempty"`
	TaskUUID string `json:"taskUUID,omitempty"`
}

// testErrorEnvelope wraps error items in the standard {"errors":[...]} frame.
type testErrorEnvelope struct {
	Errors []testErrorItem `json:"errors"`
}

// testSingularErrorEnvelope wraps a single error in the {"error":{...}} frame.
type testSingularErrorEnvelope struct {
	Error testErrorItem `json:"error"`
}

// mustMarshal marshals v to a json.RawMessage, failing the test immediately on error.
func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustMarshal: %v", err)
	}
	return b
}

// makeDataFrame builds a {"data":[...]} server frame using wsEnvelope.
func makeDataFrame(t *testing.T, items ...any) []byte {
	t.Helper()
	raws := make([]json.RawMessage, len(items))
	for i, item := range items {
		raws[i] = mustMarshal(t, item)
	}
	return mustMarshal(t, wsEnvelope{Data: raws})
}

// wsTestServer is a minimal WebSocket test server that handles the auth
// handshake and allows the test to enqueue frames to send back to the client.
type wsTestServer struct {
	server    *httptest.Server
	upgrader  websocket.Upgrader
	authReply []byte                // what to send after receiving the auth frame
	handler   func(*websocket.Conn) // optional; overrides default if set
}

func newWSTestServer(authReply []byte) *wsTestServer {
	ts := &wsTestServer{
		authReply: authReply,
		upgrader:  websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
	}
	ts.server = httptest.NewServer(http.HandlerFunc(ts.serve))
	return ts
}

func (ts *wsTestServer) serve(w http.ResponseWriter, r *http.Request) {
	conn, err := ts.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close() //nolint:errcheck,gosec

	if ts.handler != nil {
		ts.handler(conn)
		return
	}

	// Default: read the auth frame and reply with authReply.
	_, _, err = conn.ReadMessage()
	if err != nil {
		return
	}
	conn.WriteMessage(websocket.TextMessage, ts.authReply) //nolint:errcheck,gosec
	// Keep connection open until client closes.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

// wsURL converts an httptest server URL to a ws:// URL.
func wsURL(s *httptest.Server) string {
	return "ws" + strings.TrimPrefix(s.URL, "http")
}

func authSuccessReply(t *testing.T, sessionUUID string) []byte {
	t.Helper()
	return makeDataFrame(t, wsAuthData{TaskType: wsAuthTaskType, ConnectionSessionUUID: sessionUUID})
}

func authErrorReply(t *testing.T, code, message string) []byte {
	t.Helper()
	return mustMarshal(t, testErrorEnvelope{Errors: []testErrorItem{
		{Code: code, Message: message, TaskType: wsAuthTaskType},
	}})
}

// --- Existing tests (unchanged behaviour) ---

func TestWSTransport_ErrNoAPIKey(t *testing.T) {
	_, err := DialWS(context.Background(), "", "ws://localhost", slog.Default())
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestWSTransport_DialAuthFailure(t *testing.T) {
	srv := newWSTestServer(authErrorReply(t, "invalidApiKey", "invalid key"))
	defer srv.server.Close()

	_, err := DialWS(context.Background(), "bad-key", wsURL(srv.server), slog.Default())
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
	if !IsAuthError(err) {
		t.Errorf("expected IsAuthError=true, got %v", err)
	}
}

func TestWSTransport_DialSuccess(t *testing.T) {
	srv := newWSTestServer(authSuccessReply(t, "test-session-uuid"))
	defer srv.server.Close()

	tr, err := DialWS(context.Background(), "valid-key", wsURL(srv.server), slog.Default())
	if err != nil {
		t.Fatalf("unexpected DialWS error: %v", err)
	}
	if tr.sessionUUID != "test-session-uuid" {
		t.Errorf("expected sessionUUID=%q, got %q", "test-session-uuid", tr.sessionUUID)
	}
	tr.Close() //nolint:errcheck,gosec
}

func TestWSTransport_SendReceive(t *testing.T) {
	const taskUUID = "aaaaaaaa-0000-0000-0000-000000000001"
	resultFrame := makeDataFrame(t, testResultItem{
		TaskType: testTaskTypePing,
		TaskUUID: taskUUID,
		Pong:     true,
	})

	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		// Auth handshake.
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply(t, "sid")) //nolint:errcheck,gosec

		// Receive task frame, reply with result.
		conn.ReadMessage()                                    //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, resultFrame) //nolint:errcheck,gosec

		// Keep open.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
	defer srv.server.Close()

	tr, err := DialWS(context.Background(), "key", wsURL(srv.server), slog.Default())
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}
	defer tr.Close() //nolint:errcheck,gosec

	tasks := []any{testTask{TaskType: testTaskTypePing, TaskUUID: taskUUID, Ping: true}}
	results, err := tr.Send(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	var got testResultItem
	if err := json.Unmarshal(results[0], &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !got.Pong {
		t.Errorf("expected pong=true in result, got %v", got.Pong)
	}
}

func TestWSTransport_SendServerError(t *testing.T) {
	const taskUUID = "aaaaaaaa-0000-0000-0000-000000000002"
	errFrame := mustMarshal(t, testErrorEnvelope{Errors: []testErrorItem{
		{Code: "quotaExceeded", Message: "quota exceeded", TaskUUID: taskUUID},
	}})

	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply(t, "sid")) //nolint:errcheck,gosec
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, errFrame)                   //nolint:errcheck,gosec
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
	defer srv.server.Close()

	tr, err := DialWS(context.Background(), "key", wsURL(srv.server), slog.Default())
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}
	defer tr.Close() //nolint:errcheck,gosec

	tasks := []any{testTask{TaskType: testTaskTypeInference, TaskUUID: taskUUID}}
	_, err = tr.Send(context.Background(), tasks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var re *RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *RunwareError, got %T: %v", err, err)
	}
	if re.Code != CodeQuota {
		t.Errorf("expected CodeQuota, got %v", re.Code)
	}
}

func TestWSTransport_ContextCancellation(t *testing.T) {
	const taskUUID = "aaaaaaaa-0000-0000-0000-000000000003"

	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply(t, "sid")) //nolint:errcheck,gosec
		// Read the task frame but never reply.
		conn.ReadMessage() //nolint:errcheck,gosec
		// Keep connection open so the client times out via context.
		time.Sleep(5 * time.Second)
	}
	defer srv.server.Close()

	tr, err := DialWS(context.Background(), "key", wsURL(srv.server), slog.Default())
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}
	defer tr.Close() //nolint:errcheck,gosec

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	tasks := []any{testTask{TaskType: testTaskTypePing, TaskUUID: taskUUID}}
	_, err = tr.Send(ctx, tasks)
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if ctx.Err() == nil {
		t.Errorf("expected context to be done, but it is not; err=%v", err)
	}
}

func TestWSTransport_DisconnectDrainsInflight(t *testing.T) {
	const taskUUID = "aaaaaaaa-0000-0000-0000-000000000004"

	// This channel lets the test synchronise with the server handler.
	serverReceivedTask := make(chan struct{})

	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply(t, "sid")) //nolint:errcheck,gosec
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		close(serverReceivedTask)
		// Never reply — Disconnect should unblock the Send.
		time.Sleep(5 * time.Second)
	}
	defer srv.server.Close()

	tr, err := DialWS(context.Background(), "key", wsURL(srv.server), slog.Default())
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}

	tasks := []any{testTask{TaskType: testTaskTypePing, TaskUUID: taskUUID}}

	errCh := make(chan error, 1)
	go func() {
		_, err := tr.Send(context.Background(), tasks)
		errCh <- err
	}()

	// Wait until the server has received the task, then close.
	<-serverReceivedTask
	tr.Close() //nolint:errcheck,gosec

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected connection error after Close, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Send did not unblock after Close")
	}
}

// TestWSTransport_NoUUIDResponseDispatch verifies that a server response
// carrying only a taskType (no taskUUID) is still dispatched correctly to the
// waiting Send call via the FIFO type queue.
func TestWSTransport_NoUUIDResponseDispatch(t *testing.T) {
	const taskUUID = "aaaaaaaa-0000-0000-0000-000000000006"

	// Server responds with taskType but no taskUUID — the exact ping shape.
	pingResponseNoUUID := makeDataFrame(t, testResultItem{
		TaskType: testTaskTypePing,
		Pong:     true,
		// intentionally no TaskUUID
	})

	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply(t, "sid")) //nolint:errcheck,gosec
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, pingResponseNoUUID)         //nolint:errcheck,gosec
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
	defer srv.server.Close()

	tr, err := DialWS(context.Background(), "key", wsURL(srv.server), slog.Default())
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}
	defer tr.Close() //nolint:errcheck,gosec

	tasks := []any{testTask{TaskType: testTaskTypePing, TaskUUID: taskUUID, Ping: true}}
	results, err := tr.Send(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	var got testResultItem
	if err := json.Unmarshal(results[0], &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !got.Pong {
		t.Errorf("expected pong=true in result, got %v", got.Pong)
	}
}

// TestWSTransport_DialAuthNoSession verifies that DialWS fails when the
// server's auth response is missing connectionSessionUUID.
func TestWSTransport_DialAuthNoSession(t *testing.T) {
	noSessionReply := makeDataFrame(t, wsAuthData{TaskType: wsAuthTaskType})
	srv := newWSTestServer(noSessionReply)
	defer srv.server.Close()

	_, err := DialWS(context.Background(), "key", wsURL(srv.server), slog.Default())
	if err == nil {
		t.Fatal("expected error for missing connectionSessionUUID, got nil")
	}
	var re *RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *RunwareError, got %T: %v", err, re)
	}
	if re.Code != CodeAuth {
		t.Errorf("expected CodeAuth, got %v", re.Code)
	}
}

// TestWSTransport_DialTimeout verifies that DialWS returns promptly when the
// server never replies to the auth frame, using the context deadline.
func TestWSTransport_DialTimeout(t *testing.T) {
	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		conn.ReadMessage() //nolint:errcheck,gosec // read auth, never reply
		time.Sleep(5 * time.Second)
	}
	defer srv.server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := DialWS(ctx, "key", wsURL(srv.server), slog.Default())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed > 2*time.Second {
		t.Errorf("DialWS took too long (%v); expected < 2s", elapsed)
	}
}

// TestWSTransport_CloseIsTerminal verifies that Send returns an error
// after Close has been called.
func TestWSTransport_CloseIsTerminal(t *testing.T) {
	srv := newWSTestServer(authSuccessReply(t, "sid"))
	defer srv.server.Close()

	tr, err := DialWS(context.Background(), "key", wsURL(srv.server), slog.Default())
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}
	tr.Close() //nolint:errcheck,gosec

	_, err = tr.Send(context.Background(), []any{})
	if err == nil {
		t.Fatal("expected error after Close, got nil")
	}
}

// TestWSTransport_SingularErrorField verifies that a server frame using the
// singular {"error":{...}} shape (rather than {"errors":[...]}) is correctly
// routed to the waiting Send call.
func TestWSTransport_SingularErrorField(t *testing.T) {
	const taskUUID = "aaaaaaaa-0000-0000-0000-000000000007"
	singularErrFrame := mustMarshal(t, testSingularErrorEnvelope{
		Error: testErrorItem{Code: "quotaExceeded", Message: "quota exceeded", TaskUUID: taskUUID},
	})

	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply(t, "sid")) //nolint:errcheck,gosec
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, singularErrFrame)           //nolint:errcheck,gosec
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
	defer srv.server.Close()

	tr, err := DialWS(context.Background(), "key", wsURL(srv.server), slog.Default())
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}
	defer tr.Close() //nolint:errcheck,gosec

	tasks := []any{testTask{TaskType: testTaskTypeInference, TaskUUID: taskUUID}}
	_, err = tr.Send(context.Background(), tasks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var re *RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *RunwareError, got %T: %v", err, err)
	}
	if re.Code != CodeQuota {
		t.Errorf("expected CodeQuota, got %v", re.Code)
	}
}

// TestWSTransport_MultiResult verifies that a task with numberResults:3 collects
// all three items delivered in a single server frame before Send returns.
func TestWSTransport_MultiResult(t *testing.T) {
	const taskUUID = "aaaaaaaa-0000-0000-0000-000000000008"
	multiFrame := makeDataFrame(t,
		testResultItem{TaskType: testTaskTypeInference, TaskUUID: taskUUID, Seq: 1},
		testResultItem{TaskType: testTaskTypeInference, TaskUUID: taskUUID, Seq: 2},
		testResultItem{TaskType: testTaskTypeInference, TaskUUID: taskUUID, Seq: 3},
	)

	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply(t, "sid")) //nolint:errcheck,gosec
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, multiFrame)                 //nolint:errcheck,gosec
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
	defer srv.server.Close()

	tr, err := DialWS(context.Background(), "key", wsURL(srv.server), slog.Default())
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}
	defer tr.Close() //nolint:errcheck,gosec

	tasks := []any{testTask{TaskType: testTaskTypeInference, TaskUUID: taskUUID, NumberResults: 3}}
	results, err := tr.Send(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for i, raw := range results {
		var got testResultItem
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal result %d: %v", i, err)
		}
		if got.Seq != i+1 {
			t.Errorf("result %d: expected seq=%d, got %d", i, i+1, got.Seq)
		}
	}
}

// TestWSTransport_MultiResultMultiFrame verifies that a task with numberResults:3
// collects all three items even when the server delivers them across two frames.
func TestWSTransport_MultiResultMultiFrame(t *testing.T) {
	const taskUUID = "aaaaaaaa-0000-0000-0000-000000000009"
	frame1 := makeDataFrame(t,
		testResultItem{TaskType: testTaskTypeInference, TaskUUID: taskUUID, Seq: 1},
		testResultItem{TaskType: testTaskTypeInference, TaskUUID: taskUUID, Seq: 2},
	)
	frame2 := makeDataFrame(t,
		testResultItem{TaskType: testTaskTypeInference, TaskUUID: taskUUID, Seq: 3},
	)

	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply(t, "sid")) //nolint:errcheck,gosec
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, frame1)                     //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, frame2)                     //nolint:errcheck,gosec
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
	defer srv.server.Close()

	tr, err := DialWS(context.Background(), "key", wsURL(srv.server), slog.Default())
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}
	defer tr.Close() //nolint:errcheck,gosec

	tasks := []any{testTask{TaskType: testTaskTypeInference, TaskUUID: taskUUID, NumberResults: 3}}
	results, err := tr.Send(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
}

// TestWSTransport_PingSent verifies that keepAlive sends a ping frame to the
// server within the configured ping interval.
func TestWSTransport_PingSent(t *testing.T) {
	pingSeen := make(chan struct{}, 1)

	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply(t, "sid")) //nolint:errcheck,gosec
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var frames []wsHeartbeatRequest
			if json.Unmarshal(msg, &frames) == nil {
				for _, f := range frames {
					if f.TaskType == wsPingTaskType {
						select {
						case pingSeen <- struct{}{}:
						default:
						}
					}
				}
			}
		}
	}
	defer srv.server.Close()

	tr, err := DialWS(context.Background(), "key", wsURL(srv.server), slog.Default(),
		WithPingInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}
	defer tr.Close() //nolint:errcheck,gosec

	select {
	case <-pingSeen:
		// pass
	case <-time.After(500 * time.Millisecond):
		t.Fatal("keepAlive ping not sent within timeout")
	}
}

// TestWSTransport_AutoReconnect verifies that after a connection drop the
// transport reconnects automatically and subsequent Send calls succeed.
func TestWSTransport_AutoReconnect(t *testing.T) {
	const (
		taskUUID1 = "aaaaaaaa-0000-0000-0000-00000000000a"
		taskUUID2 = "aaaaaaaa-0000-0000-0000-00000000000b"
	)
	result1 := makeDataFrame(t, testResultItem{TaskType: testTaskTypeInference, TaskUUID: taskUUID1, V: 1})
	result2 := makeDataFrame(t, testResultItem{TaskType: testTaskTypeInference, TaskUUID: taskUUID2, V: 2})

	var (
		connMu    sync.Mutex
		connCount int
	)
	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		connMu.Lock()
		connCount++
		n := connCount
		connMu.Unlock()

		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply(t, "sid")) //nolint:errcheck,gosec
		conn.ReadMessage()                                                   //nolint:errcheck,gosec

		if n == 1 {
			// Reply and immediately drop the connection.
			conn.WriteMessage(websocket.TextMessage, result1) //nolint:errcheck,gosec
			conn.Close()                                      //nolint:errcheck,gosec
			return
		}
		// Second+ connection: serve task2.
		conn.WriteMessage(websocket.TextMessage, result2) //nolint:errcheck,gosec
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
	defer srv.server.Close()

	tr, err := DialWS(context.Background(), "key", wsURL(srv.server), slog.Default(),
		WithReconnectBaseDelay(10*time.Millisecond))
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}
	defer tr.Close() //nolint:errcheck,gosec

	// First send — succeeds on the first connection.
	tasks1 := []any{testTask{TaskType: testTaskTypeInference, TaskUUID: taskUUID1}}
	if _, err := tr.Send(context.Background(), tasks1); err != nil {
		t.Fatalf("first Send: %v", err)
	}

	// Give the transport time to detect the drop and reconnect.
	time.Sleep(200 * time.Millisecond)

	// Second send — must succeed on the new connection.
	tasks2 := []any{testTask{TaskType: testTaskTypeInference, TaskUUID: taskUUID2}}
	results, err := tr.Send(context.Background(), tasks2)
	if err != nil {
		t.Fatalf("second Send after reconnect: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// TestWSTransport_SessionResumption verifies that an in-flight Send survives a
// connection drop and receives the result after reconnection, simulating the
// server replaying results via connectionSessionUUID.
func TestWSTransport_SessionResumption(t *testing.T) {
	const taskUUID = "aaaaaaaa-0000-0000-0000-00000000000c"
	resultFrame := makeDataFrame(t, testResultItem{TaskType: testTaskTypeInference, TaskUUID: taskUUID, Done: true})

	serverGotTask := make(chan struct{}) // fired when first conn reads the task frame
	dropConn := make(chan struct{})      // test signals to drop the connection

	var (
		connMu    sync.Mutex
		connCount int
	)
	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		connMu.Lock()
		connCount++
		n := connCount
		connMu.Unlock()

		conn.ReadMessage()                                                   //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply(t, "sid")) //nolint:errcheck,gosec

		if n == 1 {
			conn.ReadMessage() //nolint:errcheck,gosec // task frame
			close(serverGotTask)
			<-dropConn   // wait for test to initiate drop
			conn.Close() //nolint:errcheck,gosec // simulate connection drop
			return
		}
		// Second connection: immediately replay the result (simulating server
		// session resumption via connectionSessionUUID).
		conn.WriteMessage(websocket.TextMessage, resultFrame) //nolint:errcheck,gosec
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
	defer srv.server.Close()

	tr, err := DialWS(context.Background(), "key", wsURL(srv.server), slog.Default(),
		WithReconnectBaseDelay(10*time.Millisecond))
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}
	defer tr.Close() //nolint:errcheck,gosec

	tasks := []any{testTask{TaskType: testTaskTypeInference, TaskUUID: taskUUID}}

	type sendResult struct {
		results []json.RawMessage
		err     error
	}
	done := make(chan sendResult, 1)
	go func() {
		results, err := tr.Send(context.Background(), tasks)
		done <- sendResult{results, err}
	}()

	// Wait for the server to receive the task, then drop the connection.
	<-serverGotTask
	close(dropConn)

	// Send should survive the reconnect and receive the replayed result.
	select {
	case sr := <-done:
		if sr.err != nil {
			t.Fatalf("Send failed after reconnect: %v", sr.err)
		}
		if len(sr.results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(sr.results))
		}
		var got testResultItem
		if err := json.Unmarshal(sr.results[0], &got); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if !got.Done {
			t.Errorf("expected done=true in result, got %v", got.Done)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send did not complete after reconnect within 5s")
	}
}

// TestWSTransport_ReconnectGivesUpAfterMaxAttempts verifies that when all
// reconnect attempts fail the in-flight Send call receives a connection error.
func TestWSTransport_ReconnectGivesUpAfterMaxAttempts(t *testing.T) {
	const taskUUID = "aaaaaaaa-0000-0000-0000-00000000000d"

	var (
		connMu    sync.Mutex
		connCount int
	)
	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		connMu.Lock()
		connCount++
		n := connCount
		connMu.Unlock()

		if n == 1 {
			// First connection: complete auth, receive the task, then drop.
			conn.ReadMessage()                                                   //nolint:errcheck,gosec
			conn.WriteMessage(websocket.TextMessage, authSuccessReply(t, "sid")) //nolint:errcheck,gosec
			conn.ReadMessage()                                                   //nolint:errcheck,gosec // task frame
			conn.Close()                                                         //nolint:errcheck,gosec
			return
		}
		// Subsequent connections: read the auth frame but close without
		// replying so that Connect fails on the auth response read.
		conn.ReadMessage() //nolint:errcheck,gosec
		conn.Close()       //nolint:errcheck,gosec
	}
	defer srv.server.Close()

	const maxAttempts = 2
	tr, err := DialWS(context.Background(), "key", wsURL(srv.server), slog.Default(),
		WithMaxReconnectAttempts(maxAttempts),
		WithReconnectBaseDelay(10*time.Millisecond))
	if err != nil {
		t.Fatalf("DialWS: %v", err)
	}
	defer tr.Close() //nolint:errcheck,gosec

	tasks := []any{testTask{TaskType: testTaskTypeInference, TaskUUID: taskUUID}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = tr.Send(ctx, tasks)
	if err == nil {
		t.Fatal("expected error after reconnect exhaustion, got nil")
	}
	var re *RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *RunwareError, got %T: %v", err, err)
	}
	if re.Code != CodeConnection {
		t.Errorf("expected CodeConnection, got %v", re.Code)
	}
}
