package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/rodaine/table"
	"golang.org/x/term"
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

// Print outputs data in the specified format.
// For table format, provide headers and rows.
// For json/yaml, data is serialized directly.
func Print(format Format, data any, headers []any, rows [][]any) error {
	switch format {
	case FormatJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	case FormatYAML:
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		return enc.Encode(data)
	default:
		if len(headers) == 0 {
			return nil
		}
		tbl := table.New(headers...)
		for _, row := range rows {
			tbl.AddRow(row...)
		}
		tbl.Print()
		return nil
	}
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

// IsTTY returns true if stderr is a terminal (for spinner decisions).
func IsTTY() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}
