package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDoReturnsErrorOnNon2xxWithoutErrorsBody(t *testing.T) {
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

func TestDoSurfacesAPIErrorEvenOnNon2xx(t *testing.T) {
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
	if !strings.Contains(err.Error(), "bad input") {
		t.Errorf("expected error to surface API error message, got: %v", err)
	}
}

func TestDoUnauthorizedOn401(t *testing.T) {
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
		t.Errorf("expected IsAuthError to be true for invalidApiKey, got: %v", err)
	}
}
