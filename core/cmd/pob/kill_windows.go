//go:build windows

// Asking after and ending processes on Windows — for `pob kill`. The process
// table comes from a toolhelp snapshot, which the standard library exposes in
// full, so this needs no dependencies and no ps of its own.
package main

import (
	"os"
	"syscall"
	"unsafe"
)

// processInfo reports a process's parent and the executable it is running.
// ok is false when the snapshot has no such process.
func processInfo(pid int) (parent int, name string, ok bool) {
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, "", false
	}
	defer syscall.CloseHandle(snapshot)

	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := syscall.Process32First(snapshot, &entry); err == nil; err = syscall.Process32Next(snapshot, &entry) {
		if int(entry.ProcessID) == pid {
			return int(entry.ParentProcessID), syscall.UTF16ToString(entry.ExeFile[:]), true
		}
	}
	return 0, "", false
}

// signalStop ends a process. Windows has no SIGTERM to ask a GUI app with
// from outside its window, so this is the same abrupt end as forceStop —
// which costs nothing here: core writes its end time when the shell's pipe
// closes, however the shell came to close it.
func signalStop(pid int) error { return forceStop(pid) }

// forceStop ends a process outright.
func forceStop(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
