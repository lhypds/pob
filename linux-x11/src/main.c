// Pob Linux/X11 shell. A feature-for-feature port of the macOS shell
// (macos/Sources): translucent always-on-top overlay window, header-bar
// toolbar, targeting/crop/click-through/lock modes, and the CoreBridge that
// drives the shared Go core. Window controls follow the desktop
// environment's native (Unix) button layout; everything else mirrors macOS.
#include "app.h"
#include "app_logger.h"
#include "content_view.h"
#include "core_bridge.h"
#include "mouse_service.h"
#include "settings_service.h"

#include <X11/Xlib.h>
#include <gdk/gdkx.h>
#include <string.h>

AppState g_state;

static guint save_frame_timeout = 0;

// ── version ─────────────────────────────────────────────────────────────────

const char *app_version(void) {
    static gchar *version = NULL;
    if (version) return version;

    // Project root first (dev workflow), then relative to the executable
    // (linux-x11/bin/pob -> ../../VERSION).
    gchar *candidates[2] = {NULL, NULL};
    candidates[0] = g_build_filename(settings_project_root(), "VERSION", NULL);
    gchar *self = g_file_read_link("/proc/self/exe", NULL);
    if (self) {
        gchar *dir = g_path_get_dirname(self);
        candidates[1] = g_build_filename(dir, "..", "..", "VERSION", NULL);
        g_free(dir);
        g_free(self);
    }

    for (int i = 0; i < 2 && !version; i++) {
        gchar *contents = NULL;
        if (candidates[i] &&
            g_file_get_contents(candidates[i], &contents, NULL, NULL)) {
            version = g_strstrip(contents); // keeps ownership of `contents`
            if (*version == '\0') {
                g_free(contents);
                version = NULL;
            }
        }
    }
    g_free(candidates[0]);
    g_free(candidates[1]);
    if (!version) version = g_strdup("0.0.0");
    return version;
}

// ── toolbar helpers ─────────────────────────────────────────────────────────

// Toolbar icon size in logical pixels. The GTK default (16) reads larger
// than the macOS compact toolbar, so render slightly smaller.
#define POB_ICON_SIZE 12

static const char *pick_icon(const char *const names[]) {
    GtkIconTheme *theme = gtk_icon_theme_get_default();
    for (int i = 0; names[i]; i++)
        if (gtk_icon_theme_has_icon(theme, names[i])) return names[i];
    return names[0];
}

static void set_button_icon(GtkWidget *btn, const char *const icon_names[]) {
    GtkWidget *img = gtk_image_new_from_icon_name(pick_icon(icon_names),
                                                  GTK_ICON_SIZE_BUTTON);
    gtk_image_set_pixel_size(GTK_IMAGE(img), POB_ICON_SIZE);
    gtk_button_set_image(GTK_BUTTON(btn), img);
    gtk_widget_show(img);
}

static GtkWidget *icon_button(const char *const icon_names[], const char *tooltip) {
    GtkWidget *btn = gtk_button_new();
    set_button_icon(btn, icon_names);
    gtk_widget_set_tooltip_text(btn, tooltip);
    return btn;
}

static void set_active_class(GtkWidget *btn, const char *cls, gboolean active) {
    GtkStyleContext *ctx = gtk_widget_get_style_context(btn);
    if (active)
        gtk_style_context_add_class(ctx, cls);
    else
        gtk_style_context_remove_class(ctx, cls);
}

// Click-through icon: plain hand when ON, slashed hand when OFF — the same
// pair as macOS (hand.raised / hand.raised.slash), instead of tinting the
// button. Icon themes ship no slashed variant, so the "disabled" diagonal
// is drawn over the symbolic icon.
static void set_clickthrough_icon(void);

