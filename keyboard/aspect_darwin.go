//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>

// holdAspect asks the window server to keep this app's windows to the given
// content proportions. Doing it here rather than in Go is the whole point: the
// constraint then applies while the resize drag is happening, so the window
// scales under the pointer. Correcting the size afterwards can only ever follow
// the drag around a frame late.
static void holdAspect(double w, double h) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSSize aspect = NSMakeSize(w, h);
		for (NSWindow *win in [NSApp windows]) {
			[win setContentAspectRatio:aspect];
		}
	});
}
*/
import "C"

// holdWindowAspect asks the platform to constrain resizing to these proportions,
// reporting whether it can. On macOS it can, so the fallback that corrects the
// size after the fact isn't needed.
func holdWindowAspect(cols, rows float32) bool {
	C.holdAspect(C.double(cols), C.double(rows))
	return true
}
