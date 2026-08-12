// Carries the windows Pob frames along when the frame itself is dragged.
// The X11 half of the macOS CarryService (macos/Sources/Services): the overlay
// is a hole punched over somebody else's desktop, and while the window is
// locked everything under the frame keeps the arrangement it was set up in.
#ifndef POB_CARRY_SERVICE_H
#define POB_CARRY_SERVICE_H

#include <gtk/gtk.h>

// Turns carrying on or off — the lock drives this, so it follows
// app_update_window_lock. Switching it off mid-drag lets go of whatever the
// current drag had hold of.
void carry_service_set_enabled(gboolean enabled);
gboolean carry_service_is_enabled(void);

// Seeds the anchor from the window as it now stands. Called once the frame has
// been restored, so the first drag is measured from where the window actually
// starts rather than from wherever GTK first put it.
void carry_service_seed(void);

// The frame's geometry changed — every ConfigureNotify, so everything behind
// this is either cheap or latched for the duration of a drag. A configure that
// resizes rather than moves carries nothing.
void carry_service_window_configured(void);

#endif
