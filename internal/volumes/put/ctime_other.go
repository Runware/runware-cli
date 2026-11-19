//go:build !linux && !darwin && !windows

package put

import "io/fs"

// changeTime returns zero: the platform reports no change time, so the hash cache rehashes every file.
func changeTime(string, fs.FileInfo) int64 { return 0 }
