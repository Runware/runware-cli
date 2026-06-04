package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Download fetches url and writes the response body to destPath atomically:
// it streams into a temporary file and renames it to destPath on success,
// so a failed download never leaves a partial file behind.
func Download(ctx context.Context, url, destPath string, timeout time.Duration) error {
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck,gosec
		resp.Body.Close()              //nolint:errcheck,gosec
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	tmp, err := os.CreateTemp(destDir, "runware-download-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		tmp.Close()        //nolint:errcheck,gosec
		os.Remove(tmpPath) //nolint:errcheck,gosec
	}()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to flush temp file: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("failed to move file to destination: %w", err)
	}

	return nil
}
