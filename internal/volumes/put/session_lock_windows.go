package put

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func lockSession(dir string, wait bool) (*os.File, error) {
	path := filepath.Join(dir, sessionLockName)
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK)
	if !wait {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	err = windows.LockFileEx(handle, flags, 0, 1, 0, &windows.Overlapped{})
	if err == nil {
		return os.NewFile(uintptr(handle), path), nil
	}
	_ = windows.CloseHandle(handle)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return nil, nil
	}
	return nil, err
}