static const char *const ICONS_SETTINGS[] = {"emblem-system-symbolic", "preferences-system-symbolic", NULL};
static const char *const ICONS_LOGS[] = {"text-x-generic-symbolic", "document-open-symbolic", NULL};
static const char *const ICONS_INSTRUCTION[] = {"format-justify-left-symbolic", "format-justify-fill-symbolic", NULL};
static const char *const ICONS_MACRO[] = {"system-run-symbolic", "application-x-executable-symbolic", NULL};
static const char *const ICONS_RECORD[] = {"media-record-symbolic", NULL};
static const char *const ICONS_PLAY[] = {"media-playback-start-symbolic", NULL};
static const char *const ICONS_STOP[] = {"media-playback-stop-symbolic", NULL};
static const char *const ICONS_TARGET[] = {"find-location-symbolic", "edit-find-symbolic", NULL};
static const char *const ICONS_CROP[] = {"image-crop-symbolic", "edit-select-all-symbolic", NULL};
static const char *const ICONS_HAND[] = {"hand-open-symbolic", "touch-symbolic", "input-mouse-symbolic", "input-touchpad-symbolic", NULL};
static const char *const ICONS_LOCKED[] = {"changes-prevent-symbolic", NULL};
static const char *const ICONS_UNLOCKED[] = {"changes-allow-symbolic", NULL};
static const char *const ICONS_TRASH[] = {"user-trash-symbolic", NULL};

static void set_clickthrough_icon(void) {
    GtkWidget *btn = g_state.clickthrough_btn;
    int scale = gtk_widget_get_scale_factor(btn);
    GtkIconTheme *theme = gtk_icon_theme_get_default();
    GtkIconInfo *info = gtk_icon_theme_lookup_icon_for_scale(
        theme, pick_icon(ICONS_HAND), POB_ICON_SIZE, scale,
        GTK_ICON_LOOKUP_USE_BUILTIN);

    GdkPixbuf *pix = NULL;
    if (info) {
        pix = gtk_icon_info_load_symbolic_for_context(
            info, gtk_widget_get_style_context(btn), NULL, NULL);
        g_object_unref(info);
    }
    if (!pix) { // fallback: plain themed icon without the slash overlay
        set_button_icon(btn, ICONS_HAND);
        return;
    }

    cairo_surface_t *surf = gdk_cairo_surface_create_from_pixbuf(
        pix, scale, gtk_widget_get_window(btn));
    g_object_unref(pix);

    if (!g_state.is_click_through) {
        // OFF: draw the diagonal "disabled" slash in the theme text color.
        GdkRGBA color;
        gtk_style_context_get_color(gtk_widget_get_style_context(btn),
                                    gtk_widget_get_state_flags(btn), &color);
        cairo_t *cr = cairo_create(surf);
        gdk_cairo_set_source_rgba(cr, &color);
        cairo_set_line_width(cr, 1.4);
        cairo_set_line_cap(cr, CAIRO_LINE_CAP_ROUND);
        cairo_move_to(cr, 2.0, 2.0);
        cairo_line_to(cr, POB_ICON_SIZE - 2.0, POB_ICON_SIZE - 2.0);
        cairo_stroke(cr);
        cairo_destroy(cr);
    }

    GtkWidget *img = gtk_image_new_from_surface(surf);
    cairo_surface_destroy(surf);
    gtk_button_set_image(GTK_BUTTON(btn), img);
    gtk_widget_show(img);
}

// ── mode / state transitions ────────────────────────────────────────────────

void app_update_click_through(void) {
    GtkWidget *win = GTK_WIDGET(g_state.window);
    if (!gtk_widget_get_realized(win)) return;

    // Pass clicks through the content area (toolbar stays interactive) when
    // the user enabled click-through, or while executing — on X11 the
    // synthesized XTest clicks must reach the window below the overlay.
    // Targeting and cropping need the content clickable, so they win.
    gboolean pass = (g_state.is_click_through || g_state.is_executing) &&
                    !g_state.is_targeting && !g_state.is_cropping;

    if (pass) {
        GtkAllocation alloc;
        gtk_widget_get_allocation(g_state.headerbar, &alloc);
        cairo_rectangle_int_t rect = {alloc.x, alloc.y, alloc.width, alloc.height};
        cairo_region_t *region = cairo_region_create_rectangle(&rect);
        gtk_widget_input_shape_combine_region(win, region);
        cairo_region_destroy(region);
    } else {
        gtk_widget_input_shape_combine_region(win, NULL);
    }
}

void app_update_window_lock(void) {
    gboolean locked = g_state.is_locked || g_state.is_executing;
    gtk_window_set_resizable(g_state.window, !locked);
}

