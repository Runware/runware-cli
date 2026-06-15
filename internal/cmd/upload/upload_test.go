package upload

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildImageInput_Passthrough: URLs and data URIs are forwarded unchanged.
func TestBuildImageInput_Passthrough(t *testing.T) {
	cases := []string{
		"http://example.com/a.png",
		"https://example.com/a.png",
		"HTTPS://EXAMPLE.COM/A.PNG",
		"data:image/png;base64,AAAA",
	}
	for _, in := range cases {
		got, err := buildImageInput(in)
		if err != nil {
			t.Errorf("buildImageInput(%q) error: %v", in, err)
			continue
		}
		if got != in {
			t.Errorf("buildImageInput(%q) = %q, want unchanged", in, got)
		}
	}
}

// TestBuildImageInput_LocalFile: a local image file is read and encoded as a
// data URI with the MIME type derived from its extension.
func TestBuildImageInput_LocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	content := []byte("fake-png-bytes")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	got, err := buildImageInput(path)
	if err != nil {
		t.Fatalf("buildImageInput error: %v", err)
	}

	wantPrefix := "data:image/png;base64,"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("got %q, want prefix %q", got, wantPrefix)
	}
	encoded := strings.TrimPrefix(got, wantPrefix)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if !bytes.Equal(decoded, content) {
		t.Errorf("decoded = %q, want %q", decoded, content)
	}
}

// TestBuildImageInput_UnsupportedExtension: an unsupported extension is rejected.
func TestBuildImageInput_UnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	if _, err := buildImageInput(path); err == nil {
		t.Fatal("expected error for unsupported extension, got nil")
	}
}

// TestBuildImageInput_MissingFile: a non-existent path returns an error.
func TestBuildImageInput_MissingFile(t *testing.T) {
	if _, err := buildImageInput(filepath.Join(t.TempDir(), "nope.png")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestBuildImageInput_Directory: a directory path is rejected.
func TestBuildImageInput_Directory(t *testing.T) {
	if _, err := buildImageInput(t.TempDir()); err == nil {
		t.Fatal("expected error for directory, got nil")
	}
}
