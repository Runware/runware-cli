package transport

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Compile-time check: WSTransport implements Transport.
var _ Transport = (*WSTransport)(nil)

const (
	testTaskTypePing = "ping"
	testKeyTaskType  = "taskType"
	testKeyTaskUUID  = "taskUUID"
)

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

func authSuccessReply(sessionUUID string) []byte {
	b, _ := json.Marshal(map[string]any{
		"data": []any{
			map[string]any{
				testKeyTaskType:         wsAuthTaskType,
				"connectionSessionUUID": sessionUUID,
			},
		},
	})
	return b
}

func authErrorReply(code, message string) []byte {
	b, _ := json.Marshal(map[string]any{
		"errors": []any{
			map[string]any{
				"code":          code,
				"message":       message,
				testKeyTaskType: wsAuthTaskType,
			},
		},
	})
	return b
}

// --- Tests ---

func TestWSTransport_ErrNoAPIKey(t *testing.T) {
	tr := NewWSTransport("", "ws://localhost", slog.Default())
	_, err := tr.Send(context.Background(), []any{})
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestWSTransport_ConnectAuthFailure(t *testing.T) {
	srv := newWSTestServer(authErrorReply("invalidApiKey", "invalid key"))
	defer srv.server.Close()

	tr := NewWSTransport("bad-key", wsURL(srv.server), slog.Default())
	err := tr.Connect(context.Background())
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
	if !IsAuthError(err) {
		t.Errorf("expected IsAuthError=true, got %v", err)
	}
}

func TestWSTransport_ConnectSuccess(t *testing.T) {
	srv := newWSTestServer(authSuccessReply("test-session-uuid"))
	defer srv.server.Close()

	tr := NewWSTransport("valid-key", wsURL(srv.server), slog.Default())
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("unexpected Connect error: %v", err)
	}
	if tr.sessionUUID != "test-session-uuid" {
		t.Errorf("expected sessionUUID=%q, got %q", "test-session-uuid", tr.sessionUUID)
	}
	tr.Disconnect() //nolint:errcheck,gosec
}

func TestWSTransport_ConnectIdempotent(t *testing.T) {
	srv := newWSTestServer(authSuccessReply("sid"))
	defer srv.server.Close()

	tr := NewWSTransport("key", wsURL(srv.server), slog.Default())
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	// Second Connect should be a no-op, not fail.
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	tr.Disconnect() //nolint:errcheck,gosec
}

func TestWSTransport_SendReceive(t *testing.T) {
	const taskUUID = "aaaaaaaa-0000-0000-0000-000000000001"
	resultFrame, _ := json.Marshal(map[string]any{
		"data": []any{
			map[string]any{
				testKeyTaskType: testTaskTypePing,
				testKeyTaskUUID: taskUUID,
				"pong":          true,
			},
		},
	})

	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		// Auth handshake.
		conn.ReadMessage()                                                //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply("sid")) //nolint:errcheck,gosec

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

	tr := NewWSTransport("key", wsURL(srv.server), slog.Default())
	defer tr.Disconnect() //nolint:errcheck,gosec

	tasks := []any{map[string]any{testKeyTaskType: testTaskTypePing, testKeyTaskUUID: taskUUID, "ping": true}}
	results, err := tr.Send(context.Background(), tasks)
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	var got map[string]any
	if err := json.Unmarshal(results[0], &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got["pong"] != true {
		t.Errorf("expected pong=true in result, got %v", got)
	}
}

func TestWSTransport_SendServerError(t *testing.T) {
	const taskUUID = "aaaaaaaa-0000-0000-0000-000000000002"
	errFrame, _ := json.Marshal(map[string]any{
		"errors": []any{
			map[string]any{
				"code":          "quotaExceeded",
				"message":       "quota exceeded",
				testKeyTaskUUID: taskUUID,
			},
		},
	})

	srv := newWSTestServer(nil)
	srv.handler = func(conn *websocket.Conn) {
		conn.ReadMessage()                                                //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply("sid")) //nolint:errcheck,gosec
		conn.ReadMessage()                                                //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, errFrame)                //nolint:errcheck,gosec
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
	defer srv.server.Close()

	tr := NewWSTransport("key", wsURL(srv.server), slog.Default())
	defer tr.Disconnect() //nolint:errcheck,gosec

	tasks := []any{map[string]any{testKeyTaskType: "inference", testKeyTaskUUID: taskUUID}}
	_, err := tr.Send(context.Background(), tasks)
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
		conn.ReadMessage()                                                //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply("sid")) //nolint:errcheck,gosec
		// Read the task frame but never reply.
		conn.ReadMessage() //nolint:errcheck,gosec
		// Keep connection open so the client times out via context.
		time.Sleep(5 * time.Second)
	}
	defer srv.server.Close()

	tr := NewWSTransport("key", wsURL(srv.server), slog.Default())
	defer tr.Disconnect() //nolint:errcheck,gosec

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	tasks := []any{map[string]any{testKeyTaskType: testTaskTypePing, testKeyTaskUUID: taskUUID}}
	_, err := tr.Send(ctx, tasks)
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
		conn.ReadMessage()                                                //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply("sid")) //nolint:errcheck,gosec
		conn.ReadMessage()                                                //nolint:errcheck,gosec
		close(serverReceivedTask)
		// Never reply — Disconnect should unblock the Send.
		time.Sleep(5 * time.Second)
	}
	defer srv.server.Close()

	tr := NewWSTransport("key", wsURL(srv.server), slog.Default())

	tasks := []any{map[string]any{testKeyTaskType: testTaskTypePing, testKeyTaskUUID: taskUUID}}

	errCh := make(chan error, 1)
	go func() {
		_, err := tr.Send(context.Background(), tasks)
		errCh <- err
	}()

	// Wait until the server has received the task, then disconnect.
	<-serverReceivedTask
	tr.Disconnect() //nolint:errcheck,gosec

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected connection error after Disconnect, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Send did not unblock after Disconnect")
	}
}

func TestWSTransport_LazyConnect(t *testing.T) {
	srv := newWSTestServer(authSuccessReply("sid"))
	// Override to also echo back a result for the send.
	const taskUUID = "aaaaaaaa-0000-0000-0000-000000000005"
	resultFrame, _ := json.Marshal(map[string]any{
		"data": []any{map[string]any{testKeyTaskType: testTaskTypePing, testKeyTaskUUID: taskUUID, "pong": true}},
	})
	srv.handler = func(conn *websocket.Conn) {
		conn.ReadMessage()                                                //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, authSuccessReply("sid")) //nolint:errcheck,gosec
		conn.ReadMessage()                                                //nolint:errcheck,gosec
		conn.WriteMessage(websocket.TextMessage, resultFrame)             //nolint:errcheck,gosec
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
	defer srv.server.Close()

	tr := NewWSTransport("key", wsURL(srv.server), slog.Default())
	defer tr.Disconnect() //nolint:errcheck,gosec

	// Call Send without a prior Connect — should connect lazily.
	tasks := []any{map[string]any{testKeyTaskType: testTaskTypePing, testKeyTaskUUID: taskUUID}}
	results, err := tr.Send(context.Background(), tasks)
	if err != nil {
		t.Fatalf("lazy Send: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}
