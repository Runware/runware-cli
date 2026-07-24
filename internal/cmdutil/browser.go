package cmdutil

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser opens url in the user's default web browser. It returns an error
// if the platform launcher cannot be started (e.g. in a headless environment).
func OpenBrowser(ctx context.Context, url string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
		args = []string{url}
	case "windows":
		name = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		name = "xdg-open"
		args = []string{url}
	}
	// The launcher name is a fixed per-OS constant and the URL is passed as a
	// separate argv element (not shell-interpreted), so there is no injection.
	if err := exec.CommandContext(ctx, name, args...).Start(); err != nil { //nolint:gosec
		return fmt.Errorf("failed to open browser: %w", err)
	}
	return nil
}
