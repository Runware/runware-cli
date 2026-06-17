package cmdutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/output"
	"gopkg.in/yaml.v3"
)

func runwareErrorFromRaw(t *testing.T, raw string) *transport.RunwareError {
	t.Helper()
	var re transport.RunwareError
	if err := json.Unmarshal([]byte(raw), &re); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	return &re
}

func TestPrintErrorStructuredOutput(t *testing.T) {
	fixtures := []struct {
		name string
		raw  string
	}{
		{
			name: "minimal",
			raw:  `{"code":"serverError","message":"internal error"}`,
		},
		{
			name: "validation constraints",
			raw:  `{"code":"invalidCustomHeight","message":"Invalid height.","parameter":"height","type":"integer","min":128,"max":2048,"multiplier":64}`,
		},
		{
			name: "allowed values",
			raw:  `{"code":"invalidParameter","message":"bad","parameter":"model","allowedValues":["a","b"]}`,
		},
	}

	logger := log.New(io.Discard)

	for _, tt := range fixtures {
		t.Run(tt.name+"/json", func(t *testing.T) {
			re := runwareErrorFromRaw(t, tt.raw)
			var buf bytes.Buffer
			PrintErrorTo(logger, &buf, output.FormatJSON, re)
			out := buf.String()

			var payload struct {
				Errors []map[string]any `json:"errors"`
			}
			if err := json.Unmarshal([]byte(out), &payload); err != nil {
				t.Fatalf("unmarshal stderr JSON: %v\noutput: %s", err, out)
			}
			if len(payload.Errors) != 1 {
				t.Fatalf("expected 1 error, got %d", len(payload.Errors))
			}

			var want map[string]any
			if err := json.Unmarshal([]byte(tt.raw), &want); err != nil {
				t.Fatalf("unmarshal want: %v", err)
			}
			if !mapsEqualJSON(t, payload.Errors[0], want) {
				t.Errorf("stderr error = %#v, want %#v", payload.Errors[0], want)
			}
		})

		t.Run(tt.name+"/yaml", func(t *testing.T) {
			re := runwareErrorFromRaw(t, tt.raw)
			var buf bytes.Buffer
			PrintErrorTo(logger, &buf, output.FormatYAML, re)
			out := buf.String()

			var payload struct {
				Errors []map[string]any `yaml:"errors"`
			}
			if err := yaml.Unmarshal([]byte(out), &payload); err != nil {
				t.Fatalf("unmarshal stderr YAML: %v\noutput: %s", err, out)
			}
			if len(payload.Errors) != 1 {
				t.Fatalf("expected 1 error, got %d", len(payload.Errors))
			}

			var want map[string]any
			if err := json.Unmarshal([]byte(tt.raw), &want); err != nil {
				t.Fatalf("unmarshal want: %v", err)
			}
			if !mapsEqualJSON(t, payload.Errors[0], want) {
				t.Errorf("stderr error = %#v, want %#v", payload.Errors[0], want)
			}
		})
	}
}

func TestPrintErrorNoAPIKeyJSON(t *testing.T) {
	logger := log.New(io.Discard)
	var buf bytes.Buffer
	PrintErrorTo(logger, &buf, output.FormatJSON, transport.ErrNoAPIKey)
	out := buf.String()

	var payload struct {
		Errors []map[string]any `json:"errors"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if len(payload.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(payload.Errors))
	}
	if payload.Errors[0]["code"] != "missingApiKey" {
		t.Errorf("code = %v", payload.Errors[0]["code"])
	}
	if payload.Errors[0]["message"] != "No API key configured" {
		t.Errorf("message = %v", payload.Errors[0]["message"])
	}
}

func TestPrintErrorGenericJSON(t *testing.T) {
	logger := log.New(io.Discard)
	var buf bytes.Buffer
	PrintErrorTo(logger, &buf, output.FormatJSON, errors.New("something broke"))
	out := buf.String()

	var payload struct {
		Errors []map[string]any `json:"errors"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if len(payload.Errors) != 1 || payload.Errors[0]["message"] != "something broke" {
		t.Errorf("unexpected payload: %#v", payload.Errors)
	}
}

func TestPrintErrorMsgPreservesAPIErrorMessage(t *testing.T) {
	const apiRaw = `{"code":"invalidCustomHeight","message":"Invalid height.","parameter":"height"}`
	re := runwareErrorFromRaw(t, apiRaw)

	logger := log.New(io.Discard)
	var buf bytes.Buffer
	PrintErrorMsgTo(logger, &buf, output.FormatJSON, "custom warning", re)
	out := buf.String()

	var payload struct {
		Message string           `json:"message"`
		Errors  []map[string]any `json:"errors"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if payload.Message != "custom warning" {
		t.Errorf("top-level message = %q, want %q", payload.Message, "custom warning")
	}
	if len(payload.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(payload.Errors))
	}
	if payload.Errors[0]["message"] != "Invalid height." {
		t.Errorf("API message overwritten: %v", payload.Errors[0]["message"])
	}
}

func mapsEqualJSON(t *testing.T, got, want map[string]any) bool {
	t.Helper()
	return reflect.DeepEqual(normalizeMapJSON(t, got), normalizeMapJSON(t, want))
}

func normalizeMapJSON(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal map: %v", err)
	}
	return out
}