void app_set_targeting(gboolean targeting) {
    g_state.is_targeting = targeting;
    if (targeting) g_state.is_cropping = FALSE;
    set_active_class(g_state.target_btn, "pob-active", g_state.is_targeting);
    set_active_class(g_state.crop_btn, "pob-active", g_state.is_cropping);
    gtk_widget_set_tooltip_text(g_state.target_btn,
                                targeting ? "Stop Targeting" : "Target");
    app_update_click_through();
    content_view_update_cursor_style();
}

void app_set_cropping(gboolean cropping) {
    g_state.is_cropping = cropping;
    if (cropping) g_state.is_targeting = FALSE;
    set_active_class(g_state.crop_btn, "pob-active", g_state.is_cropping);
    set_active_class(g_state.target_btn, "pob-active", g_state.is_targeting);
    gtk_widget_set_tooltip_text(g_state.crop_btn,
                                cropping ? "Stop Cropping" : "Crop");
    app_update_click_through();
    content_view_update_cursor_style();
}

void app_set_executing(gboolean executing) {
    g_state.is_executing = executing;
    if (executing) content_view_reset_anim();
    set_button_icon(g_state.play_btn, executing ? ICONS_STOP : ICONS_PLAY);
    gtk_widget_set_tooltip_text(g_state.play_btn, executing ? "Stop" : "Execute");
    // Don't hold keyboard focus while the agent drives other windows.
    gtk_window_set_accept_focus(g_state.window, !executing);
    app_update_window_lock();
    app_update_click_through();
    gtk_widget_queue_draw(g_state.content);
}

// ── dialogs ─────────────────────────────────────────────────────────────────

static void on_max_step_response(GtkDialog *dialog, gint response, gpointer data) {
    (void)data;
    core_bridge_resolve_max_step(response == GTK_RESPONSE_ACCEPT);
    gtk_widget_destroy(GTK_WIDGET(dialog));
}

void app_show_max_step_dialog(void) {
    GtkWidget *dialog = gtk_message_dialog_new(
        g_state.window, GTK_DIALOG_MODAL | GTK_DIALOG_DESTROY_WITH_PARENT,
        GTK_MESSAGE_WARNING, GTK_BUTTONS_NONE, "Warning");
    gtk_message_dialog_format_secondary_text(GTK_MESSAGE_DIALOG(dialog),
                                             "Max step exceed.");
    gtk_dialog_add_button(GTK_DIALOG(dialog), "Stop", GTK_RESPONSE_CANCEL);
    gtk_dialog_add_button(GTK_DIALOG(dialog), "Continue", GTK_RESPONSE_ACCEPT);
    g_signal_connect(dialog, "response", G_CALLBACK(on_max_step_response), NULL);
    gtk_widget_show_all(dialog);
}

enum {
    RESPONSE_RUN_INSTRUCTION = 1,
    RESPONSE_RUN_MACRO = 2,
};

static void on_macro_choice_response(GtkDialog *dialog, gint response, gpointer data) {
    (void)data;
    if (response == RESPONSE_RUN_INSTRUCTION)
        core_bridge_run_instruction(g_state.is_recording);
    else if (response == RESPONSE_RUN_MACRO)
        core_bridge_run_macro();
    gtk_widget_destroy(GTK_WIDGET(dialog));
}

static void show_macro_choice_dialog(void) {
    GtkWidget *dialog = gtk_message_dialog_new(
        g_state.window, GTK_DIALOG_MODAL | GTK_DIALOG_DESTROY_WITH_PARENT,
        GTK_MESSAGE_QUESTION, GTK_BUTTONS_NONE, "What would you like to run?");
    gtk_message_dialog_format_secondary_text(GTK_MESSAGE_DIALOG(dialog),
                                             "macro.txt has recorded actions.");
    gtk_dialog_add_button(GTK_DIALOG(dialog), "Cancel", GTK_RESPONSE_CANCEL);
    gtk_dialog_add_button(GTK_DIALOG(dialog), "Run Macro", RESPONSE_RUN_MACRO);
    gtk_dialog_add_button(GTK_DIALOG(dialog), "Run Instruction", RESPONSE_RUN_INSTRUCTION);
    g_signal_connect(dialog, "response", G_CALLBACK(on_macro_choice_response), NULL);
    gtk_widget_show_all(dialog);
}

