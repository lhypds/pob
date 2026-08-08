//go:build !darwin && !linux

package main

import "os"

// rawMode is implemented where the standard library alone can put a terminal
// into it. Everywhere else — Windows above all — the chooser falls back to
// typing a number, which needs nothing from the terminal at all.
func rawMode(*os.File) (func(), bool) { return nil, false }
