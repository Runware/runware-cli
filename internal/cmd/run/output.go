package run

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/cmdutil"
	runwarehttp "github.com/runware/runware-cli/internal/http"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// mediaURLFields lists the known result fields that contain downloadable media URLs.
// The order determines the download filename suffix.
var mediaURLFields = []struct {
	key string
	ext string
}{
	{"imageURL", ""}, // extension inferred from URL
	{"videoURL", ""},
	{"mediaURL", ""},
	{"audioURL", ""},
}

// runResult wraps a single parsed inference result for structured rendering.
type runResult struct {
	fields []runResultField
}

type runResultField struct {
	key   string
	value string
}

func (r runResult) Headers() []string {
	return []string{"Field", "Value"}
}

func (r runResult) Rows() [][]any {
	rows := make([][]any, len(r.fields))
	for i, f := range r.fields {
		rows[i] = []any{f.key, f.value}
	}
	return rows
}

// MarshalJSON delegates to the underlying field map so JSON output is an object.
func (r runResult) MarshalJSON() ([]byte, error) {
	m := make(map[string]string, len(r.fields))
	for _, f := range r.fields {
		m[f.key] = f.value
	}
	return json.Marshal(m)
}

// handleResults outputs each raw API result and optionally downloads media files.
func handleResults(cmd *cobra.Command, logger *log.Logger, results []json.RawMessage, outputDir string, noDownload bool) error {
	format := cmdutil.FormatFor(cmd)

	for i, raw := range results {
		// Parse raw result into a generic map.
		var parsed map[string]any
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return fmt.Errorf("failed to parse result %d: %w", i, err)
		}

		if format == output.FormatJSON || format == output.FormatYAML {
			// Emit raw JSON / YAML directly without transformation.
			if err := emitRaw(format, parsed); err != nil {
				return err
			}
		} else {
			// Table: flatten to key-value rows, surfacing important fields first.
			res := buildRunResult(parsed)
			if err := output.Print(format, res); err != nil {
				return err
			}
		}

		if !noDownload {
			downloadMedia(cmd.Context(), logger, parsed, outputDir, i, len(results) > 1)
		}
	}
	return nil
}

// emitRaw prints a single parsed result as JSON or YAML.
func emitRaw(format output.Format, parsed map[string]any) error {
	// Re-use output.Print by encoding the map; JSON/YAML marshalers handle maps natively.
	return output.Print(format, jsonMapValue(parsed))
}

// jsonMapValue is a thin wrapper that makes map[string]any serialisable via output.Print.
type jsonMapValue map[string]any

func (v jsonMapValue) MarshalJSON() ([]byte, error) { return json.Marshal(map[string]any(v)) }
func (v jsonMapValue) MarshalYAML() (any, error)    { return map[string]any(v), nil }

// buildRunResult converts a parsed API result map into an ordered key-value table.
// Priority fields (imageURL, videoURL, audioURL, mediaURL, text, taskUUID) are surfaced
// first; remaining fields are appended sorted alphabetically.
func buildRunResult(parsed map[string]any) runResult {
	priorityKeys := []string{"taskUUID", "imageURL", "videoURL", "audioURL", "mediaURL", "text", "finishReason", "seed", "cost"}
	seen := make(map[string]bool)

	var fields []runResultField

	for _, k := range priorityKeys {
		v, ok := parsed[k]
		if !ok {
			continue
		}
		fields = append(fields, runResultField{key: k, value: formatValue(v)})
		seen[k] = true
	}

	// Append remaining fields in sorted order, skipping internal/redundant ones.
	skipKeys := map[string]bool{"taskType": true, "taskUUID": true}
	remaining := sortedKeys(parsed)
	for _, k := range remaining {
		if seen[k] || skipKeys[k] {
			continue
		}
		fields = append(fields, runResultField{key: k, value: formatValue(parsed[k])})
	}

	return runResult{fields: fields}
}

// formatValue converts an arbitrary JSON value to a display string.
func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case nil:
		return "—"
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	}
}

// sortedKeys returns the keys of a map sorted alphabetically.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// simple insertion sort for small maps; avoids importing sort in this file
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// downloadMedia scans the parsed result for known media URL fields and
// downloads each to outputDir. Index and multi are used to generate filenames
// when multiple results are returned.
func downloadMedia(ctx context.Context, logger *log.Logger, parsed map[string]any, outputDir string, idx int, multi bool) {
	for _, mf := range mediaURLFields {
		urlVal, ok := parsed[mf.key]
		if !ok {
			continue
		}
		urlStr, ok := urlVal.(string)
		if !ok || urlStr == "" {
			continue
		}

		destPath := buildDestPath(outputDir, mf.key, urlStr, idx, multi)
		if err := runwarehttp.Download(ctx, urlStr, destPath, 5*time.Minute); err != nil {
			logger.Warn("failed to download "+mf.key, "url", urlStr, "err", err)
			continue
		}
		fmt.Fprintf(os.Stderr, "Saved %s → %s\n", mf.key, destPath)
	}
}

// buildDestPath constructs the local file path for a downloaded media file.
// It derives the file extension from the URL when possible.
func buildDestPath(outputDir, field, urlStr string, idx int, multi bool) string {
	// Extract extension from URL path (before any query string).
	ext := ""
	rawPath := urlStr
	if i := strings.Index(rawPath, "?"); i != -1 {
		rawPath = rawPath[:i]
	}
	if dot := strings.LastIndex(rawPath, "."); dot != -1 {
		candidate := rawPath[dot:] // e.g. ".png"
		if len(candidate) <= 6 {   // sanity: extensions are short
			ext = candidate
		}
	}

	base := fieldBaseName(field)
	if multi {
		base = fmt.Sprintf("%s-%d", base, idx+1)
	}
	return filepath.Join(outputDir, base+ext)
}

// fieldBaseName returns a short lowercase filename stem for a media URL field.
func fieldBaseName(field string) string {
	switch field {
	case "imageURL":
		return "image"
	case "videoURL", "mediaURL":
		return "video"
	case "audioURL":
		return "audio"
	default:
		return "output"
	}
}
