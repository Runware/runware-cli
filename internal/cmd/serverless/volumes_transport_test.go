package serverless

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/volumes/put"
)

// releasedPartTransport is the part transport the command makes, saved before any test
// replaces the seam.
var releasedPartTransport = newPartTransport

// sendPart sends one part with the transport newTransport makes for a local upload
// endpoint, and returns how many requests reached the endpoint and the error.
func sendPart(t *testing.T, newTransport func(*http.Client) put.Transport) (int32, error) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, `{"partNumber":1,"ack":"kid1.sealed-tag"}`)
	}))
	t.Cleanup(srv.Close)
	_, err := newTransport(srv.Client()).PutPart(t.Context(), put.Target{
		PartNumber: 1,
		URL:        srv.URL + "/v1/parts",
		Token:      put.Secret("tok.example.1.4.0"),
		ExpiresAt:  time.Now().Add(time.Hour),
	}, strings.NewReader("part"), 4)
	return requests.Load(), err
}

// TestPartTransportHoldsProductionToThePlatformDomain: the transport the command makes for
// the production API refuses an upload address off the platform's domain and sends nothing.
func TestPartTransportHoldsProductionToThePlatformDomain(t *testing.T) {
	// The released transport dials with its own client, which would not trust the local
	// endpoint, so a request that got through would fail differently from ErrProtocol.
	requests, err := sendPart(t, func(*http.Client) put.Transport {
		return releasedPartTransport(config.DefaultServerlessBaseURL, nil)
	})
	if !errors.Is(err, put.ErrProtocol) {
		t.Fatalf("error = %v, want ErrProtocol", err)
	}
	if requests != 0 {
		t.Fatalf("%d requests reached a refused upload address", requests)
	}
}

// TestPartTransportSendsAnywhereOffProduction: the transport the command makes for any
// other API host sends to the upload address it is given.
func TestPartTransportSendsAnywhereOffProduction(t *testing.T) {
	requests, err := sendPart(t, func(client *http.Client) put.Transport {
		return newHTTPPartTransport(client, "https://api.example.test", nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("%d requests reached the upload address, want 1", requests)
	}
}