enum {
    RESPONSE_CLEAR_INSTRUCTION = 1,
    RESPONSE_CLEAR_MACRO = 2,
    RESPONSE_CLEAR_LOGS = 3,
    RESPONSE_CLEAR_ALL = 4,
};

static void on_clear_response(GtkDialog *dialog, gint response, gpointer data) {
    (void)data;
    switch (response) {
    case RESPONSE_CLEAR_INSTRUCTION: settings_clear_instruction(); break;
    case RESPONSE_CLEAR_MACRO: settings_clear_macro(); break;
    case RESPONSE_CLEAR_LOGS: settings_clear_logs(); break;
    case RESPONSE_CLEAR_ALL:
        settings_clear_instruction();
        settings_clear_macro();
        settings_clear_logs();
        break;
    default: break;
    }
    gtk_widget_destroy(GTK_WIDGET(dialog));
}

static void show_clear_dialog(void) {
    GtkWidget *dialog = gtk_message_dialog_new(
        g_state.window, GTK_DIALOG_MODAL | GTK_DIALOG_DESTROY_WITH_PARENT,
        GTK_MESSAGE_QUESTION, GTK_BUTTONS_NONE, "Clear");
    GtkDialog *d = GTK_DIALOG(dialog);
    GtkWidget *b;
    b = gtk_dialog_add_button(d, "Clear Instruction", RESPONSE_CLEAR_INSTRUCTION);
    gtk_style_context_add_class(gtk_widget_get_style_context(b), "destructive-action");
    b = gtk_dialog_add_button(d, "Clear Macro", RESPONSE_CLEAR_MACRO);
    gtk_style_context_add_class(gtk_widget_get_style_context(b), "destructive-action");
    b = gtk_dialog_add_button(d, "Clear Logs", RESPONSE_CLEAR_LOGS);
    gtk_style_context_add_class(gtk_widget_get_style_context(b), "destructive-action");
    b = gtk_dialog_add_button(d, "Clear All", RESPONSE_CLEAR_ALL);
    gtk_style_context_add_class(gtk_widget_get_style_context(b), "destructive-action");
    gtk_dialog_add_button(d, "Cancel", GTK_RESPONSE_CANCEL);

    // Stack the buttons vertically, like the macOS confirmation dialog.
    GtkWidget *action_area;
    G_GNUC_BEGIN_IGNORE_DEPRECATIONS
    action_area = gtk_dialog_get_action_area(d);
    G_GNUC_END_IGNORE_DEPRECATIONS
    gtk_orientable_set_orientation(GTK_ORIENTABLE(action_area),
                                   GTK_ORIENTATION_VERTICAL);
    gtk_button_box_set_layout(GTK_BUTTON_BOX(action_area), GTK_BUTTONBOX_EXPAND);

    g_signal_connect(dialog, "response", G_CALLBACK(on_clear_response), NULL);
    gtk_widget_show_all(dialog);
}

static void show_about_dialog(void) {
    GtkWidget *dialog = gtk_dialog_new_with_buttons(
        "", g_state.window, GTK_DIALOG_MODAL | GTK_DIALOG_DESTROY_WITH_PARENT,
        "OK", GTK_RESPONSE_OK, NULL);
    GtkWidget *content = gtk_dialog_get_content_area(GTK_DIALOG(dialog));
    gtk_container_set_border_width(GTK_CONTAINER(content), 20);

    GtkWidget *name = gtk_label_new(NULL);
    gtk_label_set_markup(GTK_LABEL(name), "<b><big>Pob</big></b>");
    gtk_widget_set_halign(name, GTK_ALIGN_START);

    GtkWidget *full = gtk_label_new("Perception and Operation Bridge");
    gtk_widget_set_halign(full, GTK_ALIGN_START);
    gtk_style_context_add_class(gtk_widget_get_style_context(full), "dim-label");

    gchar *ver_text = g_strdup_printf("Version %s", app_version());
    GtkWidget *ver = gtk_label_new(ver_text);
    g_free(ver_text);
    gtk_widget_set_halign(ver, GTK_ALIGN_START);
    gtk_style_context_add_class(gtk_widget_get_style_context(ver), "dim-label");

    gtk_box_pack_start(GTK_BOX(content), name, FALSE, FALSE, 2);
    gtk_box_pack_start(GTK_BOX(content), full, FALSE, FALSE, 2);
    gtk_box_pack_start(GTK_BOX(content), ver, FALSE, FALSE, 2);

    g_signal_connect(dialog, "response", G_CALLBACK(gtk_widget_destroy), NULL);
    gtk_widget_show_all(dialog);
}

