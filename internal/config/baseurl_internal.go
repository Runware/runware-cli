//go:build internal

package config

import "github.com/spf13/viper"

// GetBaseURL returns the API base URL, honoring the RUNWARE_BASE_URL env var
// and the base_url config key before falling back to the built-in default.
// Available only in internal builds (build tag: internal); the public variant
// in baseurl_public.go ignores all overrides.
func GetBaseURL() string {
	if url := viper.GetString("base_url"); url != "" {
		return url
	}
	return DefaultBaseURL
}
