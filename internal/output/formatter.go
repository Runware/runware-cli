package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"gopkg.in/yaml.v3"
)

// Format represents an output format.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
)

// ParseFormat parses a format string, defaulting to table.
func ParseFormat(s string) Format {
	switch strings.ToLower(s) {
	case "json":
		return FormatJSON
	case "yaml":
		return FormatYAML
	default:
		return FormatTable
	}
}

// Table holds headers and rows for table-format rendering.
type Table struct {
	Headers []string
	Rows    [][]any
}

// Print outputs data in the specified format.
// For JSON/YAML, data is serialised directly; t is ignored.
// For table, t is rendered via go-pretty. If t is nil, falls back to JSON.
func Print(format Format, data any, t *Table) error {
	switch format {
	case FormatJSON:
		return printJSON(data)
	case FormatYAML:
		return printYAML(data)
	default:
		if t == nil {
			return printJSON(data)
		}
		return printTable(t)
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

func printTable(t *Table) error {
	tw := table.NewWriter()
	tw.SetOutputMirror(os.Stdout)
	tw.SetStyle(table.StyleLight)

	header := make(table.Row, len(t.Headers))
	for i, h := range t.Headers {
		header[i] = h
	}
	tw.AppendHeader(header)

	for _, row := range t.Rows {
		tw.AppendRow(table.Row(row))
	}

	tw.Render()
	return nil
}

// Success prints a success message to stderr.
func Success(msg string) {
	fmt.Fprintf(os.Stderr, "✓ %s\n", msg)
}

// Error prints an error message to stderr.
func Error(msg string) {
	fmt.Fprintf(os.Stderr, "✗ %s\n", msg)
}

// Info prints an info message to stderr.
func Info(msg string) {
	fmt.Fprintf(os.Stderr, "• %s\n", msg)
}
