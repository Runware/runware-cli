package serverless

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// packPythonFile zips a single Python entry file and returns the base64-encoded
// archive plus the modelFile path expected inside the zip (the file's basename).
func packPythonFile(path string) (zipBase64, modelFile string, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("read entry file: %w", err)
	}
	if info.IsDir() {
		return "", "", fmt.Errorf("entry file %q is a directory", path)
	}

	modelFile = filepath.Base(path)
	if modelFile == "." || modelFile == string(filepath.Separator) {
		return "", "", fmt.Errorf("invalid entry file path %q", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return "", "", fmt.Errorf("open entry file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(modelFile)
	if err != nil {
		return "", "", fmt.Errorf("create zip entry: %w", err)
	}
	if _, err := io.Copy(w, f); err != nil {
		return "", "", fmt.Errorf("write zip entry: %w", err)
	}
	if err := zw.Close(); err != nil {
		return "", "", fmt.Errorf("close zip: %w", err)
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), modelFile, nil
}
