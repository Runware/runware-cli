package serverless

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/api/transport"
)

const testSecretName = "FOO"

func TestListSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/secrets" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Errorf("limit query = %q, want 10", got)
		}
		if got := r.URL.Query().Get("cursor"); got != testCursorPage2 {
			t.Errorf("cursor query = %q, want %s", got, testCursorPage2)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{
			"id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			"name":"FOO",
			"type":"generic",
			"createdAt":"2026-07-30T12:00:00Z"
		}],"nextCursor":"` + testCursorPage3 + `"}`))
	}))
	defer srv.Close()

	limit := Limit(10)
	cursor := Cursor(testCursorPage2)
	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListSecrets(context.Background(), &ListSecretsParams{
		Limit:  &limit,
		Cursor: &cursor,
	})
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].Name != testSecretName {
		t.Fatalf("unexpected secrets: %+v", page.Data)
	}
	if string(page.Data[0].Type) != "generic" {
		t.Errorf("unexpected type: %s", page.Data[0].Type)
	}
	if page.NextCursor == nil || *page.NextCursor != testCursorPage3 {
		t.Fatalf("unexpected nextCursor: %+v", page.NextCursor)
	}
}

func TestListSecrets_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.ListSecrets(context.Background(), nil); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestCreateSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/secrets" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["name"] != testSecretName || body["type"] != "generic" || body["value"] != "s3cret" {
			t.Errorf("unexpected body: %s", raw)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			"name":"FOO",
			"type":"generic",
			"createdAt":"2026-07-30T12:00:00Z"
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	sec, err := c.CreateSecret(context.Background(), SecretCreate{
		Name:  testSecretName,
		Type:  SecretTypeGeneric,
		Value: "s3cret",
	})
	if err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}
	if sec.Name != testSecretName || string(sec.Type) != "generic" {
		t.Errorf("unexpected secret: %+v", sec)
	}
}

func TestCreateSecret_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Conflict","status":409,"detail":"Secret name is already in use"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.CreateSecret(context.Background(), SecretCreate{
		Name:  testSecretName,
		Type:  SecretTypeGeneric,
		Value: "s3cret",
	})
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeValidation {
		t.Errorf("expected CodeValidation, got %v", re.Code)
	}
	if re.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409, got %d", re.StatusCode)
	}
	if re.Message != "Secret name is already in use" {
		t.Errorf("unexpected message: %q", re.Message)
	}
}

func TestCreateSecret_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.CreateSecret(context.Background(), SecretCreate{}); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestUpdateSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/secrets/"+testSecretName {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			"name":"FOO",
			"type":"generic"
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	sec, err := c.UpdateSecret(context.Background(), testSecretName, SecretUpdate{Value: "new-value"})
	if err != nil {
		t.Fatalf("UpdateSecret: %v", err)
	}
	if sec.Name != testSecretName {
		t.Errorf("unexpected secret: %+v", sec)
	}
}

func TestUpdateSecret_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.UpdateSecret(context.Background(), testSecretName, SecretUpdate{}); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestDeleteSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/secrets/"+testSecretName {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	if err := c.DeleteSecret(context.Background(), testSecretName); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
}

func TestDeleteSecret_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Conflict","status":409,"detail":"Secret is still attached to an app"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	err := c.DeleteSecret(context.Background(), testSecretName)
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409, got %d", re.StatusCode)
	}
	if re.Message != "Secret is still attached to an app" {
		t.Errorf("unexpected message: %q", re.Message)
	}
}

func TestDeleteSecret_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if err := c.DeleteSecret(context.Background(), testSecretName); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestListAppSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/secrets"
		if r.Method != http.MethodGet || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "5" {
			t.Errorf("limit query = %q, want 5", got)
		}
		if got := r.URL.Query().Get("cursor"); got != testCursorPage2 {
			t.Errorf("cursor query = %q, want %s", got, testCursorPage2)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{
			"id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			"name":"FOO",
			"type":"generic",
			"envVarName":"FOO_KEY",
			"createdAt":"2026-07-30T12:00:00Z"
		}],"nextCursor":"` + testCursorPage3 + `"}`))
	}))
	defer srv.Close()

	limit := Limit(5)
	cursor := Cursor(testCursorPage2)
	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListAppSecrets(context.Background(), testAppID, &ListAppSecretsParams{
		Limit:  &limit,
		Cursor: &cursor,
	})
	if err != nil {
		t.Fatalf("ListAppSecrets: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].Name != testSecretName {
		t.Fatalf("unexpected attachments: %+v", page.Data)
	}
	if page.Data[0].EnvVarName == nil || *page.Data[0].EnvVarName != "FOO_KEY" {
		t.Errorf("unexpected envVarName: %+v", page.Data[0].EnvVarName)
	}
	if page.NextCursor == nil || *page.NextCursor != testCursorPage3 {
		t.Fatalf("unexpected nextCursor: %+v", page.NextCursor)
	}
}

func TestListAppSecrets_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.ListAppSecrets(context.Background(), testAppID, nil); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestAttachAppSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/secrets"
		if r.Method != http.MethodPost || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var body SecretAttach
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.SecretName != testSecretName {
			t.Errorf("secretName = %q", body.SecretName)
		}
		if body.EnvVarName == nil || *body.EnvVarName != "FOO_KEY" {
			t.Errorf("envVarName = %+v", body.EnvVarName)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	env := "FOO_KEY"
	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	if err := c.AttachAppSecret(context.Background(), testAppID, SecretAttach{
		SecretName: testSecretName,
		EnvVarName: &env,
	}); err != nil {
		t.Fatalf("AttachAppSecret: %v", err)
	}
}

func TestAttachAppSecret_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Conflict","status":409,"detail":"Secret is already attached"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	err := c.AttachAppSecret(context.Background(), testAppID, SecretAttach{SecretName: testSecretName})
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409, got %d", re.StatusCode)
	}
	if re.Message != "Secret is already attached" {
		t.Errorf("unexpected message: %q", re.Message)
	}
}

func TestAttachAppSecret_Validation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{
			"type":"about:blank",
			"title":"Unprocessable Entity",
			"status":422,
			"detail":"env var name collides with a plain environment variable",
			"errors":[{"detail":"already exists as an environment variable","pointer":"/envVarName"}]
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	err := c.AttachAppSecret(context.Background(), testAppID, SecretAttach{SecretName: testSecretName})
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected status 422, got %d", re.StatusCode)
	}
	if !strings.Contains(re.Message, "env var name collides with a plain environment variable") {
		t.Errorf("missing detail: %q", re.Message)
	}
	if !strings.Contains(re.Message, "/envVarName: already exists as an environment variable") {
		t.Errorf("missing field error: %q", re.Message)
	}
}

func TestAttachAppSecret_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if err := c.AttachAppSecret(context.Background(), testAppID, SecretAttach{SecretName: testSecretName}); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestDetachAppSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/apps/" + testAppID + "/secrets/" + testSecretName
		if r.Method != http.MethodDelete || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	if err := c.DetachAppSecret(context.Background(), testAppID, testSecretName); err != nil {
		t.Fatalf("DetachAppSecret: %v", err)
	}
}

func TestDetachAppSecret_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if err := c.DetachAppSecret(context.Background(), testAppID, testSecretName); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}
