package cmdutil

import (
	"encoding/json"
	"errors"
	"os"
	"slices"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/output"
	"gopkg.in/yaml.v3"
)

// PrintError logs err with formatting that matches the CLI output format.
//
// Special cases:
//   - transport.ErrNoAPIKey → friendly "no key" message with auth hint
//   - *transport.RunwareError CodeAuth → "Authentication failed" with auth hint
//   - *transport.RunwareError other → full API error fields (JSON/YAML) or
//     structured key-value fields (table)
//
// For all other errors, err.Error() is used as the message.
func PrintError(logger *log.Logger, format output.Format, err error) {
	if errors.Is(err, transport.ErrNoAPIKey) {
		if isStructuredFormat(format) {
			writeStructuredError(format, map[string]any{
				"error": "No API key configured",
				"hint":  "Run 'runware auth login' to set your API key",
			})
			return
		}
		logger.Error("No API key configured. Run 'runware auth login' to set your API key.")
		return
	}

	var re *transport.RunwareError
	if errors.As(err, &re) {
		if isStructuredFormat(format) {
			writeStructuredError(format, map[string]any{
				"errors": []map[string]any{re.APIFields()},
			})
			return
		}

		switch re.Code {
		case transport.CodeAuth:
			logRunwareError(logger, re, "Authentication failed. Run 'runware auth status' to verify your credentials.")
		default:
			logRunwareError(logger, re, re.Message)
		}
		return
	}

	if isStructuredFormat(format) {
		writeStructuredError(format, map[string]any{"error": err.Error()})
		return
	}
	logger.Error(err.Error())
}

// PrintErrorMsg logs a custom message, appending structured API error fields
// when err is a *transport.RunwareError. Use for warning-style output that does
// not exit the process.
func PrintErrorMsg(logger *log.Logger, format output.Format, message string, err error) {
	var re *transport.RunwareError
	if errors.As(err, &re) {
		if isStructuredFormat(format) {
			fields := re.APIFields()
			fields["message"] = message
			writeStructuredError(format, map[string]any{
				"errors": []map[string]any{fields},
			})
			return
		}
		logRunwareError(logger, re, message)
		return
	}
	logger.Error(message)
}

func isStructuredFormat(format output.Format) bool {
	return format == output.FormatJSON || format == output.FormatYAML
}

func logRunwareError(logger *log.Logger, re *transport.RunwareError, message string) {
	args := runwareErrorLogArgs(re)
	if len(args) == 0 {
		logger.Error(message)
		return
	}
	logger.Error(message, args...)
}

func runwareErrorLogArgs(re *transport.RunwareError) []any {
	fields := re.APIFields()
	keys := sortedFieldKeys(fields)
	args := make([]any, 0, len(keys)*2)
	for _, key := range keys {
		args = append(args, key, fields[key])
	}
	return args
}

func sortedFieldKeys(fields map[string]any) []string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		if k == "message" {
			continue
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func writeStructuredError(format output.Format, data any) {
	switch format {
	case output.FormatJSON:
		enc := json.NewEncoder(os.Stderr)
		enc.SetIndent("", "  ")
		_ = enc.Encode(data) //nolint:errcheck,gosec
	case output.FormatYAML:
		enc := yaml.NewEncoder(os.Stderr)
		enc.SetIndent(2)
		_ = enc.Encode(data) //nolint:errcheck,gosec
		_ = enc.Close()      //nolint:errcheck,gosec
	}
}
