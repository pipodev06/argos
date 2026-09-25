//go:build linux || darwin

package inventory

import (
	"os/exec"
	"syscall"
)

func lowPriorityPre(*exec.Cmd) {}

// lowPriorityPost baja la prioridad del hijo (nice 10). Es best-effort: si falla, el
// hijo corre con la prioridad normal.
func lowPriorityPost(cmd *exec.Cmd) {
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, cmd.Process.Pid, 10)
}
