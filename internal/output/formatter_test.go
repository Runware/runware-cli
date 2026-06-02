package output

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected Format
	}{
		{"json", FormatJSON},
		{"JSON", FormatJSON},
		{"yaml", FormatYAML},
		{"YAML", FormatYAML},
		{"table", FormatTable},
		{"TABLE", FormatTable},
		{"", FormatTable},
		{"unknown", FormatTable},
	}

	for _, tt := range tests {
		got := ParseFormat(tt.input)
		if got != tt.expected {
			t.Errorf("ParseFormat(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestPrintJSON(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]string{"key": "value"}
	err := Print(FormatJSON, data, nil)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("Print(JSON) error: %v", err)
	}

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	var parsed map[string]string
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if parsed["key"] != "value" {
		t.Errorf("parsed[key] = %q, want %q", parsed["key"], "value")
	}
}

func TestPrintYAML(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]string{"key": "value"}
	err := Print(FormatYAML, data, nil)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("Print(YAML) error: %v", err)
	}

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	if out == "" {
		t.Error("YAML output is empty")
	}
}

func TestPrintTable(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := Print(FormatTable, nil, &Table{
		Headers: []string{"Name", "Value"},
		Rows:    [][]any{{"foo", "bar"}, {"baz", "qux"}},
	})

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("Print(Table) error: %v", err)
	}

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	for _, want := range []string{"NAME", "VALUE", "foo", "bar", "baz", "qux"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q\noutput: %s", want, out)
		}
	}
}

func TestPrintTable_NilFallsBackToJSON(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]string{"key": "value"}
	err := Print(FormatTable, data, nil)

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("Print(Table,nil) error: %v", err)
	}

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	var parsed map[string]string
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("fallback output is not valid JSON: %v\noutput: %s", err, out)
	}
}
