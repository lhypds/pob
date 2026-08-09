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
// While the view is hidden for capture the flash is held back and played when
// it comes into view again — a shutter nobody can see is no feedback at all.
void content_view_flash(void);

// Draws nothing at all — no tint, no cursor, no labels — while the window is
// off the screen for a capture, so nothing of Pob can reach the shot even
// where its opacity is not honored. Held on for as long as captures keep
// coming; see screenshot_service.c.
void content_view_set_capture_hidden(gboolean hidden);

// Shows a transient message (bottom center, black pill, white text) that
// disappears after ~2 s — action feedback like "macro.psl reset".
void content_view_show_message(const char *text);

// Applies the crosshair pointer while cropping; call after mode changes.
void content_view_update_cursor_style(void);

#endif
