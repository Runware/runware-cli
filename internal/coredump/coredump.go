// Package coredump stops the process from writing a core dump on Linux and
// macOS, so key material in its memory never reaches a core file there.
//
// On Windows, Disable does nothing. The Go runtime hands a crash to Windows
// Error Reporting only when GOTRACEBACK=wer is set; with that setting, a
// LocalDumps policy on the machine can write a full memory dump, key material
// included.
package coredump