// ── toolbar actions ─────────────────────────────────────────────────────────

static void on_settings_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    settings_open_settings_file();
}

static void on_logs_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    settings_open_logs_folder();
}

static void on_applog_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    settings_open_app_log();
}

static void on_instruction_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    settings_open_instruction_file();
}

static void on_macro_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    settings_open_macro_file();
}

static void on_record_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    g_state.is_recording = !g_state.is_recording;
    if (g_state.is_recording) settings_clear_macro();
    core_bridge_recording_changed(g_state.is_recording);
    set_active_class(g_state.record_btn, "pob-recording", g_state.is_recording);
    gtk_widget_set_tooltip_text(g_state.record_btn,
                                g_state.is_recording ? "Recording (click to stop)"
                                                     : "Record Macro");
}

static void on_play_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    if (g_state.is_executing) {
        core_bridge_stop_execution();
        return;
    }
    gchar *macro = settings_get_macro();
    g_strstrip(macro);
    if (*macro == '\0')
        core_bridge_run_instruction(g_state.is_recording);
    else
        show_macro_choice_dialog();
    g_free(macro);
}

static void on_target_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    app_set_targeting(!g_state.is_targeting);
}

static void on_crop_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    app_set_cropping(!g_state.is_cropping);
}

static void on_clickthrough_realize(GtkWidget *w, gpointer d) {
    (void)w; (void)d;
    set_clickthrough_icon();
}

static void on_clickthrough_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    g_state.is_click_through = !g_state.is_click_through;
    set_clickthrough_icon();
    gtk_widget_set_tooltip_text(g_state.clickthrough_btn,
                                g_state.is_click_through
                                    ? "Click-Through On (click to disable)"
                                    : "Click-Through Off (click to enable)");
    app_update_click_through();
}

static void on_lock_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    g_state.is_locked = !g_state.is_locked;
    set_button_icon(g_state.lock_btn, g_state.is_locked ? ICONS_LOCKED : ICONS_UNLOCKED);
    gtk_widget_set_tooltip_text(g_state.lock_btn,
                                g_state.is_locked
                                    ? "Window Locked (click to unlock)"
                                    : "Window Unlocked (click to lock)");
    app_update_window_lock();
}

static void on_trash_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    show_clear_dialog();
}

// ── headerbar (toolbar + context menu + drag lock) ──────────────────────────

static gboolean on_headerbar_button_press(GtkWidget *w, GdkEventButton *ev, gpointer d) {
    (void)w; (void)d;
    if (ev->button == 3) {
        GtkWidget *menu = gtk_menu_new();
        GtkWidget *about = gtk_menu_item_new_with_label("About Pob");
        GtkWidget *quit = gtk_menu_item_new_with_label("Quit Pob");
        g_signal_connect_swapped(about, "activate", G_CALLBACK(show_about_dialog), NULL);
        g_signal_connect_swapped(quit, "activate", G_CALLBACK(g_application_quit),
                                 G_APPLICATION(g_state.app));
        gtk_menu_shell_append(GTK_MENU_SHELL(menu), about);
        gtk_menu_shell_append(GTK_MENU_SHELL(menu), gtk_separator_menu_item_new());
        gtk_menu_shell_append(GTK_MENU_SHELL(menu), quit);
        gtk_widget_show_all(menu);
        gtk_menu_popup_at_pointer(GTK_MENU(menu), (GdkEvent *)ev);
        return TRUE;
    }
    // Window locked (or executing): swallow the press so the WM drag never starts.
    if (g_state.is_locked || g_state.is_executing) return TRUE;
    return FALSE;
}

