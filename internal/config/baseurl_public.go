//go:build !internal

package config

// GetBaseURL always returns the locked production endpoint in public builds.
// The base_url config key and RUNWARE_BASE_URL env var are intentionally ignored
// so customer binaries cannot be redirected to staging or internal environments.
// The override variant lives in baseurl_internal.go (build tag: internal).
func GetBaseURL() string {
	return DefaultBaseURL
}
