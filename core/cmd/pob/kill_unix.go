//go:build !windows

// Asking after and ending processes, the Unix way — for `pob kill`.
package main

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// processInfo reports a process's parent and the executable it is running.
// ok is false when there is no such process. ps does the asking: the syscalls
// behind it differ between the Unixes, and the core module carries no
// dependencies to paper over that with.
func processInfo(pid int) (parent int, name string, ok bool) {
	// = suppresses the column headers, leaving one line: "<ppid> <command>".
	out, err := exec.Command("ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, "", false
	}
	// Cut rather than Fields: a command is a path on macOS and may have
	// spaces in it, and only the first column is a number.
	ppidText, command, found := strings.Cut(strings.TrimSpace(string(out)), " ")
	if !found {
		return 0, "", false
	}
	ppid, err := strconv.Atoi(ppidText)
	if err != nil {
		return 0, "", false
	}
	return ppid, strings.TrimSpace(command), true
}

// signalStop asks a process to exit, the way a window's close button does.
func signalStop(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

// forceStop ends a process that would not go on being asked.
func forceStop(pid int) error { return syscall.Kill(pid, syscall.SIGKILL) }
