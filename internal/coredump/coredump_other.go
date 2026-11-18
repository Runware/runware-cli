//go:build !linux && !darwin

package coredump

// Disable does nothing where the platform has no core size limit to set.
func Disable() error { return nil }

// Disabled reports false: nothing was set on this platform.
func Disabled() (bool, error) { return false, nil }
