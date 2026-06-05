package run

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/cmdutil"
	runwarehttp "github.com/runware/runware-cli/internal/http"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// taskTypeMediaField maps each inference task type to the top-level result
// field that carries the downloadable media URL.
var taskTypeMediaField = map[string]string{
	taskTypeImage: fieldImageURL,
	taskTypeVideo: fieldVideoURL,
	taskTypeAudio: fieldAudioURL,
	taskType3D:    fieldModelURL,
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
func handleResults(cmd *cobra.Command, logger *log.Logger, results []json.RawMessage, outputDir string, noDownload bool, spin *spinner.Spinner) error {
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
		} else if tt, _ := parsed[fieldTaskType].(string); tt == taskTypeText {
			// Text inference: print the text field raw to stdout.
			// A divider separates multiple results (e.g. numberResults > 1).
			if i > 0 {
				fmt.Println("---")
			}

			if s, ok := parsed[fieldText].(string); ok {
				fmt.Println(s)
			}
		} else {
			// Table: flatten to key-value rows, surfacing important fields first.
			res := buildRunResult(parsed)
			if err := output.Print(format, res); err != nil {
				return err
			}
		}

		if !noDownload {
			downloadMedia(cmd.Context(), logger, parsed, outputDir, i, len(results) > 1, spin)
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

// extractOutputFileURLs extracts URL strings from the nested outputs.files[]
// structure used by 3D inference responses:
//
//	{"outputs": {"files": [{"url": "https://...", "uuid": "..."}]}}
//
// Returns nil if the structure is absent or malformed.
func extractOutputFileURLs(parsed map[string]any) []string {
	outputsVal, ok := parsed[fieldOutputs]
	if !ok {
		return nil
	}
	outputsMap, ok := outputsVal.(map[string]any)
	if !ok {
		return nil
	}
	filesVal, ok := outputsMap[fieldOutputFiles]
	if !ok {
		return nil
	}
	filesSlice, ok := filesVal.([]any)
	if !ok {
		return nil
	}
	var urls []string
	for _, item := range filesSlice {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		urlStr, ok := itemMap[fieldOutputURL].(string)
		if ok && urlStr != "" {
			urls = append(urls, urlStr)
		}
	}
	return urls
}

// buildRunResult converts a parsed API result map into an ordered key-value table.
// Priority fields (imageURL, videoURL, audioURL, text, taskUUID) are surfaced
// first; remaining fields are appended sorted alphabetically.
// For 3D inference results the nested outputs.files[].url entries are expanded
// into individual "file" rows so the table stays readable.
func buildRunResult(parsed map[string]any) runResult {
	priorityKeys := []string{fieldImageURL, fieldVideoURL, fieldAudioURL, fieldText, fieldTaskUUID, "finishReason", "seed", "cost"}
	seen := make(map[string]struct{})

	var fields []runResultField

	for _, k := range priorityKeys {
		v, ok := parsed[k]
		if !ok {
			continue
		}
		fields = append(fields, runResultField{key: k, value: formatValue(v)})
		seen[k] = struct{}{}
	}

	// Expand outputs.files[].url into individual rows (e.g. 3D inference .glb files).
	if fileURLs := extractOutputFileURLs(parsed); len(fileURLs) > 0 {
		for i, u := range fileURLs {
			key := "file"
			if len(fileURLs) > 1 {
				key = fmt.Sprintf("file.%d", i+1)
			}
			fields = append(fields, runResultField{key: key, value: u})
		}
		seen[fieldOutputs] = struct{}{} // suppress the raw JSON blob in the remaining loop
	}

	// Append remaining fields in sorted order, skipping internal/redundant ones.
	skipKeys := map[string]struct{}{
		fieldTaskType: {},
		fieldTaskUUID: {},
	}
	remaining := slices.Sorted(maps.Keys(parsed))
	for _, k := range remaining {
		if _, ok := seen[k]; ok {
			continue
		}
		if _, ok := skipKeys[k]; ok {
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

// downloadMedia scans the parsed result for known media URL fields and
// downloads each to outputDir. Index and multi are used to generate filenames
// when multiple results are returned. The spinner is updated to show progress.
func downloadMedia(ctx context.Context, logger *log.Logger, parsed map[string]any, outputDir string, idx int, multi bool, spin *spinner.Spinner) {
	// Flat top-level URL field derived from the task type.
	tt, _ := parsed[fieldTaskType].(string)
	if field, ok := taskTypeMediaField[tt]; ok {
		urlStr, _ := parsed[field].(string)
		if urlStr != "" {
			destPath := buildDestPath(outputDir, field, urlStr, idx, multi)

			spin.Suffix = fmt.Sprintf(" Downloading %s...", field)
			spin.Start()
			dlErr := runwarehttp.Download(ctx, urlStr, destPath, 5*time.Minute)
			spin.Stop()

			if dlErr != nil {
				logger.Warn("failed to download "+field, "url", urlStr, "err", dlErr)
			} else {
				logger.Info("saved", "path", destPath)
			}
		}
	}

	// Nested outputs.files[].url (used by 3D inference and similar task types).
	outputURLs := extractOutputFileURLs(parsed)
	for i, urlStr := range outputURLs {
		label := fmt.Sprintf("%s.%s[%d]", fieldOutputs, fieldOutputFiles, i)
		destPath := buildDestPath(outputDir, fieldOutputs, urlStr, i, len(outputURLs) > 1)

		spin.Suffix = fmt.Sprintf(" Downloading %s...", label)
		spin.Start()
		dlErr := runwarehttp.Download(ctx, urlStr, destPath, 5*time.Minute)
		spin.Stop()

		if dlErr != nil {
			logger.Warn("failed to download "+label, "url", urlStr, "err", dlErr)
			continue
		}
		logger.Info("saved", "path", destPath)
	}
}

// buildDestPath constructs the local file path for a downloaded media file.
// It preserves the original filename from the URL when possible, falling back
// to a generic stem derived from the field name.
func buildDestPath(outputDir, field, urlStr string, idx int, multi bool) string {
	if u, err := url.Parse(urlStr); err == nil {
		// Attempt to use the original filename from the URL path.
		if seg := path.Base(u.Path); seg != "" && seg != "." && seg != "/" {
			return filepath.Join(outputDir, seg)
		}

		// Fallback: derive extension from URL path + generic stem.
		ext := ""
		if dot := strings.LastIndex(u.Path, "."); dot != -1 {
			candidate := u.Path[dot:] // e.g. ".png"
			if len(candidate) <= 6 {  // sanity: extensions are short
				ext = candidate
			}
		}
		base := fieldBaseName(field)
		if multi {
			base = fmt.Sprintf("%s-%d", base, idx+1)
		}
		return filepath.Join(outputDir, base+ext)
	}

	// Last resort when URL cannot be parsed at all.
	base := fieldBaseName(field)
	if multi {
		base = fmt.Sprintf("%s-%d", base, idx+1)
	}
	return filepath.Join(outputDir, base)
}

// fieldBaseName returns a short lowercase filename stem for a media URL field.
func fieldBaseName(field string) string {
	switch field {
	case fieldImageURL:
		return "image"
	case fieldVideoURL:
		return "video"
	case fieldAudioURL:
		return "audio"
	case fieldOutputs:
		return "file"
	default:
		return "output"
	}
}