static GtkWidget *build_applog_button(void) {
    GtkWidget *btn = gtk_button_new();
    GtkWidget *box = gtk_box_new(GTK_ORIENTATION_VERTICAL, 0);
    GtkWidget *l1 = gtk_label_new("app");
    GtkWidget *l2 = gtk_label_new(".log");
    gtk_style_context_add_class(gtk_widget_get_style_context(l1), "pob-applog-label");
    gtk_style_context_add_class(gtk_widget_get_style_context(l2), "pob-applog-label");
    gtk_box_pack_start(GTK_BOX(box), l1, FALSE, FALSE, 0);
    gtk_box_pack_start(GTK_BOX(box), l2, FALSE, FALSE, 0);
    gtk_container_add(GTK_CONTAINER(btn), box);
    gtk_widget_set_tooltip_text(btn, "App Log");
    return btn;
}

static void build_headerbar(void) {
    GtkWidget *hb = gtk_header_bar_new();
    g_state.headerbar = hb;
    // Window controls (close/min/max) follow the DE's native Unix layout.
    gtk_header_bar_set_show_close_button(GTK_HEADER_BAR(hb), TRUE);
    // Hide the title text, like macOS titleVisibility(.hidden).
    gtk_header_bar_set_custom_title(GTK_HEADER_BAR(hb), gtk_label_new(""));

    GtkWidget *settings_btn = icon_button(ICONS_SETTINGS, "Settings");
    GtkWidget *logs_btn = icon_button(ICONS_LOGS, "Logs");
    GtkWidget *applog_btn = build_applog_button();
    GtkWidget *instruction_btn = icon_button(ICONS_INSTRUCTION, "Instruction");
    GtkWidget *macro_btn = icon_button(ICONS_MACRO, "Macro");
    g_state.record_btn = icon_button(ICONS_RECORD, "Record Macro");
    g_state.play_btn = icon_button(ICONS_PLAY, "Execute");
    g_state.target_btn = icon_button(ICONS_TARGET, "Target");
    g_state.crop_btn = icon_button(ICONS_CROP, "Crop");
    g_state.clickthrough_btn = icon_button(ICONS_HAND, "Click-Through Off (click to enable)");
    set_clickthrough_icon(); // initial OFF state shows the slashed hand
    // Re-render once realized so the slash picks up the final theme color.
    g_signal_connect(g_state.clickthrough_btn, "realize",
                     G_CALLBACK(on_clickthrough_realize), NULL);
    g_state.lock_btn = icon_button(ICONS_UNLOCKED, "Window Unlocked (click to lock)");
    GtkWidget *trash_btn = icon_button(ICONS_TRASH, "Clear");

    g_signal_connect(settings_btn, "clicked", G_CALLBACK(on_settings_clicked), NULL);
    g_signal_connect(logs_btn, "clicked", G_CALLBACK(on_logs_clicked), NULL);
    g_signal_connect(applog_btn, "clicked", G_CALLBACK(on_applog_clicked), NULL);
    g_signal_connect(instruction_btn, "clicked", G_CALLBACK(on_instruction_clicked), NULL);
    g_signal_connect(macro_btn, "clicked", G_CALLBACK(on_macro_clicked), NULL);
    g_signal_connect(g_state.record_btn, "clicked", G_CALLBACK(on_record_clicked), NULL);
    g_signal_connect(g_state.play_btn, "clicked", G_CALLBACK(on_play_clicked), NULL);
    g_signal_connect(g_state.target_btn, "clicked", G_CALLBACK(on_target_clicked), NULL);
    g_signal_connect(g_state.crop_btn, "clicked", G_CALLBACK(on_crop_clicked), NULL);
    g_signal_connect(g_state.clickthrough_btn, "clicked", G_CALLBACK(on_clickthrough_clicked), NULL);
    g_signal_connect(g_state.lock_btn, "clicked", G_CALLBACK(on_lock_clicked), NULL);
    g_signal_connect(trash_btn, "clicked", G_CALLBACK(on_trash_clicked), NULL);

    // Same left-to-right order as the macOS toolbar: file group, then actions.
    gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), settings_btn);
    gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), logs_btn);
    gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), applog_btn);
    gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), instruction_btn);
    gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), macro_btn);
    gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), g_state.record_btn);
    gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), g_state.play_btn);
    gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), g_state.target_btn);
    gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), g_state.crop_btn);
    gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), g_state.clickthrough_btn);
    gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), g_state.lock_btn);
    gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), trash_btn);

    gtk_widget_add_events(hb, GDK_BUTTON_PRESS_MASK);
    g_signal_connect(hb, "button-press-event",
                     G_CALLBACK(on_headerbar_button_press), NULL);
    // Input shape depends on the headerbar geometry — track it.
    g_signal_connect_swapped(hb, "size-allocate",
                             G_CALLBACK(app_update_click_through), NULL);

    gtk_window_set_titlebar(g_state.window, hb);
}

