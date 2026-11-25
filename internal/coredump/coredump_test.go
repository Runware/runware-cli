//go:build linux || darwin

package coredump

import "testing"

func TestDisableStopsCoreDumps(t *testing.T) {
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
	off, err := Disabled()
	if err != nil {
		t.Fatal(err)
	}
	if !off {
		t.Fatal("core dumps are still allowed after Disable")
	}
}
