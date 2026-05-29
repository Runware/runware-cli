//go:build internal

package config

import "github.com/spf13/viper"

// bindInternalEnv binds the RUNWARE_BASE_URL override env var. Internal builds
// only (build tag: internal); the public variant in bindenv_public.go is a no-op.
func bindInternalEnv() {
	viper.BindEnv("base_url", "RUNWARE_BASE_URL") //nolint:errcheck,gosec
}
