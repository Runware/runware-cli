package output

import (
	"fmt"
	"strings"
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

// Tabular is implemented by result types that know how to render themselves
// as a table. Print uses this when the format is table; the same value is
// serialised directly for JSON/YAML.
type Tabular interface {
	Headers() []string
	Rows() [][]any
}

// NotTabularError is returned by Print when table format is requested but the
// data does not implement Tabular. Use errors.As to inspect the type.
type NotTabularError struct {
	Got any
}

func (e NotTabularError) Error() string {
	return fmt.Sprintf("table format requires a Tabular value, got %T", e.Got)
}
