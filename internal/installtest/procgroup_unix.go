//go:build unix

package installtest

import (
	"os/exec"
	"syscall"
)

// killGroupOnCancel starts cmd in its own process group and makes context
// cancellation kill the whole group. Killing only sh would leave a hung child
// such as curl running, holding the output pipe open.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
