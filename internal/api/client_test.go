package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDo_Non2xx_InvalidJSON: non-200 with a non-JSON body returns a parse error
// that includes the HTTP status code.
func TestDo_Non2xx_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<html>internal error</html>`))
	}))
	defer srv.Close()

	c := NewClient("test-key", srv.URL, false)
	_, err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error from 5xx response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention status 500, got: %v", err)
	}
}

// TestDo_Non2xx_ValidJSON_NoErrors: non-200 with valid JSON that has no errors
// field — the previous blind spot. Must return an HTTP status error.
func TestDo_Non2xx_ValidJSON_NoErrors(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"data": []any{}})
	for _, code := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				_, _ = w.Write(body)
			}))
			defer srv.Close()

			c := NewClient("test-key", srv.URL, false)
			_, err := c.Ping(context.Background())
			if err == nil {
				t.Fatalf("expected error for HTTP %d, got nil", code)
			}
			codeStr := http.StatusText(code)
			if !strings.Contains(err.Error(), "HTTP") {
				t.Errorf("expected HTTP status error for %s, got: %v", codeStr, err)
			}
		})
	}
}

// TestDo_Non2xx_ValidJSON_WithErrors: non-200 with a structured errors field —
// errors field checked first, so structured API error surfaces over HTTP status.
func TestDo_Non2xx_ValidJSON_WithErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"code":"badInput","message":"bad input"}]}`))
	}))
	defer srv.Close()

	c := NewClient("test-key", srv.URL, false)
	_, err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Errors-first: structured message surfaces, not the HTTP status.
	if !strings.Contains(err.Error(), "bad input") {
		t.Errorf("expected structured API error message (errors first), got: %v", err)
	}
}

// TestDo_200_ValidJSON: happy path returns no error.
func TestDo_200_ValidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"taskType":"ping","taskUUID":"abc"}]}`))
	}))
	defer srv.Close()

	c := NewClient("test-key", srv.URL, false)
	_, err := c.Ping(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on 200 response: %v", err)
	}
}

// TestDo_200_WithErrors: 200 response that carries an errors field should still
// surface the API error.
func TestDo_200_WithErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errors":[{"code":"someError","message":"something went wrong"}]}`))
	}))
	defer srv.Close()

	c := NewClient("test-key", srv.URL, false)
	_, err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error from errors field, got nil")
	}
	if !strings.Contains(err.Error(), "something went wrong") {
		t.Errorf("expected structured API error message, got: %v", err)
	}
}

// TestDo_UnauthorizedOn401: 401 with invalidApiKey in errors field returns ErrUnauthorized.
func TestDo_UnauthorizedOn401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"code":"invalidApiKey","message":"invalid key"}]}`))
	}))
	defer srv.Close()

	c := NewClient("test-key", srv.URL, false)
	_, err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsAuthError(err) {
		t.Errorf("expected IsAuthError true for invalidApiKey, got: %v", err)
	}
}
