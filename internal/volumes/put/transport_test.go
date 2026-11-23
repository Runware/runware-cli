package put

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runware/runware-cli/internal/buildinfo"
)

const workerToken = "tok.example.1.45.0"

func newWorker(t *testing.T, h http.HandlerFunc) (*HTTPTransport, Target) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return NewHTTPTransport(srv.Client()), Target{
		PartNumber: 3,
		URL:        srv.URL + "/v1/parts",
		Token:      Secret(workerToken),
		ExpiresAt:  time.Now().Add(time.Hour),
	}
}

func problemHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func TestHTTPTransportPutsPart(t *testing.T) {
	var gotMethod, gotAuth, gotType, gotAgent string
	var gotLength int64
	var gotBody []byte
	tr, target := newWorker(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotAuth, gotType, gotAgent = r.Method, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.Header.Get("User-Agent")
		gotLength = r.ContentLength
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"partNumber":3,"ack":"kid1.sealed-tag"}`)
	})
	body := patterned(45, 1)

	ack, err := tr.PutPart(t.Context(), target, bytes.NewReader(body), int64(len(body)))

	if err != nil {
		t.Fatal(err)
	}
	if string(ack) != "kid1.sealed-tag" {
		t.Errorf("ack = %q", string(ack))
	}
	if gotMethod != http.MethodPut || gotAuth != "Bearer "+workerToken || gotType != "application/octet-stream" || gotLength != 45 || !bytes.Equal(gotBody, body) {
		t.Errorf("request = %s, auth %q, type %q, length %d, body of %d bytes", gotMethod, gotAuth, gotType, gotLength, len(gotBody))
	}
	if gotAgent != buildinfo.UserAgent() {
		t.Errorf("User-Agent = %q, want the CLI's %q", gotAgent, buildinfo.UserAgent())
	}
}

func TestHTTPTransportDoesNotInheritClientTimeout(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			return
		}
		_, _ = io.WriteString(w, `{"partNumber":1,"ack":"example-ack"}`)
	}))
	t.Cleanup(srv.Close)
	client := *srv.Client()
	client.Timeout = time.Nanosecond
	tr := NewHTTPTransport(&client)

	target := Target{PartNumber: 1, URL: srv.URL, Token: Secret(workerToken)}
	if _, err := tr.PutPart(t.Context(), target, strings.NewReader("x"), 1); err != nil {
		t.Fatal(err)
	}
	if client.Timeout != time.Nanosecond {
		t.Error("the part transport changed the caller's client")
	}
}

type problemCase struct {
	name      string
	status    int
	body      string
	want      error
	retryable bool
}

func TestHTTPTransportMapsProblems(t *testing.T) {
	const base = `{"type":"https://docs.runware.ai/serverless/errors#`
	cases := []problemCase{
		{
			name:   "token invalid",
			status: http.StatusUnauthorized,
			body:   base + `volume-part-token-invalid","title":"Unauthorized","status":401}`,
			want:   ErrPartTokenInvalid,
		},
		{
			name:   "token expired",
			status: http.StatusUnauthorized,
			body:   base + `volume-part-token-expired","title":"Unauthorized","status":401}`,
			want:   ErrPartTokenExpired,
		},
		{
			name:   "length mismatch",
			status: http.StatusBadRequest,
			body:   base + `volume-part-length-mismatch","title":"Bad Request","status":400}`,
			want:   ErrPartLengthMismatch,
		},
		{
			name:      "storage unavailable",
			status:    http.StatusServiceUnavailable,
			body:      base + `volume-part-storage-unavailable","title":"Service Unavailable","status":503}`,
			want:      ErrPartStorageUnavailable,
			retryable: true,
		},
		{
			name:      "an edge error that is not a problem",
			status:    http.StatusBadGateway,
			body:      "<html>bad gateway</html>",
			retryable: true,
		},
		{
			name:   "a body over the zone's limit",
			status: http.StatusRequestEntityTooLarge,
			body:   "<html>too large</html>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr, target := newWorker(t, problemHandler(tc.status, tc.body))

			_, err := tr.PutPart(t.Context(), target, strings.NewReader("x"), 1)

			p, ok := errors.AsType[*ProblemError](err)
			if !ok || p.Status != tc.status {
				t.Fatalf("error = %v, want a problem with status %d", err, tc.status)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("error %v is not %v", err, tc.want)
			}
			if retryable(err) != tc.retryable {
				t.Errorf("retryable = %v, want %v", retryable(err), tc.retryable)
			}
			if strings.Contains(err.Error(), workerToken) {
				t.Error("the error holds the token")
			}
		})
	}
}

