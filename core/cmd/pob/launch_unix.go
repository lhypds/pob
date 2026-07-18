//go:build !windows

package main

import "syscall"

// detachSysProcAttr puts a launched app in its own session so it outlives
// this CLI and a Ctrl-C in the terminal never reaches it.
var detachSysProcAttr = &syscall.SysProcAttr{Setsid: true}
