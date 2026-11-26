//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package put

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// lockSession holds the local session until the returned file is closed.
// A nil file and nil error mean another process holds a nonblocking lock.
func lockSession(dir string, wait bool) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, sessionLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	flags := unix.LOCK_EX
	if !wait {
		flags |= unix.LOCK_NB
	}
	for {
		err = unix.Flock(int(f.Fd()), flags)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err == nil {
		return f, nil
	}
	_ = f.Close()
	if errors.Is(err, unix.EWOULDBLOCK) {
		return nil, nil
	}
	return nil, err
}
