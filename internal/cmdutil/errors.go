package cmdutil

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/output"
	"gopkg.in/yaml.v3"
)

const fieldMessage = "message"

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
	PrintErrorTo(logger, os.Stderr, format, err)
}

// PrintErrorTo logs err with formatting that matches the CLI output format,
// writing structured output to w.
func PrintErrorTo(logger *log.Logger, w io.Writer, format output.Format, err error) {
	if errors.Is(err, transport.ErrNoAPIKey) {
		if isStructuredFormat(format) {
			writeStructuredError(w, format, structuredErrors(map[string]any{
				"code":       "missingApiKey",
				fieldMessage: "No API key configured",
				"hint":       "Run 'runware auth login' to set your API key",
			}))
			return
		}
		logger.Error("No API key configured. Run 'runware auth login' to set your API key.")
		return
	}

	var re *transport.RunwareError
	if errors.As(err, &re) {
		if isStructuredFormat(format) {
			writeStructuredError(w, format, structuredErrors(re.APIFields()))
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
		writeStructuredError(w, format, structuredErrors(map[string]any{
			fieldMessage: err.Error(),
		}))
		return
	}
	logger.Error(err.Error())
}

// PrintErrorMsg logs a custom message, appending structured API error fields
// when err is a *transport.RunwareError. Use for warning-style output that does
// not exit the process.
func PrintErrorMsg(logger *log.Logger, format output.Format, message string, err error) {
	PrintErrorMsgTo(logger, os.Stderr, format, message, err)
}

// PrintErrorMsgTo logs a custom message and writes structured output to w.
func PrintErrorMsgTo(logger *log.Logger, w io.Writer, format output.Format, message string, err error) {
	var re *transport.RunwareError
	if errors.As(err, &re) {
		if isStructuredFormat(format) {
			writeStructuredError(w, format, map[string]any{
				fieldMessage: message,
				"errors":     []map[string]any{re.APIFields()},
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

func structuredErrors(item map[string]any) map[string]any {
	return map[string]any{"errors": []map[string]any{item}}
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
		if k == fieldMessage {
			continue
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func writeStructuredError(w io.Writer, format output.Format, data any) {
	switch format {
	case output.FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(data) //nolint:errcheck,gosec
	case output.FormatYAML:
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		_ = enc.Encode(data) //nolint:errcheck,gosec
		_ = enc.Close()      //nolint:errcheck,gosec
	}
}
