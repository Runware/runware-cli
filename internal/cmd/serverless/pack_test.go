package serverless

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestPackPythonFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.py")
	content := []byte("def predict():\n    return 1\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	encoded, modelFile, err := packPythonFile(path)
	if err != nil {
		t.Fatalf("packPythonFile: %v", err)
	}
	if modelFile != "app.py" {
		t.Errorf("modelFile = %q, want app.py", modelFile)
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("expected 1 zip entry, got %d", len(zr.File))
	}
	if zr.File[0].Name != "app.py" {
		t.Errorf("zip entry name = %q, want app.py", zr.File[0].Name)
	}

	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close() //nolint:errcheck
	got := new(bytes.Buffer)
	if _, err := got.ReadFrom(rc); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), content) {
		t.Errorf("zip contents = %q, want %q", got.Bytes(), content)
	}
}

func TestPackPythonFile_Missing(t *testing.T) {
	_, _, err := packPythonFile(filepath.Join(t.TempDir(), "missing.py"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestPackPythonFile_Directory(t *testing.T) {
	_, _, err := packPythonFile(t.TempDir())
	if err == nil {
		t.Fatal("expected error for directory")
	}
}
