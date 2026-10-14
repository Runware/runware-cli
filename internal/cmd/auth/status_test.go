package auth

import (
	"encoding/json"
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

	invalidKeyReply = `{"errors":[{"code":"invalidApiKey","message":"invalid key"}]}`
	pongReply       = `{"data":[{"taskType":"ping","pong":true}]}`
	wsAuthReply     = `{"data":[{"taskType":"authentication","connectionSessionUUID":"session"}]}`
)

// fakeAPI answers ping over HTTP and WebSocket, accepting only validKey.
type fakeAPI struct {
	upgrader websocket.Upgrader
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if websocket.IsWebSocketUpgrade(r) {
		f.serveWS(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+validKey {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(invalidKeyReply))
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
	_ = conn.WriteMessage(websocket.TextMessage, []byte(wsAuthReply))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(pongReply))
	}
}

func TestStatus_ReportsKeyState(t *testing.T) {
	api := httptest.NewServer(&fakeAPI{})
	defer api.Close()
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()

	cases := []struct {
		name       string
		transport  string
		key        string
		apiURL     string
		wantStatus string
	}{
		{
			name:       "valid key over http",
			transport:  transport.SchemeHTTP,
			key:        validKey,
			apiURL:     api.URL,
			wantStatus: "valid",
		},
		{
			name:       "valid key over websocket",
			transport:  transport.SchemeWS,
			key:        validKey,
			apiURL:     api.URL,
			wantStatus: "valid",
		},
		{
			name:       "invalid key over http",
			transport:  transport.SchemeHTTP,
			key:        invalidKey,
			apiURL:     api.URL,
			wantStatus: "invalid",
		},
		{
			name:       "invalid key over websocket",
			transport:  transport.SchemeWS,
			key:        invalidKey,
			apiURL:     api.URL,
			wantStatus: "invalid",
		},
		{
			name:       "api down over http",
			transport:  transport.SchemeHTTP,
			key:        validKey,
			apiURL:     down.URL,
			wantStatus: "unreachable",
		},
		{
			name:       "api down over websocket",
			transport:  transport.SchemeWS,
			key:        validKey,
			apiURL:     down.URL,
			wantStatus: "unreachable",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("RUNWARE_API_KEY", tc.key)
			t.Setenv("RUNWARE_BASE_URL", tc.apiURL)
			t.Setenv("RUNWARE_WS_BASE_URL", "ws"+strings.TrimPrefix(tc.apiURL, "http"))

			got := runStatus(t, tc.transport)

			if got.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tc.wantStatus)
			}
		})
	}
}

func runStatus(t *testing.T, transportScheme string) authStatusResult {
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
	execErr := root.Execute()
	os.Stdout = old
	if execErr != nil {
		t.Fatalf("auth status: %v", execErr)
	}

	b, err := os.ReadFile(stdout.Name())
	if err != nil {
		t.Fatal(err)
	}
	var got authStatusResult
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("decode %q: %v", b, err)
	}
	return got
}
