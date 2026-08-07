//go:build !darwin

package main

// holdWindowAspect reports that the platform won't constrain resizing for us, so
// the caller falls back to reshaping the window itself after each resize.
func holdWindowAspect(cols, rows float32) bool {
	return false
}
