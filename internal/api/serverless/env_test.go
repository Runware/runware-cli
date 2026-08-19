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

const (
	testEnvVarName  = "MY_KEY"
	testEnvVarValue = "hello"
)

func TestListDeploymentEnvironmentVariables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/deployments/" + testDeploymentID + "/environment-variables"
		if r.Method != http.MethodGet || r.URL.Path != want {
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
			"deploymentId":"my-app",
			"key":"` + testEnvVarName + `",
			"value":"` + testEnvVarValue + `",
			"createdAt":"2026-07-30T12:00:00Z",
			"updatedAt":"2026-07-30T12:00:00Z"
		}],"nextCursor":"` + testCursorPage3 + `"}`))
	}))
	defer srv.Close()

	limit := Limit(10)
	cursor := Cursor(testCursorPage2)
	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	page, err := c.ListDeploymentEnvironmentVariables(context.Background(), testDeploymentID, &ListDeploymentEnvironmentVariablesParams{
		Limit:  &limit,
		Cursor: &cursor,
	})
	if err != nil {
		t.Fatalf("ListDeploymentEnvironmentVariables: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].Key != testEnvVarName {
		t.Fatalf("unexpected env vars: %+v", page.Data)
	}
	if page.Data[0].Value != testEnvVarValue {
		t.Errorf("unexpected value: %q", page.Data[0].Value)
	}
	if page.NextCursor == nil || *page.NextCursor != testCursorPage3 {
		t.Fatalf("unexpected nextCursor: %+v", page.NextCursor)
	}
}

func TestListDeploymentEnvironmentVariables_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.ListDeploymentEnvironmentVariables(context.Background(), testDeploymentID, nil); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestUpdateDeploymentEnvironmentVariable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/deployments/" + testDeploymentID + "/environment-variables/" + testEnvVarName
		if r.Method != http.MethodPut || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var body EnvironmentVariableUpdate
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Value != testEnvVarValue {
			t.Errorf("unexpected body: %s", raw)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			"deploymentId":"my-app",
			"key":"` + testEnvVarName + `",
			"value":"` + testEnvVarValue + `",
			"createdAt":"2026-07-30T12:00:00Z",
			"updatedAt":"2026-07-30T12:00:00Z"
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	ev, err := c.UpdateDeploymentEnvironmentVariable(context.Background(), testDeploymentID, testEnvVarName, EnvironmentVariableUpdate{
		Value: testEnvVarValue,
	})
	if err != nil {
		t.Fatalf("UpdateDeploymentEnvironmentVariable: %v", err)
	}
	if ev.Key != testEnvVarName || ev.Value != testEnvVarValue {
		t.Errorf("unexpected env var: %+v", ev)
	}
}

func TestUpdateDeploymentEnvironmentVariable_Unprocessable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{
			"type":"about:blank",
			"title":"Unprocessable Entity",
			"status":422,
			"detail":"RUNTIME is a reserved platform name",
			"errors":[{"detail":"reserved platform name","pointer":"/variableName"}]
		}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	_, err := c.UpdateDeploymentEnvironmentVariable(context.Background(), testDeploymentID, "RUNTIME", EnvironmentVariableUpdate{
		Value: "x",
	})
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeValidation {
		t.Errorf("expected CodeValidation, got %v", re.Code)
	}
	if re.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected status 422, got %d", re.StatusCode)
	}
	if !strings.Contains(re.Message, "RUNTIME is a reserved platform name") {
		t.Errorf("missing detail: %q", re.Message)
	}
	if !strings.Contains(re.Message, "/variableName: reserved platform name") {
		t.Errorf("missing field error: %q", re.Message)
	}
}

func TestUpdateDeploymentEnvironmentVariable_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if _, err := c.UpdateDeploymentEnvironmentVariable(context.Background(), testDeploymentID, testEnvVarName, EnvironmentVariableUpdate{}); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestDeleteDeploymentEnvironmentVariable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v1/deployments/" + testDeploymentID + "/environment-variables/" + testEnvVarName
		if r.Method != http.MethodDelete || r.URL.Path != want {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	if err := c.DeleteDeploymentEnvironmentVariable(context.Background(), testDeploymentID, testEnvVarName); err != nil {
		t.Fatalf("DeleteDeploymentEnvironmentVariable: %v", err)
	}
}

func TestDeleteDeploymentEnvironmentVariable_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"detail":"No environment variable '` + testEnvVarName + `' exists"}`))
	}))
	defer srv.Close()

	c := newClient("test-key", srv.URL, slog.Default(), srv.Client())
	err := c.DeleteDeploymentEnvironmentVariable(context.Background(), testDeploymentID, testEnvVarName)
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", re.Code)
	}
	if re.Message != "No environment variable '"+testEnvVarName+"' exists" {
		t.Errorf("unexpected message: %q", re.Message)
	}
}

func TestDeleteDeploymentEnvironmentVariable_NoAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	if err := c.DeleteDeploymentEnvironmentVariable(context.Background(), testDeploymentID, testEnvVarName); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}