type badAckCase struct {
	name string
	body string
}

func TestHTTPTransportRefusesBadAcks(t *testing.T) {
	cases := []badAckCase{
		{
			name: "another part",
			body: `{"partNumber":4,"ack":"a"}`,
		},
		{
			name: "no ack",
			body: `{"partNumber":3}`,
		},
		{
			name: "an ack over the bound",
			body: `{"partNumber":3,"ack":"` + strings.Repeat("a", 1025) + `"}`,
		},
		{
			name: "not JSON",
			body: "ok",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr, target := newWorker(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, tc.body) })

			_, err := tr.PutPart(t.Context(), target, strings.NewReader("x"), 1)

			if !errors.Is(err, ErrProtocol) || retryable(err) {
				t.Errorf("error = %v, want a contract violation that is not retried", err)
			}
		})
	}
}

func TestHTTPTransportAcceptsAnAckAtTheBound(t *testing.T) {
	ack := strings.Repeat("a", 1024)
	tr, target := newWorker(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"partNumber":3,"ack":"`+ack+`"}`)
	})

	got, err := tr.PutPart(t.Context(), target, strings.NewReader("x"), 1)

	if err != nil || string(got) != ack {
		t.Errorf("an ack of 1024 characters: error = %v, %d characters back, want it accepted whole", err, len(got))
	}
}

// connections records the protocol and the connection of every request that reaches the server.
type connections struct {
	mu     sync.Mutex
	protos map[string]int
	conns  map[string]struct{}
}

func (c *connections) add(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.protos[r.Proto]++
	c.conns[r.RemoteAddr] = struct{}{}
}

// roundGate holds each request of a round until the whole round has arrived.
type roundGate struct {
	arrived atomic.Int32
	open    chan struct{}
}

// TestDefaultTransportKeepsAnHTTP1ConnectionPerPart sends the most parts the engine runs at once, twice, to a server that offers HTTP/2.
func TestDefaultTransportKeepsAnHTTP1ConnectionPerPart(t *testing.T) {
	const rounds = 2
	parts := maxParallelism
	seen := &connections{
		protos: map[string]int{},
		conns:  map[string]struct{}{},
	}
	var gate atomic.Pointer[roundGate]
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		_, _ = io.Copy(io.Discard, r.Body)
		g := gate.Load()
		if int(g.arrived.Add(1)) == parts {
			close(g.open)
		}
		select {
		case <-g.open:
		case <-time.After(10 * time.Second):
		}
		_, _ = io.WriteString(w, `{"partNumber":1,"ack":"a"}`)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	tr := NewHTTPTransport(nil)
	inner, ok := tr.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("the default client's transport is a %T", tr.client.Transport)
	}
	inner.TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()

	for round := range rounds {
		gate.Store(&roundGate{open: make(chan struct{})})
		var wg sync.WaitGroup
		for part := range parts {
			wg.Go(func() {
				target := Target{
					PartNumber: 1,
					URL:        srv.URL,
					Token:      Secret("t"),
				}
				if _, err := tr.PutPart(t.Context(), target, strings.NewReader("x"), 1); err != nil {
					t.Errorf("round %d, part %d: %v", round, part, err)
				}
			})
		}
		wg.Wait()
	}

	seen.mu.Lock()
	defer seen.mu.Unlock()
	if want := map[string]int{"HTTP/1.1": rounds * parts}; !maps.Equal(seen.protos, want) {
		t.Errorf("protocols = %v, want every part over HTTP/1.1", seen.protos)
	}
	if len(seen.conns) != parts {
		t.Errorf("%d connections for %d concurrent parts sent twice, want one connection per part, reused", len(seen.conns), parts)
	}
}

func TestHTTPTransportRefusesCleartext(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(srv.Close)
	tr := NewHTTPTransport(srv.Client())

	_, err := tr.PutPart(t.Context(), Target{
		PartNumber: 1,
		URL:        srv.URL,
		Token:      Secret(workerToken),
	}, strings.NewReader("x"), 1)

	if !errors.Is(err, ErrProtocol) || requests.Load() != 0 {
		t.Errorf("error = %v after %d requests, want the token never sent over http", err, requests.Load())
	}
	for _, bad := range []string{"", "not a url", "https://", "ftp://example.com/x"} {
		if _, err := tr.PutPart(t.Context(), Target{
			PartNumber: 1,
			URL:        bad,
		}, strings.NewReader("x"), 1); !errors.Is(err, ErrProtocol) {
			t.Errorf("address %q: error = %v, want a contract violation", bad, err)
		}
	}
}

func TestHTTPTransportDoesNotFollowRedirects(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere.Add(1) }))
	t.Cleanup(other.Close)
	tr, target := newWorker(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	})

	_, err := tr.PutPart(t.Context(), target, strings.NewReader("x"), 1)

	p, ok := errors.AsType[*ProblemError](err)
	if !ok || p.Status != http.StatusTemporaryRedirect || elsewhere.Load() != 0 {
		t.Errorf("error = %v, redirected requests = %d, want the redirect refused and the token kept", err, elsewhere.Load())
	}
}

func TestHTTPTransportErrorsHoldNoAddress(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	tr := NewHTTPTransport(srv.Client())
	target := Target{
		PartNumber: 1,
		URL:        srv.URL + "/v1/parts",
		Token:      Secret(workerToken),
	}
	srv.Close()

	_, err := tr.PutPart(t.Context(), target, strings.NewReader("x"), 1)

	if err == nil || !retryable(err) {
		t.Fatalf("error = %v, want a connection failure that is retried", err)
	}
	if strings.Contains(err.Error(), srv.URL) || strings.Contains(err.Error(), "/v1/parts") || strings.Contains(err.Error(), workerToken) {
		t.Error("the error holds the upload address or the token")
	}
}

type retryableCase struct {
	name string
	err  error
	want bool
}

func TestRetryable(t *testing.T) {
	cases := []retryableCase{
		{
			name: "a broken connection",
			err:  io.ErrUnexpectedEOF,
			want: true,
		},
		{
			name: "storage unavailable",
			err:  ErrPartStorageUnavailable,
			want: true,
		},
		{
			name: "503",
			err:  &ProblemError{Status: http.StatusServiceUnavailable},
			want: true,
		},
		{
			name: "429",
			err:  &ProblemError{Status: http.StatusTooManyRequests},
			want: true,
		},
		{
			name: "a stalled part",
			err:  errPartStalled,
			want: true,
		},
		{
			name: "token invalid",
			err:  ErrPartTokenInvalid,
		},
		{
			name: "length mismatch",
			err:  ErrPartLengthMismatch,
		},
		{
			name: "a contract violation",
			err:  ErrProtocol,
		},
		{
			name: "403",
			err:  &ProblemError{Status: http.StatusForbidden},
		},
		{
			name: "404",
			err:  &ProblemError{Status: http.StatusNotFound},
		},
	}
	for _, tc := range cases {
		if got := retryable(tc.err); got != tc.want {
			t.Errorf("%s: retryable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDefaultTransportBoundsAStalledAnswer(t *testing.T) {
	tr, ok := NewHTTPTransport(nil).client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("the default client has no *http.Transport")
	}
	if tr.ResponseHeaderTimeout != 2*time.Minute {
		t.Fatalf("ResponseHeaderTimeout = %v, want 2m0s", tr.ResponseHeaderTimeout)
	}
}

// TestHTTPTransportStopsReadingTheBodyOnReturn: a refusal that comes before the body is sent leaves no read of it running.
func TestHTTPTransportStopsReadingTheBodyOnReturn(t *testing.T) {
	tr, target := newWorker(t, problemHandler(http.StatusRequestEntityTooLarge, "<html>too large</html>"))
	body := &inFlightReader{r: bytes.NewReader(patterned(1<<20, 1))}

	_, err := tr.PutPart(t.Context(), target, body, 1<<20)

	if n := body.running.Load(); n != 0 {
		t.Errorf("%d reads of the body still running when PutPart returned", n)
	}
	if p, ok := errors.AsType[*ProblemError](err); !ok || p.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("error = %v, want the refusal", err)
	}
}

type stallCase struct {
	name    string
	size    int
	handler http.HandlerFunc
}

func TestHTTPTransportEndsAStalledPart(t *testing.T) {
	cases := []stallCase{
		{
			name: "an answer that stops after its headers",
			size: 1,
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = io.WriteString(w, `{"partNumber":3,`)
				_ = http.NewResponseController(w).Flush()
			},
		},
		{
			name:    "a worker that stops reading the part",
			size:    64 << 20,
			handler: func(http.ResponseWriter, *http.Request) {},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			tr, target := newWorker(t, func(w http.ResponseWriter, r *http.Request) {
				tc.handler(w, r)
				select {
				case <-release:
				case <-time.After(10 * time.Second):
				}
			})
			t.Cleanup(func() { close(release) })
			tr.stall = time.Second

			_, err := tr.PutPart(t.Context(), target, bytes.NewReader(make([]byte, tc.size)), int64(tc.size))

			if !errors.Is(err, errPartStalled) || !retryable(err) {
				t.Errorf("error = %v, want a stall that is retried", err)
			}
		})
	}
}

func TestPartBodyEndsReadsOnStop(t *testing.T) {
	b := &partBody{
		r:     unreadReader{t: t},
		timer: time.AfterFunc(time.Hour, func() {}),
		stall: time.Hour,
	}
	b.stop()

	if n, err := b.Read(make([]byte, 8)); n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("a read after stop = %d, %v, want 0, EOF", n, err)
	}
}

// unreadReader fails the test when it is read.
type unreadReader struct {
	t *testing.T
}

func (r unreadReader) Read([]byte) (int, error) {
	r.t.Error("the body was read after stop")
	return 0, io.EOF
}

// inFlightReader counts its reads still running; each pauses so one is in flight when a refusal arrives.
type inFlightReader struct {
	r       io.Reader
	running atomic.Int32
}

func (r *inFlightReader) Read(p []byte) (int, error) {
	r.running.Add(1)
	defer r.running.Add(-1)
	time.Sleep(200 * time.Millisecond)
	return r.r.Read(p)
}

// slowReader hands out a few bytes per read after a pause.
type slowReader struct {
	r     io.Reader
	pause time.Duration
}

func (s *slowReader) Read(p []byte) (int, error) {
	time.Sleep(s.pause)
	return s.r.Read(p[:min(len(p), 4)])
}

func TestHTTPTransportKeepsASlowPartThatMoves(t *testing.T) {
	var got []byte
	tr, target := newWorker(t, func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"partNumber":3,"ack":"a"}`)
	})
	tr.stall = time.Second
	body := patterned(16, 1)

	_, err := tr.PutPart(t.Context(), target, &slowReader{
		r:     bytes.NewReader(body),
		pause: 300 * time.Millisecond,
	}, int64(len(body)))

	if err != nil || !bytes.Equal(got, body) {
		t.Errorf("error = %v, %d of %d bytes arrived, want a part that keeps moving sent whole", err, len(got), len(body))
	}
}

