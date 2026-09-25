//go:build windows

package inventory

import (
	"os/exec"
	"syscall"
)

const (
	belowNormalPriorityClass = 0x00004000
	createNoWindow           = 0x08000000
)

// lowPriorityPre arranca el hijo con prioridad BELOW_NORMAL y sin ventana.
func lowPriorityPre(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: belowNormalPriorityClass | createNoWindow}
}

func lowPriorityPost(*exec.Cmd) {}
