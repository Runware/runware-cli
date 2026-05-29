//go:build !internal

package cmd

// settableConfigKeys returns the config keys offered by `config set` completion.
// Public builds omit base_url — the API endpoint is locked to production.
func settableConfigKeys() []string {
	return []string{
		"defaults.model",
		"defaults.width",
		"defaults.height",
		"defaults.steps",
		"defaults.cfg_scale",
		"defaults.scheduler",
		"defaults.output_dir",
		"defaults.output_format",
		"defaults.format",
	}
}

// isSettableKey reports whether key may be set via `config set` in this build.
// Public builds reject base_url so it cannot be written to the config file.
func isSettableKey(key string) bool {
	return key != "base_url"
}
