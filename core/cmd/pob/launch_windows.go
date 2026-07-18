//go:build windows

package main

import "syscall"

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

// detachSysProcAttr detaches a launched app from this CLI's console so it
// outlives the CLI and a Ctrl-C in the terminal never reaches it.
var detachSysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
