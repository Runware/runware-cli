package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootLongMatchesReadme(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	const prefix = "Run image generation"
	var blurb string
	for line := range strings.SplitSeq(string(readme), "\n") {
		if strings.HasPrefix(line, prefix) {
			blurb = line
			break
		}
	}
	if blurb == "" {
		t.Fatalf("README has no opening description starting with %q", prefix)
	}
	cmd := NewRootCmd(newLogger())
	if !strings.Contains(cmd.Long, blurb) {
		t.Fatalf("root Long drifted from README:\nLong:\n%s\nREADME:\n%s", cmd.Long, blurb)
	}
}
