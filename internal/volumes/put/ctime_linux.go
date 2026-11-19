package put

import (
	"io/fs"
	"syscall"
)

// changeTime returns the file's inode change time in Unix nanoseconds.
func changeTime(_ string, info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Ctim.Nano()
	}
	return 0
}
