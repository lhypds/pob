// Virtual cursor state + XTest-based mouse/keyboard synthesis, mirroring the
// macOS MouseService. The virtual cursor lives in screenshot pixel
// coordinates (top-left origin) and never touches the real pointer except
// for the brief instant an action is performed (X11 requires the pointer to
// be at the click position; it is restored immediately afterwards).
//
// Blocking actions run on a dedicated worker thread with its own X Display
// connection, so the GTK main loop stays responsive; the worker answers the
// pending JSON-RPC request through core_bridge's thread-safe responders.
#ifndef POB_MOUSE_SERVICE_H
#define POB_MOUSE_SERVICE_H

#include <glib.h>

typedef enum {
    MOUSE_JOB_CLICK,
    MOUSE_JOB_RIGHT_CLICK,
    MOUSE_JOB_DOUBLE_CLICK,
    MOUSE_JOB_DRAG,
    MOUSE_JOB_SCROLL,
    MOUSE_JOB_TYPE,
    MOUSE_JOB_KEY_PRESS,
} MouseJobType;

void mouse_service_init(void);
void mouse_service_shutdown(void);

// Virtual cursor (screenshot pixels, top-left origin). Thread-safe.
void mouse_get_virtual_pos(double *x, double *y);
void mouse_reset_cursor(void);              // -> (20, 20)
void mouse_move_by(double dx, double dy);

// Queue a blocking action; `id` is the JSON-RPC request id to answer,
// `text` is the payload for TYPE ("text") and KEY_PRESS ("key").
// Both strings are copied.
void mouse_enqueue_job(MouseJobType type, const char *id, double dx, double dy, const char *text);

#endif
