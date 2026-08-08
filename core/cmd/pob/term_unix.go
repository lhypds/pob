//go:build darwin || linux

// Raw terminal mode, for the arrow-key chooser in `pob launch`. Done with the
// standard library alone: the core module carries no dependencies, and this is
// the whole of what a chooser needs from a terminal.
package main

import (
	"os"
	"syscall"
	"unsafe"
)

// rawMode hands back keys as they are pressed: nothing echoed, no waiting for
// a line to be finished, and Ctrl-C arriving as a byte rather than a signal so
// the terminal can always be put back the way it was found. ok is false when
// there is no terminal to change — a pipe, a script, a CI job — which is the
// caller's cue to ask its question the plain way instead.
func rawMode(f *os.File) (restore func(), ok bool) {
	var saved syscall.Termios
	if err := termiosIoctl(f.Fd(), ioctlGetTermios, &saved); err != nil {
		return nil, false
	}

	raw := saved
	raw.Iflag &^= syscall.BRKINT | syscall.ICRNL | syscall.INPCK | syscall.ISTRIP | syscall.IXON
	// Output post-processing off too, so a line ending is exactly the "\r\n"
	// written rather than a carriage return the terminal adds one of its own to.
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.IEXTEN | syscall.ISIG
	// One byte is enough to return from a read, and never wait for a second.
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := termiosIoctl(f.Fd(), ioctlSetTermios, &raw); err != nil {
		return nil, false
	}
	return func() { _ = termiosIoctl(f.Fd(), ioctlSetTermios, &saved) }, true
}

func termiosIoctl(fd, request uintptr, t *syscall.Termios) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(unsafe.Pointer(t))); errno != 0 {
		return errno
	}
	return nil
}
