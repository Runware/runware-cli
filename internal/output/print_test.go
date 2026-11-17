package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
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

// yamlKeyConnector mimics an API type: json tags in camelCase and no yaml tags, which
// a YAML encoder would print as serviceaccountemail.
type yamlKeyConnector struct {
	Name                string         `json:"name"`
	CreatedAt           time.Time      `json:"createdAt"`
	ServiceAccountEmail *string        `json:"serviceAccountEmail,omitempty"`
	Gcp                 yamlKeyGcp     `json:"gcp"`
	Attached            []yamlKeyGcp   `json:"attachedTo"`
	Labels              map[string]any `json:"labels"`
	Count               int64          `json:"requestCount"`
	Ratio               float64        `json:"p95Ratio"`
}

type yamlKeyGcp struct {
	WorkloadIdentityProvider string `json:"workloadIdentityProvider"`
}

// yamlPrintedOutput returns what PrintTo writes for data in format.
func yamlPrintedOutput(t *testing.T, format Format, data any) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := PrintTo(&buf, format, data); err != nil {
		t.Fatalf("PrintTo(%v): %v", format, err)
	}
	return buf.Bytes()
}

// The YAML of a value, read back, equals its JSON read back: the same keys and the
// same values, whatever a string looks like to a YAML reader.
func TestPrintYAMLEqualsJSON(t *testing.T) {
	email := "worker@my-project.iam.gserviceaccount.com"
	value := yamlKeyConnector{
		Name:                "gcp-prod",
		CreatedAt:           time.Date(2026, 11, 16, 13, 12, 6, 0, time.UTC),
		ServiceAccountEmail: &email,
		Gcp:                 yamlKeyGcp{WorkloadIdentityProvider: "projects/1/locations/global/workloadIdentityPools/p/providers/q"},
		Attached:            []yamlKeyGcp{{WorkloadIdentityProvider: "a"}, {WorkloadIdentityProvider: "b"}},
		Labels: map[string]any{
			"yes":        "yes",
			"number":     "123",
			"bool":       "true",
			"null":       "null",
			"timestamp":  "2026-11-16T13:12:06Z",
			"colon":      "a: b",
			"hash":       "# not a comment",
			"html":       "<b>&</b>",
			"empty":      "",
			"multi":      "line one\nline two\n",
			"unicode":    "zażółć 🚀",
			"leadspace":  "  indented",
			"realNumber": 42,
			"realBool":   false,
			"nothing":    nil,
			"nested":     map[string]any{"deepKey": []any{1, "two", map[string]any{"threeKey": 3.5}}},
		},
		Count: 7,
		Ratio: 0.25,
	}

	var fromJSON, fromYAML any
	if err := json.Unmarshal(yamlPrintedOutput(t, FormatJSON, value), &fromJSON); err != nil {
		t.Fatalf("parse JSON output: %v", err)
	}
	if err := yaml.Unmarshal(yamlPrintedOutput(t, FormatYAML, value), &fromYAML); err != nil {
		t.Fatalf("parse YAML output: %v", err)
	}
	// A YAML reader yields int where a JSON reader yields float64; compare as JSON.
	asJSON, err := json.Marshal(fromYAML)
	if err != nil {
		t.Fatalf("encode parsed YAML as JSON: %v", err)
	}
	if err := json.Unmarshal(asJSON, &fromYAML); err != nil {
		t.Fatalf("parse parsed YAML as JSON: %v", err)
	}
	if !reflect.DeepEqual(fromYAML, fromJSON) {
		t.Fatalf("YAML read back differs from JSON read back\nyaml: %#v\njson: %#v", fromYAML, fromJSON)
	}
}

