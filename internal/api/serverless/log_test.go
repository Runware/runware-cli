package serverless

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestRedactJSONValues_EnvVarList(t *testing.T) {
	in := []byte(`{"data":[{"key":"MY_KEY","value":"super-secret"}],"nextCursor":"page-2"}`)
	out := redactJSONValues(in)
	if strings.Contains(string(out), "super-secret") {
		t.Fatalf("value leaked: %s", out)
	}
	if !strings.Contains(string(out), `"key":"MY_KEY"`) {
		t.Fatalf("key should remain: %s", out)
	}
	if !strings.Contains(string(out), `"value":"[redacted]"`) {
		t.Fatalf("value should be redacted: %s", out)
	}
}

func TestRedactJSONValues_NestedDeploymentEnvVars(t *testing.T) {
	in := []byte(`{"deploymentId":"my-app","environmentVariables":[{"key":"K","value":"v"}]}`)
	out := redactJSONValues(in)
	if strings.Contains(string(out), `"value":"v"`) {
		t.Fatalf("nested value leaked: %s", out)
	}
	if !strings.Contains(string(out), `"deploymentId":"my-app"`) {
		t.Fatalf("other fields should remain: %s", out)
	}
}

func TestRedactJSONValues_UnchangedWithoutValueKey(t *testing.T) {
	in := []byte(`{"data":[{"id":"h100","name":"H100"}]}`)
	out := redactJSONValues(in)
	if !bytes.Equal(out, in) {
		t.Fatalf("expected original bytes, got %s", out)
	}
}

func TestRedactJSONValues_InvalidJSON(t *testing.T) {
	in := []byte("not-json")
	if got := string(redactJSONValues(in)); got != "not-json" {
		t.Fatalf("got %q", got)
	}
}

func TestDebugLogBody_RedactsSuccessKeepsError(t *testing.T) {
	secret := []byte(`{"key":"MY_KEY","value":"super-secret"}`)
	got := debugLogBody(http.StatusOK, secret)
	if strings.Contains(got, "super-secret") {
		t.Fatalf("200 leaked value: %s", got)
	}

	problem := []byte(`{"title":"Unprocessable Entity","status":422,"detail":"RUNTIME is a reserved platform name"}`)
	got = debugLogBody(http.StatusUnprocessableEntity, problem)
	if got != string(problem) {
		t.Fatalf("422 body should be unchanged, got %s", got)
	}
}

func TestLogResponse_NilBodyOmitsBody(t *testing.T) {
	var buf bytes.Buffer
	c := &Client{
		logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	c.logResponse(context.Background(), "/v1/secrets", http.StatusOK, nil)
	out := buf.String()
	if !strings.Contains(out, `"/v1/secrets"`) {
		t.Fatalf("path missing: %s", out)
	}
	if strings.Contains(out, `"body"`) || strings.Contains(out, "bodyBytes") {
		t.Fatalf("nil body should omit body attrs: %s", out)
	}
}

func TestDebugLogBody_CreatedRedacts(t *testing.T) {
	in := []byte(`{"environmentVariables":[{"key":"K","value":"hidden"}]}`)
	got := debugLogBody(http.StatusCreated, in)
	if strings.Contains(got, "hidden") {
		t.Fatalf("201 leaked value: %s", got)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("redacted body should stay JSON: %v", err)
	}
}
