//go:build !internal

package cmd

import "github.com/runware/runware-cli/internal/config"

func sanitizeConfigForDisplay(display *config.Config) {
	// Public builds hard-lock the runtime endpoint and ignore base_url overrides.
	// Avoid showing stored base_url values in JSON/YAML config output.
	display.BaseURL = ""
}
