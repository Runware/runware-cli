//go:build internal

package cmd

// settableConfigKeys returns the config keys offered by `config set` completion.
// Internal builds include base_url so the API endpoint can be overridden.
func settableConfigKeys() []string {
	return []string{
		"base_url",
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
// Internal builds allow every key, including base_url.
func isSettableKey(string) bool {
	return true
}
