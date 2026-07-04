// The overlay content view, mirroring the macOS ContentView body: a
// translucent gray area with targeting mode (click to copy coordinates),
// crop mode (drag to copy a region), the animated virtual-cursor overlay
// shown while the agent executes, and the white screenshot flash.
#ifndef POB_CONTENT_VIEW_H
#define POB_CONTENT_VIEW_H

#include <gtk/gtk.h>

GtkWidget *content_view_new(void);

// New virtual-cursor display target in screenshot (device) pixels; the
// overlay animates toward it with a 0.1 s ease-out, like the macOS view.
void content_view_cursor_target_changed(double x, double y);

// Snaps the animated cursor back to (20, 20) — called when execution starts.
void content_view_reset_anim(void);

// Triggers the white screenshot flash (opacity 0.5 fading out over 0.4 s).
void content_view_flash(void);

// Shows a transient message (top center, black pill, white text) that
// disappears after ~2 s — action feedback like "Logs cleared".
void content_view_show_message(const char *text);

// Applies the crosshair pointer while cropping; call after mode changes.
void content_view_update_cursor_style(void);

#endif