// ── window frame persistence ────────────────────────────────────────────────

static gboolean save_frame_now(gpointer data) {
    (void)data;
    save_frame_timeout = 0;
    int x, y, w, h;
    gtk_window_get_position(g_state.window, &x, &y);
    gtk_window_get_size(g_state.window, &w, &h);
    // The WM sends synthetic ConfigureNotify on focus changes; don't rewrite
    // settings.json unless the frame actually moved or resized.
    static int last_x, last_y, last_w, last_h;
    static gboolean seeded = FALSE;
    if (!seeded) {
        seeded = TRUE;
        if (!settings_get_window_frame(&last_x, &last_y, &last_w, &last_h))
            last_x = last_y = last_w = last_h = G_MININT;
    }
    if (x == last_x && y == last_y && w == last_w && h == last_h)
        return G_SOURCE_REMOVE;
    last_x = x; last_y = y; last_w = w; last_h = h;
    settings_save_window_frame(x, y, w, h);
    return G_SOURCE_REMOVE;
}

static gboolean on_configure(GtkWidget *w, GdkEventConfigure *ev, gpointer d) {
    (void)w; (void)ev; (void)d;
    if (save_frame_timeout) g_source_remove(save_frame_timeout);
    save_frame_timeout = g_timeout_add(500, save_frame_now, NULL);
    return FALSE;
}

// ── styling ─────────────────────────────────────────────────────────────────

static void install_css(void) {
    GtkCssProvider *provider = gtk_css_provider_new();
    // background-image must be cleared too — some themes (PiXflat on
    // Raspberry Pi OS) fill windows with an opaque background-image that a
    // background-color override alone doesn't remove.
    const char *css =
        "window.pob-window, window.pob-window decoration {\n"
        "  background-color: rgba(0, 0, 0, 0); background-image: none;\n"
        "}\n"
        ".pob-active { color: " POB_ACCENT_CSS "; }\n"
        ".pob-recording { color: " POB_RED_CSS "; }\n"
        ".pob-applog-label { font-family: monospace; font-size: 6pt; }\n"
        // Compact toolbar buttons, closer to the macOS unified-compact look.
        "window.pob-window headerbar button {\n"
        "  min-width: 22px; min-height: 22px; padding: 1px;\n"
        "  border-radius: 0;\n"
        "}\n";
    gtk_css_provider_load_from_data(provider, css, -1, NULL);
    gtk_style_context_add_provider_for_screen(
        gdk_screen_get_default(), GTK_STYLE_PROVIDER(provider),
        GTK_STYLE_PROVIDER_PRIORITY_APPLICATION);
    g_object_unref(provider);
}

// ── application lifecycle ───────────────────────────────────────────────────

// Clears the window to fully transparent before GTK draws the headerbar and
// content on top. More robust than relying on the theme honoring the CSS
// transparent background (still needs an RGBA visual + a compositor).
static gboolean on_window_draw(GtkWidget *widget, cairo_t *cr, gpointer data) {
    (void)widget;
    (void)data;
    cairo_save(cr);
    cairo_set_operator(cr, CAIRO_OPERATOR_SOURCE);
    cairo_set_source_rgba(cr, 0, 0, 0, 0);
    cairo_paint(cr);
    cairo_restore(cr);
    return FALSE; // continue with normal (headerbar + content) drawing
}

static void on_composited_changed(GdkScreen *screen, gpointer data) {
    (void)data;
    app_logger_log("Compositor state changed: composited=%d",
                   gdk_screen_is_composited(screen));
    gtk_widget_queue_draw(GTK_WIDGET(g_state.window));
}

