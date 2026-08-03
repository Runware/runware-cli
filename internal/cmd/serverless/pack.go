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

// maxPackEntryBytes is the maximum size of a single entry file we will zip and
// base64-encode for createDeployment. Keeps accidental large inputs from
// blowing memory (base64 expands the payload by ~4/3).
const maxPackEntryBytes int64 = 10 << 20 // 10 MiB

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
	if info.Size() > maxPackEntryBytes {
		return "", "", fmt.Errorf(
			"entry file %q is %d bytes; maximum supported size is %d bytes (%d MiB)",
			path,
			info.Size(),
			maxPackEntryBytes,
			maxPackEntryBytes>>20,
		)
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
	// Cap the copy in case the file grows after Stat.
	written, err := io.Copy(w, io.LimitReader(f, maxPackEntryBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("write zip entry: %w", err)
	}
	if written > maxPackEntryBytes {
		return "", "", fmt.Errorf(
			"entry file %q exceeds maximum supported size of %d bytes (%d MiB)",
			path,
			maxPackEntryBytes,
			maxPackEntryBytes>>20,
		)
	}
	if err := zw.Close(); err != nil {
		return "", "", fmt.Errorf("close zip: %w", err)
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), modelFile, nil
}