// TestHTTPTransportDebugLogShowsNoSecret: under debug the transport logs each
// part's number, status and byte count, and never the upload address, the part
// token or the acknowledgement.
func TestHTTPTransportDebugLogShowsNoSecret(t *testing.T) {
	const secretAck = "SECRET-ACK"
	for _, status := range []int{http.StatusOK, http.StatusForbidden} {
		var logged bytes.Buffer
		tr, target := newWorker(t, func(w http.ResponseWriter, _ *http.Request) {
			if status != http.StatusOK {
				problemHandler(status, `{"title":"Forbidden","status":403}`)(w, nil)
				return
			}
			_, _ = io.WriteString(w, `{"partNumber":3,"ack":"`+secretAck+`"}`)
		})
		tr.WithLogger(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
		target.URL += "/SECRET-PATH?sig=SECRET-QUERY"
		body := patterned(45, 1)
		_, _ = tr.PutPart(t.Context(), target, bytes.NewReader(body), int64(len(body)))
		out := logged.String()
		for _, want := range []string{"partNumber=3", "status=" + strconv.Itoa(status), "bytes=45"} {
			if !strings.Contains(out, want) {
				t.Errorf("status %d: log lacks %s: %s", status, want, out)
			}
		}
		for _, secret := range []string{"SECRET-", workerToken, target.URL} {
			if strings.Contains(out, secret) {
				t.Errorf("status %d: log shows %q: %s", status, secret, out)
			}
		}
	}
}
