package put

import (
	"io/fs"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileBasicInfo is the Windows FILE_BASIC_INFO structure.
type fileBasicInfo struct {
	CreationTime   int64
	LastAccessTime int64
	LastWriteTime  int64
	ChangeTime     int64
	FileAttributes uint32
	_              uint32
}

// changeTime returns the file's NTFS change time in Unix nanoseconds, or zero when it cannot be read.
func changeTime(path string, _ fs.FileInfo) int64 {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	h, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return 0
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var info fileBasicInfo
	if err := windows.GetFileInformationByHandleEx(h, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil || info.ChangeTime == 0 { //nolint:gosec // the API fills a fixed-size struct
		return 0
	}
	ft := windows.Filetime{
		LowDateTime:  uint32(info.ChangeTime),
		HighDateTime: uint32(info.ChangeTime >> 32),
	}
	return ft.Nanoseconds()
}
