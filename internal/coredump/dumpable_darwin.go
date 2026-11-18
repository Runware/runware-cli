package coredump

// macOS has no dumpable flag; the zero core size limit is what stops a dump.
func disableDumpable() error { return nil }

func dumpableOff() (bool, error) { return true, nil }
