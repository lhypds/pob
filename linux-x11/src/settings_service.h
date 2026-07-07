// UI-side view of the shared project files, mirroring the macOS
// SettingsService. The Go core owns settings.json defaults, instruction.txt,
// macro.txt and the logs tree; this service only resolves the project root,
// opens files in the user's editor, persists the window frame and clears
// user files on request.
#ifndef POB_SETTINGS_SERVICE_H
#define POB_SETTINGS_SERVICE_H

#include <glib.h>

// Absolute project root path (cached after first call, never freed).
const char *settings_project_root(void);

// logs/<instance> directory id reserved for this process (cached after first
// call, never freed). Holds this instance's settings.json, seeded from the
// root settings.json; passed to pob-core via --instance.
const char *settings_instance_id(void);

// Saved window frame from settings.json (window_x/y/width/height).
// Returns FALSE when any key is missing.
gboolean settings_get_window_frame(int *x, int *y, int *w, int *h);
void settings_save_window_frame(int x, int y, int w, int h);

void settings_open_settings_file(void);
void settings_open_instruction_file(void);
void settings_open_macro_file(void);
void settings_open_app_log(void);
void settings_open_logs_folder(void);

// Contents of macro.txt ("" when missing); caller frees.
gchar *settings_get_macro(void);

void settings_clear_macro(void);
void settings_clear_instruction(void);
void settings_clear_logs(void);

#endif
