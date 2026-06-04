package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// minimalSchemaBody is a minimal valid schema envelope used across happy-path tests.
const minimalSchemaBody = `{
	"requestSchema":  {"type": "object", "properties": {"model": {"type": "string"}}, "required": ["model"]},
	"responseSchema": {"type": "object", "properties": {"taskType": {"type": "string"}}},
	"documentation":  "https://runware.ai/docs/models/test-model"
}`

// schemaServer is a test helper that starts an httptest server with the given
// handler and returns it together with its base URL (with trailing slash).
func schemaServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, srv.URL + "/"
}

// TestFetchModelSchema_200 verifies that a 200 response is parsed correctly.
func TestFetchModelSchema_200(t *testing.T) {
	srv, base := schemaServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Verify the AIR is appended to the path.
		if !strings.HasSuffix(r.URL.Path, "civitai:305149@392545") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(minimalSchemaBody))
	})

	schema, err := fetchModelSchema(context.Background(), "civitai:305149@392545", srv.Client(), base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if schema.Documentation != "https://runware.ai/docs/models/test-model" {
		t.Errorf("unexpected Documentation: %s", schema.Documentation)
	}
	if len(schema.RequestSchema) == 0 {
		t.Error("RequestSchema is empty")
	}
	if len(schema.ResponseSchema) == 0 {
		t.Error("ResponseSchema is empty")
	}
}

// TestFetchModelSchema_404 verifies that a 404 returns a "not found" error
// that names the AIR.
func TestFetchModelSchema_404(t *testing.T) {
	_, base := schemaServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := fetchModelSchema(context.Background(), "unknown:1@1", http.DefaultClient, base)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "unknown:1@1") {
		t.Errorf("expected AIR in error, got: %v", err)
	}
}

// TestFetchModelSchema_UnexpectedStatus verifies that non-200/404 status codes
// return an error that includes the status code.
func TestFetchModelSchema_UnexpectedStatus(t *testing.T) {
	for _, code := range []int{http.StatusInternalServerError, http.StatusServiceUnavailable, http.StatusBadRequest} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			_, base := schemaServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			})

			_, err := fetchModelSchema(context.Background(), "google:3@2", http.DefaultClient, base)
			if err == nil {
				t.Fatalf("expected error for HTTP %d, got nil", code)
			}
			codeStr := http.StatusText(code)
			if !strings.Contains(err.Error(), "status") {
				t.Errorf("expected 'status' in error for %s, got: %v", codeStr, err)
			}
		})
	}
}

// TestFetchModelSchema_InvalidJSON verifies that a 200 with a malformed body
// returns a parse error.
func TestFetchModelSchema_InvalidJSON(t *testing.T) {
	_, base := schemaServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json at all`))
	})

	_, err := fetchModelSchema(context.Background(), "google:3@2", http.DefaultClient, base)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected 'parse' in error, got: %v", err)
	}
}

// TestFetchModelSchema_ContextCancelled verifies that a cancelled context
// causes the request to fail promptly.
func TestFetchModelSchema_ContextCancelled(t *testing.T) {
	_, base := schemaServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Handler intentionally does nothing; context will be cancelled before response.
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := fetchModelSchema(ctx, "google:3@2", http.DefaultClient, base)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}
