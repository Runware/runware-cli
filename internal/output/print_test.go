package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

const testValue = "value"

func TestPrintJSON(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]string{"key": testValue}
	err := Print(FormatJSON, data)

	w.Close() //nolint:errcheck,gosec
	os.Stdout = old

	if err != nil {
		t.Fatalf("Print(JSON) error: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r) //nolint:errcheck,gosec
	out := buf.String()

	var parsed map[string]string
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if parsed["key"] != testValue {
		t.Errorf("parsed[key] = %q, want %q", parsed["key"], testValue)
	}
}

func TestPrintYAML(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]string{"key": testValue}
	err := Print(FormatYAML, data)

	w.Close() //nolint:errcheck,gosec
	os.Stdout = old

	if err != nil {
		t.Fatalf("Print(YAML) error: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r) //nolint:errcheck,gosec
	out := buf.String()

	if out == "" {
		t.Error("YAML output is empty")
	}
}

// testTabular is a minimal Tabular implementation for testing.
type testTabular struct {
	headers []string
	rows    [][]any
}

func (t testTabular) Headers() []string { return t.headers }
func (t testTabular) Rows() [][]any     { return t.rows }

func TestPrintTable(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := Print(FormatTable, testTabular{
		headers: []string{"Name", "Value"},
		rows:    [][]any{{"foo", "bar"}, {"baz", "qux"}},
	})

	w.Close() //nolint:errcheck,gosec
	os.Stdout = old

	if err != nil {
		t.Fatalf("Print(Table) error: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r) //nolint:errcheck,gosec
	out := buf.String()

	for _, want := range []string{"NAME", "VALUE", "foo", "bar", "baz", "qux"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q\noutput: %s", want, out)
		}
	}
}

func TestPrintTable_NonTabularReturnsError(t *testing.T) {
	data := map[string]string{"key": testValue}
	err := Print(FormatTable, data)
	var notTabular NotTabularError
	if !errors.As(err, &notTabular) {
		t.Fatalf("expected ErrNotTabular, got %v", err)
	}
	if notTabular.Got == nil {
		t.Error("ErrNotTabular.Got should not be nil")
	}
}
