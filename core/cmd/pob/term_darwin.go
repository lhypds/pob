//go:build darwin

package main

import "syscall"

// The BSD names for the two termios ioctls; Linux calls them TCGETS/TCSETS.
const (
	ioctlGetTermios = syscall.TIOCGETA
	ioctlSetTermios = syscall.TIOCSETA
)
