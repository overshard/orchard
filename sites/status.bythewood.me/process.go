package main

import (
	"os/exec"
	"syscall"
)

// setProcessGroup gives the child its own group so a timeout can reach the
// browser lighthouse starts: exec.CommandContext only signals its direct child.
// Linux only.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup signals the whole group; kill(2) reads a negative pid as
// "every process in this group". Safe after a clean exit, where it gets ESRCH.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
