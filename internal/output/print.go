package output

import (
	"encoding/json"
	"io"
	"os"
	"regexp"

	"github.com/jedib0t/go-pretty/v6/table"
	"gopkg.in/yaml.v3"
)

// Print outputs data in the specified format.
// For JSON/YAML, data is serialized directly.
// For table, data must implement Tabular; if it does not, an error is returned.
func Print(format Format, data any) error {
	return PrintTo(os.Stdout, format, data)
}

// PrintTo writes data to w in the specified format. Table output requires Tabular.
func PrintTo(w io.Writer, format Format, data any) error {
	switch format {
	case FormatJSON:
		return printJSON(w, data)
	case FormatYAML:
		return printYAML(w, data)
	default:
		t, ok := data.(Tabular)
		if !ok {
			return NotTabularError{Got: data}
		}
		printTable(w, t)
		return nil
	}
}

func printJSON(w io.Writer, data any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

// printYAML prints data with the same keys and values as printJSON. A YAML encoder
// ignores json tags and lowercases a field's Go name (createdAt becomes createdat),
// so the value goes through its JSON encoding first: JSON is YAML, and what
// MarshalJSON and the json tags decide is what the YAML shows.
func printYAML(w io.Writer, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	blockStyle(&doc)

	enc := yaml.NewEncoder(w)
	defer enc.Close() //nolint:errcheck,gosec

	enc.SetIndent(2)
	return enc.Encode(&doc)
}

// blockStyle drops the flow and double-quoted styles a JSON document parses with, so
// the output reads as block YAML. yaml.v3 keeps a string quoted when YAML 1.2 would read
// it as another type ("123", "true", a timestamp); misreadByYAML11 adds the strings that
// only YAML 1.1 readers (PyYAML, Ruby, yq v3) misread.
func blockStyle(n *yaml.Node) {
	n.Style = 0
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" && misreadByYAML11(n.Value) {
		n.Style = yaml.DoubleQuotedStyle
	}
	for _, c := range n.Content {
		blockStyle(c)
	}
}

// yaml11Words are the plain scalars YAML 1.1 reads as booleans, plus the merge key.
var yaml11Words = map[string]bool{
	"y": true, "Y": true, "yes": true, "Yes": true, "YES": true,
	"n": true, "N": true, "no": true, "No": true, "NO": true,
	"on": true, "On": true, "ON": true, "off": true, "Off": true, "OFF": true,
	"<<": true, "=": true,
}

// yaml11Sexagesimal matches base-60 numbers such as 1:30:00, which YAML 1.1 reads as an integer or float.
var yaml11Sexagesimal = regexp.MustCompile(`^[-+]?[0-9][0-9_]*(:[0-5]?[0-9])+(\.[0-9_]*)?$`)

func misreadByYAML11(s string) bool {
	return yaml11Words[s] || yaml11Sexagesimal.MatchString(s)
}

func printTable(w io.Writer, t Tabular) {
	tw := table.NewWriter()
	tw.SetOutputMirror(w)
	tw.SetStyle(table.StyleLight)

	header := make(table.Row, len(t.Headers()))
	for i, h := range t.Headers() {
		header[i] = h
	}
	tw.AppendHeader(header)

	for _, row := range t.Rows() {
		tw.AppendRow(table.Row(row))
	}

	tw.Render()
}
