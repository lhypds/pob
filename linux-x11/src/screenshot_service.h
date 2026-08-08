// Captures the desktop area behind the Pob window's content view, mirroring
// the macOS ScreenshotService. macOS excludes the overlay window via
// CGWindowListCreateImage(.optionOnScreenBelowWindow); X11 has no direct
// equivalent, so the window is made fully transparent for one compositor
// frame while XGetImage grabs the root window.
//
// All published coordinates are screenshot pixels = X11 device pixels
// (top-left origin), so ShotContext also records where the content area sat
// on the root window at capture time — mouse actions use it to convert the
// virtual cursor position back to root coordinates.
#ifndef POB_SCREENSHOT_SERVICE_H
#define POB_SCREENSHOT_SERVICE_H

#include <glib.h>

typedef struct {
    gboolean valid;
    int origin_x; // content-area origin on the root window, device pixels
    int origin_y;
    int width; // content-area size in device pixels — the box the virtual
    int height; // cursor lives in, and the size of the image a capture makes
    int scale; // GDK scale factor at capture time
} ShotContext;

// Thread-safe snapshot of the most recent capture context.
ShotContext screenshot_get_context(void);

// Handles a "screenshot.capture" request. Main thread only; responds
// asynchronously through the frame channel, or core_bridge if that is not up,
// once the capture completes.
//
// format is "png" or "jpeg", max_width shrinks the picture to at most that
// many pixels across (0 leaves it alone) and quality is JPEG quality (0 takes
// the default). The defaults are the agent's — a full-size PNG — and the rest
// is for the view page, which is watching the machine rather than reading it.
void screenshot_handle_capture(const char *id, gboolean with_cursor,
                               gboolean has_crop, double crop_x, double crop_y,
                               double crop_w, double crop_h, const char *format,
                               int max_width, int quality);

#endif
