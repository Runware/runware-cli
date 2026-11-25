package put

import (
	"fmt"
	"io"
	"strings"
)

// What a put did with one file.
const (
	ActionAdded     = "added"
	ActionReplaced  = "replaced"
	ActionUnchanged = "unchanged"
)

const productionEnvironment = "production"

// FileResult is what this invocation did with one file, including transfer retries.
type FileResult struct {
	Path      string `json:"path"`
	Action    string `json:"action"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	BytesSent int64  `json:"bytesSent"`
}

// Summary is what a put did to the Volume.
type Summary struct {
	Volume         string       `json:"volume"`
	Environment    string       `json:"environment"`
	Changed        bool         `json:"changed"`
	ManifestSHA256 string       `json:"manifestSha256,omitempty"`
	Added          int          `json:"added"`
	Replaced       int          `json:"replaced"`
	Reused         int          `json:"reused"` // Files that reused content, including added or replaced paths.
	BytesSent      int64        `json:"bytesSent"`
	Files          []FileResult `json:"files"`
}

// WriteText writes the summary as text.
func (s Summary) WriteText(w io.Writer) error {
	var b strings.Builder
	verb := "Uploaded to"
	if !s.Changed {
		verb = "No change to"
	}
	fmt.Fprintf(&b, "%s Volume %s (%s)\n", verb, s.Volume, s.Environment)
	digest := s.ManifestSHA256
	if digest == "" {
		digest = "none, the Volume has no files"
	}
	fmt.Fprintf(&b, "  Latest files digest: %s\n", digest)
	fmt.Fprintf(&b, "  Files: %d added, %d replaced, %d reused\n", s.Added, s.Replaced, s.Reused)
	fmt.Fprintf(&b, "  Bytes sent: %d (%s)\n", s.BytesSent, formatBytes(s.BytesSent))
	_, err := io.WriteString(w, b.String())
	return err
}
