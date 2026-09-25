//go:build !linux && !darwin && !windows

package inventory

import "os/exec"

func lowPriorityPre(*exec.Cmd)  {}
func lowPriorityPost(*exec.Cmd) {}
