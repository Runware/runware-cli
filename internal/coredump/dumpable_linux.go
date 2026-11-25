package coredump

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func disableDumpable() error {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return fmt.Errorf("mark the process not dumpable: %w", err)
	}
	return nil
}

func dumpableOff() (bool, error) {
	v, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
	if err != nil {
		return false, err
	}
	return v == 0, nil
}
