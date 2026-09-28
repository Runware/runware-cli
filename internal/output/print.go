package output

import (
	"encoding/json"
	"io"
	"os"

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

func printYAML(w io.Writer, data any) error {
	enc := yaml.NewEncoder(w)
	defer enc.Close() //nolint:errcheck,gosec

	enc.SetIndent(2)
	return enc.Encode(data)
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
