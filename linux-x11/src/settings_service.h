// UI-side view of the shared project files, mirroring the macOS
// SettingsService. The Go core owns settings.json defaults, the src/ macros
// and the logs tree; this service only resolves the project root,
// opens files in the user's editor, persists the window frame and clears
// user files on request.
//
// settings.json is the machine's, at ~/.pob; src/ and
// logs/ belong to the instance, under ~/.pob/<instance>/.
#ifndef POB_SETTINGS_SERVICE_H
#define POB_SETTINGS_SERVICE_H

#include <glib.h>

// Absolute project root path (cached after first call, never freed).
const char *settings_project_root(void);

// The machine's ~/.pob/<instance> directory id — the same one on every run
// (cached after first call, never freed). That directory holds this instance's
// src/ and logs/; the id is passed to pob-core via
// --instance.
const char *settings_instance_id(void);

// Claims this machine's instance for this process; FALSE means another Pob
// already holds it. Only one Pob drives a desktop, so this is called before
// the window is built. Claiming is also the check — see the implementation.
gboolean settings_claim_instance(void);

// Saved window frame from instance.json (window_x/y/width/height), falling
// back to settings.json where it used to be kept.
// Returns FALSE when any key is missing.
gboolean settings_get_window_frame(int *x, int *y, int *w, int *h);
void settings_save_window_frame(int x, int y, int w, int h);

// Whether the window was left locked (is_locked in instance.json). It belongs
// beside the frame: the lock is what holds the frame to its size and to what it
// frames, so a run that restored the frame but not the lock would come back
// loose. FALSE for an
// instance that has never recorded one.
gboolean settings_get_window_locked(void);
void settings_save_window_locked(gboolean locked);

// Whether the window was left passing clicks through (is_click_through in
// instance.json). It belongs beside the lock for the same reason: an instance
// set up to sit over the app it drives comes back sitting over it, instead of
// swallowing the first clicks meant for what is underneath until the button is
// pressed again. TRUE for an instance that has never recorded one — the
// overlay's resting state.
gboolean settings_get_click_through(void);
void settings_save_click_through(gboolean on);

void settings_open_settings_file(void);
void settings_open_macro_file(void);
void settings_open_instance_log(void);
void settings_open_logs_folder(void);

// Contents of src/main.macro.psl ("" when missing); caller frees.
gchar *settings_get_macro(void);

// Appends one action line to src/main.macro.psl.
void settings_append_macro(const char *line);

void settings_clear_macro(void);

#endif
