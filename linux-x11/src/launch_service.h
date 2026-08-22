// Opens an application and puts its window in the frame — a macro's launch().
// See launch_service.c.
#ifndef POB_LAUNCH_SERVICE_H
#define POB_LAUNCH_SERVICE_H

#include <glib.h>

// Runs `target` and fits the window that comes of it to the content area,
// answering the core on `id` — with the launch's result when there was
// something to open, with an error when there was not. `gap` is how much of the
// content area to leave around the window on every side, in device pixels.
//
// Returns at once. The waiting is done by a timeout on the main loop, so the
// overlay goes on drawing and the toolbar goes on working while an application
// starts up.
void launch_service_handle(const char *id, const char *target, int gap);

#endif
