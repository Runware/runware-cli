package serverless

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadValueFlag_FromFlag(t *testing.T) {
	got, err := readValueFlag("keep\n", "", strings.NewReader("ignored"))
	if err != nil {
		t.Fatalf("readValueFlag: %v", err)
	}
	if got != "keep\n" {
		t.Fatalf("flag value should be used as-is, got %q", got)
	}
}

func TestReadValueFlag_FromFileStripsTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "value.txt")
	if err := os.WriteFile(path, []byte("hello\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readValueFlag("", path, strings.NewReader(""))
	if err != nil {
		t.Fatalf("readValueFlag: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %q, want hello", got)
	}
}

func TestReadValueFlag_FromStdin(t *testing.T) {
	got, err := readValueFlag("", "-", strings.NewReader("from-stdin\n"))
	if err != nil {
		t.Fatalf("readValueFlag: %v", err)
	}
	if got != "from-stdin" {
		t.Fatalf("got %q, want from-stdin", got)
	}
}

func TestReadValueFlag_MissingFile(t *testing.T) {
	_, err := readValueFlag("", filepath.Join(t.TempDir(), "missing.txt"), strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
