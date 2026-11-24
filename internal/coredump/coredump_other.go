//go:build !linux && !darwin

package coredump

// Disable does nothing: this platform has no core size limit to set. On
// Windows a crash dump is still possible with GOTRACEBACK=wer and a LocalDumps
// policy (see the package doc), so nothing here protects key material.
func Disable() error { return nil }

// Disabled reports false: nothing was set on this platform.
func Disabled() (bool, error) { return false, nil }
