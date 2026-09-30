package serverless

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/api/transport"
)

const testSecretName = "FOO"

func TestCreateOrUpdateSecret_Creates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/secrets" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","name":"FOO","type":"generic"}`))
	}))
	defer srv.Close()

	client := serverlessapi.NewClient("test-key", srv.URL, slog.Default())
	sec, err := createOrUpdateSecret(context.Background(), client, testSecretName, "bar")
	if err != nil {
		t.Fatalf("createOrUpdateSecret: %v", err)
	}
	if sec.Name != testSecretName {
		t.Errorf("unexpected secret: %+v", sec)
	}
}

func TestCreateOrUpdateSecret_UpdatesOnConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/secrets":
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Conflict","status":409,"detail":"Secret name is already in use"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/secrets/FOO":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","name":"FOO","type":"generic"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := serverlessapi.NewClient("test-key", srv.URL, slog.Default())
	sec, err := createOrUpdateSecret(context.Background(), client, testSecretName, "bar")
	if err != nil {
		t.Fatalf("createOrUpdateSecret: %v", err)
	}
	if sec.Name != testSecretName {
		t.Errorf("unexpected secret: %+v", sec)
	}
}

func TestCreateOrUpdateSecret_ConflictThenNotFoundKeepsCreateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/secrets":
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Conflict","status":409,"detail":"Secret name is already in use"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/secrets/FOO":
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"No secret FOO"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	client := serverlessapi.NewClient("test-key", srv.URL, slog.Default())
	_, err := createOrUpdateSecret(context.Background(), client, testSecretName, "bar")
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.StatusCode != http.StatusConflict {
		t.Errorf("expected original 409, got %d (%q)", re.StatusCode, re.Message)
	}
	if re.Message != "Secret name is already in use" {
		t.Errorf("unexpected message: %q", re.Message)
	}
}