// Logs the facts transparency depends on, so app.log answers "why is the
// window opaque" without guesswork.
static void on_window_realize(GtkWidget *w, gpointer data) {
    (void)data;
    GdkWindow *gw = gtk_widget_get_window(w);
    int depth = gw ? gdk_visual_get_depth(gdk_window_get_visual(gw)) : 0;
    app_logger_log("Window realized: visual depth=%d (32 = ARGB ok), composited=%d",
                   depth,
                   gdk_screen_is_composited(gtk_widget_get_screen(w)));
}

static void on_activate(GtkApplication *app, gpointer data) {
    (void)data;
    if (g_state.window) {
        gtk_window_present(g_state.window);
        return;
    }

    install_css();

    GtkWidget *win = gtk_application_window_new(app);
    g_state.window = GTK_WINDOW(win);

    gchar *title = g_strdup_printf("Pob %s", app_version());
    gtk_window_set_title(g_state.window, title);
    g_free(title);

    gtk_style_context_add_class(gtk_widget_get_style_context(win), "pob-window");
    gtk_widget_set_app_paintable(win, TRUE);

    // Translucency needs an RGBA visual and a running compositor.
    GdkScreen *screen = gdk_screen_get_default();
    GdkVisual *visual = gdk_screen_get_rgba_visual(screen);
    if (visual)
        gtk_widget_set_visual(win, visual);
    else
        app_logger_log("Warning: no RGBA visual — the overlay will not be transparent");
    if (!gdk_screen_is_composited(screen))
        app_logger_log("Warning: no compositor detected — the overlay will not be transparent "
                       "(on Raspberry Pi OS: raspi-config → Advanced Options → Compositor, "
                       "or install picom)");
    g_signal_connect(screen, "composited-changed",
                     G_CALLBACK(on_composited_changed), NULL);
    g_signal_connect(win, "draw", G_CALLBACK(on_window_draw), NULL);

    build_headerbar();
    g_state.content = content_view_new();
    gtk_container_add(GTK_CONTAINER(win), g_state.content);

    gtk_window_set_keep_above(g_state.window, TRUE); // macOS: window.level = .floating

    int x, y, w, h;
    if (settings_get_window_frame(&x, &y, &w, &h)) {
        gtk_window_set_default_size(g_state.window, w, h);
        gtk_window_move(g_state.window, x, y);
    } else {
        gtk_window_set_default_size(g_state.window, 600, 400);
        gtk_window_set_position(g_state.window, GTK_WIN_POS_CENTER);
    }

    g_signal_connect(win, "configure-event", G_CALLBACK(on_configure), NULL);
    g_signal_connect_swapped(win, "realize", G_CALLBACK(app_update_click_through), NULL);
    g_signal_connect(win, "realize", G_CALLBACK(on_window_realize), NULL);

    gtk_widget_show_all(win);

    app_logger_log("Pob started");
    mouse_service_init();
    core_bridge_start();
}

static void on_shutdown(GApplication *app, gpointer data) {
    (void)app;
    (void)data;
    if (save_frame_timeout) {
        g_source_remove(save_frame_timeout);
        save_frame_now(NULL);
    }
    core_bridge_stop();
    mouse_service_shutdown();
}

int main(int argc, char **argv) {
    XInitThreads(); // two X connections: GDK (main) + the mouse worker
    gdk_set_allowed_backends("x11");

    memset(&g_state, 0, sizeof(g_state));

    GtkApplication *app = gtk_application_new("jp.co.linktivity.pob",
                                              G_APPLICATION_NON_UNIQUE);
    g_state.app = app;
    g_signal_connect(app, "activate", G_CALLBACK(on_activate), NULL);
    g_signal_connect(app, "shutdown", G_CALLBACK(on_shutdown), NULL);

    // Quit Pob: Ctrl+Q (the Unix stand-in for the macOS app menu item).
    static const char *quit_accels[] = {"<Control>q", NULL};
    GSimpleAction *quit_action = g_simple_action_new("quit", NULL);
    g_signal_connect_swapped(quit_action, "activate",
                             G_CALLBACK(g_application_quit), app);
    g_action_map_add_action(G_ACTION_MAP(app), G_ACTION(quit_action));
    gtk_application_set_accels_for_action(app, "app.quit", quit_accels);

    int status = g_application_run(G_APPLICATION(app), argc, argv);
    g_object_unref(app);
    return status;
}
