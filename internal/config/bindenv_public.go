//go:build !internal

package config

// bindInternalEnv is a no-op in public builds: the RUNWARE_BASE_URL override is
// not available, so the env var is intentionally left unbound. The binding lives
// in bindenv_internal.go (build tag: internal).
func bindInternalEnv() {}
