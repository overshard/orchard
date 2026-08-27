package main

import (
	"os/exec"
	"syscall"
)

// Killing a subprocess tree.
//
// exec.CommandContext kills only the process it started. Lighthouse starts
// Chromium, Chromium starts renderer processes, and none of them are the
// direct child, so a timeout that killed just the CLI would leave a browser
// running with nobody holding a reference to it. One leaked Chromium per
// wedged daily audit is a machine eaten inside a week, on hardware that is
// also Isaac's desktop.
//
// The fix is to put the child in its own process group and signal the group.
// The Rust version got this for free from tokio's kill_on_drop; Go has no
// equivalent, so it is spelled out.
//
// Linux only, which every environment this runs in is: the webdev container,
// the Alpine image, and the desktop's Docker.

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup signals the whole group. Safe to call after a clean exit:
// the group is then empty and Kill returns ESRCH, which is ignored.
//
// The negative pid is the interface, not a trick: kill(2) reads a negative
// argument as "every process in the group with this id". Passing the plain pid
// would kill exactly the process that has already exited and leave the browser
// running, which is the bug this file exists to prevent.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
