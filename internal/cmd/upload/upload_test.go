package upload

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimalPNG is a tiny valid PNG (1×1) recognised by http.DetectContentType.
var minimalPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

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
// data URI with the MIME type derived from its content.
func TestBuildImageInput_LocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(path, minimalPNG, 0600); err != nil {
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
	if !bytes.Equal(decoded, minimalPNG) {
		t.Errorf("decoded content mismatch")
	}
}

// TestBuildImageInput_ContentOverridesExtension: magic-byte detection wins over
// a misleading file extension.
func TestBuildImageInput_ContentOverridesExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(path, minimalPNG, 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	got, err := buildImageInput(path)
	if err != nil {
		t.Fatalf("buildImageInput error: %v", err)
	}
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("got %q, want image/png MIME from content", got)
	}
}

// TestBuildImageInput_UnsupportedContentType: non-image content is rejected.
func TestBuildImageInput_UnsupportedContentType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	if _, err := buildImageInput(path); err == nil {
		t.Fatal("expected error for unsupported content type, got nil")
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
