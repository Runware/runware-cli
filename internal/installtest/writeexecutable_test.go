package installtest

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// forkWait is how long a fork holds ForkLock while the test checks that
// writeExecutable waits for it. Without the lock the write finishes in well
// under a millisecond, so a write that does not wait shows up long before
// this.
const forkWait = 100 * time.Millisecond

// TestWriteExecutable_WaitsForAForkInProgress holds ForkLock for writing, as
// os/exec does while it forks, and checks that writeExecutable neither
// returns nor creates the file until the fork is done. Ordinary runs of the
// stub, tripwire and shim tests pass with or without the lock, so this is
// what keeps "text file busy" from coming back. It does not run in parallel:
// while it holds ForkLock no other test can start a process.
func TestWriteExecutable_WaitsForAForkInProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runware")
	const script = "#!/bin/sh\nexit 0\n"

	syscall.ForkLock.Lock()
	endFork := sync.OnceFunc(syscall.ForkLock.Unlock)
	// A failed check must not leave every later fork in the binary blocked.
	defer endFork()

	done := make(chan error, 1)
	go func() {
		done <- writeExecutable(path, script)
	}()

	select {
	case err := <-done:
		t.Fatalf("writeExecutable returned %v while a fork held ForkLock, want it to wait for the fork", err)
	case <-time.After(forkWait):
	}
	// An open descriptor is what a fork would inherit, so the file must not
	// exist yet either. Carry on after a failure so the write still finishes
	// before the test removes its directory.
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat %s while a fork held ForkLock: err = %v, want the file not to exist yet", path, err)
	}

	endFork()

	if err := <-done; err != nil {
		t.Fatalf("writeExecutable after the fork: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != script {
		t.Errorf("content = %q, want %q", got, script)
	}
}
