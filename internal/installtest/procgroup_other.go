//go:build !unix

package installtest

import "os/exec"

// killGroupOnCancel keeps the default cancellation. RunSh skips on these
// platforms before it starts a process.
func killGroupOnCancel(*exec.Cmd) {}