// The text itself: the API's key names, in the struct's field order, as block YAML.
func TestPrintYAMLTextUsesTheJSONKeys(t *testing.T) {
	email := "worker@my-project.iam.gserviceaccount.com"
	got := string(yamlPrintedOutput(t, FormatYAML, yamlKeyConnector{
		Name:                "gcp-prod",
		CreatedAt:           time.Date(2026, 11, 16, 13, 12, 6, 0, time.UTC),
		ServiceAccountEmail: &email,
		Gcp:                 yamlKeyGcp{WorkloadIdentityProvider: "wip"},
		Attached:            []yamlKeyGcp{{WorkloadIdentityProvider: "a"}},
		Count:               7,
		Ratio:               0.5,
	}))
	want := `name: gcp-prod
createdAt: "2026-11-16T13:12:06Z"
serviceAccountEmail: worker@my-project.iam.gserviceaccount.com
gcp:
  workloadIdentityProvider: wip
attachedTo:
  - workloadIdentityProvider: a
labels: null
requestCount: 7
p95Ratio: 0.5
`
	if got != want {
		t.Fatalf("YAML output:\n%s\nwant:\n%s", got, want)
	}
}

// A count above 2^53 keeps every digit: reading the JSON into a float64 would round it.
func TestPrintYAMLKeepsLargeIntegersExact(t *testing.T) {
	got := string(yamlPrintedOutput(t, FormatYAML, yamlKeyConnector{Name: "n", Count: 9007199254740993}))
	if !strings.Contains(got, "requestCount: 9007199254740993\n") {
		t.Fatalf("YAML output lost the integer:\n%s", got)
	}
}

// YAML 1.1 readers (PyYAML, Ruby, yq v3) read yes/no/on/off, 1:30:00 and << as a
// boolean, a number and a merge key unless they are quoted; the test reads the text,
// because a YAML 1.2 parser reads them back as strings either way.
func TestPrintYAMLQuotesWhatYAML11Misreads(t *testing.T) {
	// A literal list, not yaml11Words: a word dropped from the set must fail here.
	quoted := []string{
		"y", "Y", "yes", "Yes", "YES", "n", "N", "no", "No", "NO",
		"on", "On", "ON", "off", "Off", "OFF", "<<", "=",
		"1:30:00", "-1:30", "+1:30", "1:30:00.5", "1_0:30",
	}
	plain := []string{"hello", "1h", "1:99", "a1:30", "yesterday", "on-demand"}

	for _, word := range quoted {
		if got := yamlScalar(t, word); got != `"`+word+`"` {
			t.Errorf("%q printed as %s, want it quoted", word, got)
		}
	}
	for _, word := range plain {
		if got := yamlScalar(t, word); got != word {
			t.Errorf("%q printed as %s, want it plain", word, got)
		}
	}
}

// yamlScalar prints a one-key document and returns the text after "k: ".
func yamlScalar(t *testing.T, value string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := printYAML(&buf, map[string]any{"k": value}); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(strings.TrimPrefix(buf.String(), "k: "), "\n")
}

// A raw JSON document may hold what encoding/json never writes itself: a "\/" escape, a
// raw U+007F or U+0085. yaml.Unmarshal would fail on the first two and turn the third into a
// space; the value must come out as it went in.
func TestPrintYAMLKeepsAnyJSONString(t *testing.T) {
	for name, raw := range map[string]string{
		"escaped slash":      `{"a":"x\/y"}`,
		"raw DEL":            "{\"a\":\"x\u007fy\"}",
		"raw NEL":            "{\"a\":\"x\u0085y\"}",
		"escaped DEL":        `{"a":"\u007f"}`,
		"escaped NEL":        `{"a":"\u0085"}`,
		"surrogate pair":     `{"a":"\ud83d\ude80"}`,
		"large exact number": `{"a":9007199254740993,"b":1e+21,"c":-0.5}`,
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := printYAML(&buf, json.RawMessage(raw)); err != nil {
				t.Fatal(err)
			}
			var fromJSON, fromYAML map[string]any
			if err := json.Unmarshal([]byte(raw), &fromJSON); err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal(buf.Bytes(), &fromYAML); err != nil {
				t.Fatalf("not YAML: %v\n%s", err, buf.String())
			}
			for key, want := range fromJSON {
				if s, isString := want.(string); isString && fromYAML[key] != s {
					t.Errorf("%s = %q, want %q", key, fromYAML[key], s)
				}
			}
		})
	}
}
