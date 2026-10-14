package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/spf13/cobra"
)

const (
	validKey   = "valid-key"
	invalidKey = "invalid-key"

	invalidKeyReply   = `{"errors":[{"code":"invalidApiKey","message":"invalid key"}]}`
	serverErrorReply  = `{"errors":[{"code":"internalServerError","message":"boom"}]}`
	pongReply         = `{"data":[{"taskType":"ping","pong":true}]}`
	wsAuthReply       = `{"data":[{"taskType":"authentication","connectionSessionUUID":"session"}]}`
	wsAuthNoSessReply = `{"data":[{"taskType":"authentication"}]}`
)

// fakeAPI answers ping over exactly one transport, accepting only validKey.
type fakeAPI struct {
	upgrader websocket.Upgrader
	// scheme is the only transport this fake speaks; the other one is refused.
	scheme string
	// broken makes the API accept the key but answer unusably: HTTP 500, or a
	// WebSocket auth reply with no connectionSessionUUID.
	broken bool
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if websocket.IsWebSocketUpgrade(r) {
		if f.scheme != transport.SchemeWS {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.serveWS(w, r)
		return
	}
	if f.scheme != transport.SchemeHTTP {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+validKey {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(invalidKeyReply))
		return
	}
	if f.broken {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(serverErrorReply))
		return
	}
	_, _ = w.Write([]byte(pongReply))
}

type wsAuthFrame struct {
	APIKey string `json:"apiKey"`
}

func (f *fakeAPI) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := f.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close() //nolint:errcheck

	var auth []wsAuthFrame
	if err := conn.ReadJSON(&auth); err != nil || len(auth) == 0 {
		return
	}
	if auth[0].APIKey != validKey {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(invalidKeyReply))
		return
	}
	if f.broken {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(wsAuthNoSessReply))
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte(wsAuthReply))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(pongReply))
	}
}

func TestStatus_ReportsKeyState(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()

	cases := []struct {
		name       string
		transport  string
		key        string
		broken     bool
		apiDown    bool
		wantStatus credentialStatus
	}{
		{
			name:       "valid key over http",
			transport:  transport.SchemeHTTP,
			key:        validKey,
			wantStatus: statusValid,
		},
		{
			name:       "valid key over websocket",
			transport:  transport.SchemeWS,
			key:        validKey,
			wantStatus: statusValid,
		},
		{
			name:       "invalid key over http",
			transport:  transport.SchemeHTTP,
			key:        invalidKey,
			wantStatus: statusInvalid,
		},
		{
			name:       "invalid key over websocket",
			transport:  transport.SchemeWS,
			key:        invalidKey,
			wantStatus: statusInvalid,
		},
		{
			name:       "accepted key but server error over http",
			transport:  transport.SchemeHTTP,
			key:        validKey,
			broken:     true,
			wantStatus: statusError,
		},
		{
			name:       "accepted key but no session id over websocket",
			transport:  transport.SchemeWS,
			key:        validKey,
			broken:     true,
			wantStatus: statusError,
		},
		{
			name:       "api down over http",
			transport:  transport.SchemeHTTP,
			key:        validKey,
			apiDown:    true,
			wantStatus: statusUnreachable,
		},
		{
			name:       "api down over websocket",
			transport:  transport.SchemeWS,
			key:        validKey,
			apiDown:    true,
			wantStatus: statusUnreachable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apiURL := down.URL
			if !tc.apiDown {
				api := httptest.NewServer(&fakeAPI{scheme: tc.transport, broken: tc.broken})
				defer api.Close()
				apiURL = api.URL
			}
			t.Setenv("HOME", t.TempDir())
			t.Setenv("RUNWARE_API_KEY", tc.key)
			t.Setenv("RUNWARE_BASE_URL", apiURL)
			t.Setenv("RUNWARE_WS_BASE_URL", "ws"+strings.TrimPrefix(apiURL, "http"))

			got, err := runStatus(context.Background(), t, tc.transport)

			if err != nil {
				t.Fatalf("auth status: %v", err)
			}
			if got.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tc.wantStatus)
			}
		})
	}
}

func TestStatus_CancelledCheckFailsInsteadOfReportingAStatus(t *testing.T) {
	cases := []struct {
		name      string
		transport string
	}{
		{name: "over http", transport: transport.SchemeHTTP},
		{name: "over websocket", transport: transport.SchemeWS},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := httptest.NewServer(&fakeAPI{scheme: tc.transport})
			defer api.Close()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("RUNWARE_API_KEY", validKey)
			t.Setenv("RUNWARE_BASE_URL", api.URL)
			t.Setenv("RUNWARE_WS_BASE_URL", "ws"+strings.TrimPrefix(api.URL, "http"))
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			_, err := runStatus(ctx, t, tc.transport)

			if !errors.Is(err, context.Canceled) {
				t.Errorf("err = %v, want context.Canceled", err)
			}
		})
	}
}

func runStatus(ctx context.Context, t *testing.T, transportScheme string) (authStatusResult, error) {
	t.Helper()
	root := &cobra.Command{Use: "runware"}
	root.PersistentFlags().String("format", "json", "")
	root.PersistentFlags().String("transport", transportScheme, "")
	root.AddCommand(newStatusCmd(log.New(io.Discard)))
	root.SetArgs([]string{"status"})

	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close() //nolint:errcheck
	old := os.Stdout
	os.Stdout = stdout
	t.Cleanup(func() { os.Stdout = old })
	execErr := root.ExecuteContext(ctx)
	os.Stdout = old
	if execErr != nil {
		return authStatusResult{}, execErr
	}

	b, err := os.ReadFile(stdout.Name())
	if err != nil {
		t.Fatal(err)
	}
	var got authStatusResult
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("decode %q: %v", b, err)
	}
	return got, nil
}
