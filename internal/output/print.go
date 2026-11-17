package output

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

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
	// The node tree is built from the JSON tokens, not from yaml.Unmarshal(raw): yaml.v3
	// reads JSON as YAML and rejects or changes valid JSON strings (a "\/" escape, a raw
	// U+007F, a raw U+0085).
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	root, err := jsonNode(dec)
	if err != nil {
		return err
	}
	doc := yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}

	enc := yaml.NewEncoder(w)
	defer enc.Close() //nolint:errcheck,gosec

	enc.SetIndent(2)
	return enc.Encode(&doc)
}

// jsonNode reads one JSON value as a YAML node. Objects keep their key order and numbers
// keep their digits. The nodes carry no style, so the encoder writes block YAML and quotes
// what YAML 1.2 would read as another type ("123", "true", a timestamp); quoteYAML11
// adds the strings that only YAML 1.1 readers (PyYAML, Ruby, yq v3) misread.
func jsonNode(dec *json.Decoder) (*yaml.Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch v := tok.(type) {
	case json.Delim:
		kind, tag := yaml.SequenceNode, "!!seq"
		if v == '{' {
			kind, tag = yaml.MappingNode, "!!map"
		}
		n := &yaml.Node{Kind: kind, Tag: tag}
		for dec.More() {
			if kind == yaml.MappingNode {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := keyTok.(string)
				n.Content = append(n.Content, quoteYAML11(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}))
			}
			child, err := jsonNode(dec)
			if err != nil {
				return nil, err
			}
			n.Content = append(n.Content, child)
		}
		if _, err := dec.Token(); err != nil { // the closing } or ]
			return nil, err
		}
		return n, nil
	case string:
		return quoteYAML11(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}), nil
	case json.Number:
		tag := "!!int"
		if strings.ContainsAny(string(v), ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: string(v)}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}, nil
	default: // null
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}
}

// quoteYAML11 double-quotes a string scalar that a YAML 1.1 reader would misread.
func quoteYAML11(n *yaml.Node) *yaml.Node {
	if misreadByYAML11(n.Value) {
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

// yaml11Words are the plain scalars YAML 1.1 reads as a boolean, the merge key (<<) or the value key (=).
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
