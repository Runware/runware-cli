package output

import (
	"encoding/json"
	"os"

	"github.com/jedib0t/go-pretty/v6/table"
	"gopkg.in/yaml.v3"
)

// Print outputs data in the specified format.
// For JSON/YAML, data is serialised directly.
// For table, data must implement Tabular; if it does not, falls back to JSON.
func Print(format Format, data any) error {
	switch format {
	case FormatJSON:
		return printJSON(data)
	case FormatYAML:
		return printYAML(data)
	default:
		if t, ok := data.(Tabular); ok {
			return printTable(t)
		}
		return printJSON(data)
	}
}

func printJSON(data any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func printYAML(data any) error {
	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(2)
	return enc.Encode(data)
}

func printTable(t Tabular) error {
	tw := table.NewWriter()
	tw.SetOutputMirror(os.Stdout)
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
	return nil
}
