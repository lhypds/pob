// Shared application state and cross-module entry points for the Pob
// Linux/X11 shell. Mirrors the macOS shell (macos/Sources): a translucent
// always-on-top overlay window whose perception/operation primitives are
// driven by the Go core (pob-core) over line-delimited JSON-RPC on
// stdin/stdout.
#ifndef POB_APP_H
#define POB_APP_H

#include <gtk/gtk.h>

// Colors copied from the macOS shell (SwiftUI system palette) so both
// platforms render identically. The one exception is the content area's
// background: macOS and Windows wash theirs with Color.gray.opacity(0.2),
// X11 leaves its own clear (see content_view.c's on_draw).
#define POB_COLOR_BLUE_R (0.0 / 255.0) // SwiftUI Color.blue = systemBlue
#define POB_COLOR_BLUE_G (122.0 / 255.0)
#define POB_COLOR_BLUE_B (255.0 / 255.0)

#define POB_ACCENT_CSS "#007AFF"
#define POB_RED_CSS "#FF3B30" // SwiftUI Color.red = systemRed

typedef struct AppState {
    GtkApplication *app;
    GtkWindow *window;
    GtkWidget *headerbar;
    GtkWidget *content; // overlay drawing area (the "content view")

    GtkWidget *record_btn;
    GtkWidget *play_btn;
    GtkWidget *target_btn;
    GtkWidget *crop_btn;
    GtkWidget *clickthrough_btn;
    GtkWidget *lock_btn;

    gboolean is_targeting;
    gboolean is_cropping;
    gboolean is_click_through;
    gboolean is_locked;
    gboolean is_recording;
    gboolean is_executing;
} AppState;

extern AppState g_state;

// main.c
void app_update_click_through(void);
void app_update_window_lock(void);
void app_set_executing(gboolean executing);  // called from core_bridge (main thread)
// Where this instance answers on the network, or NULL/"" while the server is
// off — what the instance badge hands out. Also from core_bridge.
void app_set_server_url(const char *url);
void app_set_targeting(gboolean targeting);  // also syncs toolbar + click-through
void app_set_cropping(gboolean cropping);
void app_show_max_step_dialog(void); // "Max step exceed." Continue/Stop
// Whatever the core has to say for itself, with an OK — the settings a macro's
// IF needs and hasn't got, say. Both strings come from the core.
void app_show_alert_dialog(const char *title, const char *message);
const char *app_version(void);

#endif
