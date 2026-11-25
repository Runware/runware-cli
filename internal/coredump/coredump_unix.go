//go:build linux || darwin

package coredump

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// Disable sets the soft and hard core size limits to zero, so neither this
// process nor a child it starts can raise them again, and on Linux also marks
// the process not dumpable.
func Disable() error {
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{
		Cur: 0,
		Max: 0,
	}); err != nil {
		return fmt.Errorf("disable core dumps: %w", err)
	}
	return disableDumpable()
}

// Disabled reports whether Disable is in effect for this process.
func Disabled() (bool, error) {
	var lim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CORE, &lim); err != nil {
		return false, err
	}
	if lim.Cur != 0 || lim.Max != 0 {
		return false, nil
	}
	return dumpableOff()
}
