// Pob Linux/X11 shell. A feature-for-feature port of the macOS shell
// (macos/Sources): translucent always-on-top overlay window, header-bar
// toolbar, targeting/crop/click-through/lock modes, and the CoreBridge that
// drives the shared Go core. Window controls follow the desktop
// environment's native (Unix) button layout; everything else mirrors macOS.
#include "app.h"
#include "app_logger.h"
#include "carry_service.h"
#include "content_view.h"
#include "core_bridge.h"
#include "mouse_service.h"
#include "settings_service.h"

#include <X11/Xatom.h>
#include <X11/Xlib.h>
#include <gdk/gdkx.h>
#include <string.h>

AppState g_state;

static guint save_frame_timeout = 0;

// ── version ─────────────────────────────────────────────────────────────────

const char *app_version(void) {
    static gchar *version = NULL;
    if (version) return version;

    // What the build compiled in (see the Makefile): an installed copy has no
    // VERSION file beside the binary, so the file below is only the fallback
    // for a build made without the stamp.
#ifdef POB_VERSION
    if (POB_VERSION[0] != '\0') {
        version = g_strdup(POB_VERSION);
        return version;
    }
#endif

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

// ── app icon ────────────────────────────────────────────────────────────────

// What the taskbar, alt-tab and the window itself show: the same drawing macOS
// puts in AppIcon.icns and Windows embeds in Pob.exe, shipped here as a plain
// 256×256 PNG. It is only decoration — a missing file is logged, not fatal.
//
// Packaged install: pob.png sits next to the shell binary (the installer keeps
// that layout). Dev workflow: walk up from linux-x11/bin/pob towards the
// repository root and use the master copy under assets/icon.
static void install_app_icon(void) {
    gchar *self = g_file_read_link("/proc/self/exe", NULL);
    if (!self) return;
    gchar *dir = g_path_get_dirname(self);
    g_free(self);

    gchar *path = g_build_filename(dir, "pob.png", NULL);
    for (int i = 0; i < 6 && !g_file_test(path, G_FILE_TEST_IS_REGULAR); i++) {
        gchar *parent = g_path_get_dirname(dir);
        g_free(dir);
        dir = parent;
        g_free(path);
        path = g_build_filename(dir, "assets", "icon", "pob.png", NULL);
    }
    g_free(dir);

    GError *error = NULL;
    if (!gtk_window_set_default_icon_from_file(path, &error)) {
        app_logger_error("no window icon (%s): %s", path,
                         error ? error->message : "not found");
        g_clear_error(&error);
    }
    g_free(path);
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
    // Natural (square) size, centered — don't stretch to the headerbar height.
    gtk_widget_set_valign(btn, GTK_ALIGN_CENTER);
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
static const char *const ICONS_INSTANCE[] = {"folder-symbolic", "folder-open-symbolic", NULL};
static const char *const ICONS_LOGS[] = {"text-x-generic-symbolic", "document-open-symbolic", NULL};
static const char *const ICONS_MACRO[] = {"system-run-symbolic", "application-x-executable-symbolic", NULL};
static const char *const ICONS_RECORD[] = {"media-record-symbolic", NULL};
static const char *const ICONS_PLAY[] = {"media-playback-start-symbolic", NULL};
static const char *const ICONS_STOP[] = {"media-playback-stop-symbolic", NULL};
static const char *const ICONS_TARGET[] = {"find-location-symbolic", "edit-find-symbolic", NULL};
static const char *const ICONS_CROP[] = {"image-crop-symbolic", "edit-select-all-symbolic", NULL};
static const char *const ICONS_SCREENSHOT[] = {"camera-photo-symbolic", "camera-web-symbolic", NULL};
// Plain hand only — never a mouse/touchpad stand-in; themes shipping neither
// name get the drawn silhouette below instead.
static const char *const ICONS_HAND[] = {"hand-open-symbolic", "touch-symbolic", NULL};
static const char *const ICONS_LOCKED[] = {"changes-prevent-symbolic", NULL};
static const char *const ICONS_UNLOCKED[] = {"changes-allow-symbolic", NULL};
static const char *const ICONS_RESET[] = {"view-refresh-symbolic", "edit-undo-symbolic", NULL};
static const char *const ICONS_HIDE_MENU[] = {"pan-up-symbolic", "go-up-symbolic", NULL};
static const char *const ICONS_MINIMIZE[] = {"window-minimize-symbolic", "go-down-symbolic", NULL};
static const char *const ICONS_MAXIMIZE[] = {"window-maximize-symbolic", "go-up-symbolic", NULL};
static const char *const ICONS_RESTORE[] = {"window-restore-symbolic", "view-restore-symbolic", NULL};
static const char *const ICONS_CLOSE[] = {"window-close-symbolic", NULL};

// Filled raised-hand silhouette (four fingers over a rounded palm plus a
// thumb), drawn in the theme text color. Fallback for icon themes that ship
// no hand glyph at all. Design space is 16 units, scaled to POB_ICON_SIZE.
static void draw_hand_silhouette(cairo_t *cr, const GdkRGBA *color) {
    gdk_cairo_set_source_rgba(cr, color);
    cairo_save(cr);
    cairo_scale(cr, POB_ICON_SIZE / 16.0, POB_ICON_SIZE / 16.0);

    static const double finger_x[4] = {3.1, 5.7, 8.3, 10.9};
    static const double finger_top[4] = {3.2, 1.4, 1.0, 2.4};
    const double r = 1.15;
    for (int i = 0; i < 4; i++) {
        cairo_new_sub_path(cr);
        cairo_arc(cr, finger_x[i], finger_top[i] + r, r, G_PI, 2 * G_PI);
        cairo_line_to(cr, finger_x[i] + r, 10.0);
        cairo_line_to(cr, finger_x[i] - r, 10.0);
        cairo_close_path(cr);
        cairo_fill(cr);
    }

    const double px0 = 1.95, px1 = 12.05, py1 = 15.0, pr = 2.4;
    cairo_move_to(cr, px0, 8.0);
    cairo_line_to(cr, px1, 8.0);
    cairo_arc(cr, px1 - pr, py1 - pr, pr, 0, G_PI / 2);
    cairo_arc(cr, px0 + pr, py1 - pr, pr, G_PI / 2, G_PI);
    cairo_close_path(cr);
    cairo_fill(cr);

    // Thumb: a tilted ellipse hugging the palm's right edge.
    cairo_save(cr);
    cairo_translate(cr, 13.1, 10.2);
    cairo_rotate(cr, -0.55);
    cairo_scale(cr, 1.0, 1.9);
    cairo_arc(cr, 0, 0, 1.05, 0, 2 * G_PI);
    cairo_restore(cr);
    cairo_fill(cr);

    cairo_restore(cr);
}

static void set_clickthrough_icon(void) {
    GtkWidget *btn = g_state.clickthrough_btn;
    int scale = gtk_widget_get_scale_factor(btn);
    GtkStyleContext *style = gtk_widget_get_style_context(btn);
    GdkRGBA color;
    gtk_style_context_get_color(style, gtk_widget_get_state_flags(btn), &color);

    GtkIconTheme *theme = gtk_icon_theme_get_default();
    GdkPixbuf *pix = NULL;
    for (int i = 0; ICONS_HAND[i] && !pix; i++) {
        if (!gtk_icon_theme_has_icon(theme, ICONS_HAND[i])) continue;
        GtkIconInfo *info = gtk_icon_theme_lookup_icon_for_scale(
            theme, ICONS_HAND[i], POB_ICON_SIZE, scale, GTK_ICON_LOOKUP_USE_BUILTIN);
        if (!info) continue;
        pix = gtk_icon_info_load_symbolic_for_context(info, style, NULL, NULL);
        g_object_unref(info);
    }

    cairo_surface_t *surf;
    if (pix) {
        surf = gdk_cairo_surface_create_from_pixbuf(pix, scale,
                                                    gtk_widget_get_window(btn));
        g_object_unref(pix);
    } else {
        surf = cairo_image_surface_create(CAIRO_FORMAT_ARGB32,
                                          POB_ICON_SIZE * scale,
                                          POB_ICON_SIZE * scale);
        cairo_surface_set_device_scale(surf, scale, scale);
        cairo_t *cr = cairo_create(surf);
        draw_hand_silhouette(cr, &color);
        cairo_destroy(cr);
    }

    if (!g_state.is_click_through) {
        // OFF: draw the diagonal "disabled" slash in the theme text color.
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
    if (!gtk_widget_get_realized(win) || !g_state.content) return;

    // Fullscreen passes everything through, always. There is no headerbar to
    // press and no edge to resize by — the frame is the screen and stays that
    // way — and it covers every pixel the user has: a live area anywhere on it
    // would be desktop that had stopped answering, with no button on screen to
    // hand it back. An empty input region is the whole window given up.
    if (g_state.is_fullscreen) {
        cairo_region_t *nothing = cairo_region_create();
        gtk_widget_input_shape_combine_region(win, nothing);
        cairo_region_destroy(nothing);
        return;
    }

    // Pass clicks through the content area (toolbar stays interactive) when
    // the user enabled click-through, or while executing — on X11 the
    // synthesized XTest clicks must reach the window below the overlay.
    // Targeting and cropping need the content clickable, so they win.
    gboolean pass = (g_state.is_click_through || g_state.is_executing) &&
                    !g_state.is_targeting && !g_state.is_cropping;

    if (pass) {
        GtkAllocation alloc, hb, content;
        gtk_widget_get_allocation(win, &alloc);
        gtk_widget_get_allocation(g_state.headerbar, &hb);
        gtk_widget_get_allocation(g_state.content, &content);

        // Interactive: the headerbar, plus — while the window can be resized
        // — the CSD shadow band around the visible frame. GTK parks its eight
        // input-only resize handles in that band (gtkwindow.c,
        // update_border_windows: they sit *outside* the frame, the part of
        // each corner handle that would reach inside is shaped away), and an
        // input shape clipped to the frame takes them with it — no resize
        // cursor, no resize. A locked (or executing) window cannot be resized
        // anyway, so it keeps the band out and the invisible shadow catches
        // no clicks.
        //
        // A hidden menu has no headerbar on the window at all, and the
        // allocation GTK left on the hidden widget is the one it had when it
        // was last on screen — a band that now falls across the top of the
        // content. The frame starts at the content instead, and the dot below
        // is what is kept live in the headerbar's place.
        gboolean resizable = !g_state.is_locked && !g_state.is_executing;
        int top = g_state.is_menu_hidden ? content.y : hb.y;
        cairo_rectangle_int_t frame = {
            content.x, top, content.width, content.y + content.height - top,
        };
        if (resizable) {
            frame.x = 0;
            frame.y = 0;
            frame.width = alloc.width;
            frame.height = alloc.height;
        }
        cairo_region_t *region = cairo_region_create_rectangle(&frame);
        if (!g_state.is_menu_hidden) {
            cairo_rectangle_int_t hbr = {hb.x, hb.y, hb.width, hb.height};
            cairo_region_union_rectangle(region, &hbr);
        }

        cairo_rectangle_int_t hole = {content.x, content.y, content.width,
                                      content.height};
        cairo_region_subtract_rectangle(region, &hole);

        // The dot the hidden menu left behind, added back after the hole is
        // punched: it is the only way to the toolbar, and it lives inside the
        // content area, which is the part of the window that has just been
        // given away. Not while the agent is driving, though — those same
        // pixels are where a macro's clicks are aimed, and a live dot would
        // swallow one meant for the application below.
        if (g_state.is_menu_hidden && !g_state.is_executing) {
            cairo_rectangle_int_t dot = {
                content.x + content.width - POB_MENU_DOT_INSET - POB_MENU_DOT_HIT / 2,
                content.y + POB_MENU_DOT_INSET - POB_MENU_DOT_HIT / 2,
                POB_MENU_DOT_HIT, POB_MENU_DOT_HIT,
            };
            cairo_region_union_rectangle(region, &dot);
        }

        gtk_widget_input_shape_combine_region(win, region);
        cairo_region_destroy(region);
    } else {
        gtk_widget_input_shape_combine_region(win, NULL);
    }
}

// The content area in root device pixels — the box the screenshots are of and
// the clicks are aimed through.
//
// The same rect the capture is taken from (see screenshot_service.c), which is
// what makes the window found under it the window the screenshots are of
// (carry_service.c), and the rect a launched window is fitted to the frame the
// screenshots will be of (launch_service.c).
//
// GTK reports a widget's geometry in logical pixels and X places windows in
// device ones, so on a scaled display the frame is worth more than its face
// value by the time it reaches anything below it.
gboolean app_content_rect(GdkRectangle *out) {
    GtkWidget *win = GTK_WIDGET(g_state.window);
    GdkWindow *gdk_win = win ? gtk_widget_get_window(win) : NULL;
    if (!gdk_win || !g_state.content) return FALSE;

    int rel_x = 0, rel_y = 0;
    gtk_widget_translate_coordinates(g_state.content, win, 0, 0, &rel_x, &rel_y);
    int origin_x = 0, origin_y = 0;
    gdk_window_get_origin(gdk_win, &origin_x, &origin_y);
    int scale = gtk_widget_get_scale_factor(win);

    GtkAllocation alloc;
    gtk_widget_get_allocation(g_state.content, &alloc);

    out->x = (origin_x + rel_x) * scale;
    out->y = (origin_y + rel_y) * scale;
    out->width = alloc.width * scale;
    out->height = alloc.height * scale;
    return TRUE;
}

// ── putting the frame back where it was ─────────────────────────────────────
//
// Three steps, and what makes them three is that the frame is placed by the
// window manager: where it went can only be read back after the WM has been
// round to put it there, so each step waits a round trip for the last one.
//
//   unmaximize_in_place    the frame comes off the maximized state
//   settle_placed_frame    did it land where it was asked to? ask again if not
//   start_carry_when_placed   carry takes over a frame that has stopped moving
#define PLACE_SETTLE_MS 300

// Where the frame was asked to be, and the step waiting to look.
static int placed_x, placed_y;
static guint place_settle_source;

static gboolean settle_placed_frame(gpointer data);
static gboolean start_carry_when_placed(gpointer data);

// Takes a maximized frame off the maximized state without moving it — the
// window is left standing over exactly the pixels it was covering, as an
// ordinary window of that size and position.
//
// The lock is what needs this (see app_update_window_lock). TRUE when there was
// a maximized frame to put back, and the frame is on its way rather than where
// it will end up.
static gboolean unmaximize_in_place(void) {
    if (!g_state.window || !gtk_window_is_maximized(g_state.window)) return FALSE;

    int x = 0, y = 0;
    gtk_window_get_position(g_state.window, &x, &y);

    // Only the position is asked for here. The size is already held, as the
    // size the window opens at (hold_window_size, called before this), and that
    // is a number GTK turns into hints itself — where a window asked outright
    // to be a size while it is still maximized is asked in a different
    // arithmetic than the one it will be measured in a moment later, and comes
    // out a shadow's width short about half the time.
    //
    // The move is asked for after the state is dropped rather than before, so
    // the window manager's own restore — back to whatever the frame was before
    // it was maximized — is overwritten instead of raced with.
    gtk_window_unmaximize(g_state.window);
    gtk_window_move(g_state.window, x, y);
    app_logger_log("Window unmaximized in place at %d,%d", x, y);

    placed_x = x;
    placed_y = y;
    if (place_settle_source) g_source_remove(place_settle_source);
    place_settle_source = g_timeout_add(PLACE_SETTLE_MS, settle_placed_frame, NULL);
    return TRUE;
}

// Where did it land?
//
// A move asked for while the window is still maximized is a move a
// client-side-decorated window gets slightly wrong: GTK places the window by
// its own top-left and reports it by the visible frame's, and the invisible
// shadow band the theme draws between the two is not in GTK's arithmetic while
// the window is maximized — a maximized window has no shadow. The frame lands a
// shadow's width down and to the right of where it was asked for.
//
// So the position is asked for a second time, now that the window is an
// ordinary one again and the band is back. Asked for, not corrected by the
// difference: the difference *is* the band, and asking again is what puts it
// in. Once only — a window manager that places windows where it likes would
// otherwise be argued with forever.
static gboolean settle_placed_frame(gpointer data) {
    (void)data;
    place_settle_source = 0;
    if (!g_state.window) return G_SOURCE_REMOVE;

    int x = 0, y = 0;
    gtk_window_get_position(g_state.window, &x, &y);
    if (x != placed_x || y != placed_y) {
        gtk_window_move(g_state.window, placed_x, placed_y);
        app_logger_log("Window frame landed at %d,%d rather than %d,%d — asked again",
                       x, y, placed_x, placed_y);
    }
    place_settle_source =
        g_timeout_add(PLACE_SETTLE_MS, start_carry_when_placed, NULL);
    return G_SOURCE_REMOVE;
}

// Carry starts, from where the frame has come to rest.
//
// It is what watches the frame for drags, and it notices them by the frame
// having moved — so a frame still being put back is a drag it would invent, and
// the windows under the frame would travel a shadow's width for a lock nobody
// dragged anything with. Its own seeding (carry_service_set_enabled) takes the
// frame as it now stands, which is the point of waiting for it to stand still.
static gboolean start_carry_when_placed(gpointer data) {
    (void)data;
    place_settle_source = 0;
    carry_service_set_enabled(g_state.is_locked || g_state.is_executing);
    return G_SOURCE_REMOVE;
}

// Writes the size the frame has now down as the size it opens at, which is the
// only way to keep the lock from resizing it.
//
// On the other two shells the lock is a resize taken away and nothing else:
// AppKit drops .resizable, the Windows shell refuses the edge drag, and either
// way the window stays the size it was. GTK does not work like that. A window
// it has been told is not resizable is one GTK sizes itself, and the size it
// reaches for is the one the window was given to open at
// (gtk_window_set_default_size) — not the size the window has on screen. It
// says that size to the window manager as min = max hints, and the window
// manager obeys, so a frame dragged smaller since startup is snapped back to
// the startup size by the act of locking it. Geometry hints of our own are no
// answer either: the non-resizable path overwrites them (all measured against
// GTK 3.24 on the microVM's openbox, with the frame small and the lock going
// on).
//
// In the guest that is the difference between a window and a window taller than
// the screen it is on: ~/.pob is copied in whole (vm/msb/run.sh), so a frame
// left at 997x935 on a Mac is what the guest's Pob opens with on a 1024x768
// screen, and the lock is what makes it take it.
//
// The size is read and written in GTK's own space — the visible frame, without
// the invisible shadow band around it — which is the space default sizes are
// said in. GTK puts the band back when it turns this into hints for the window
// manager.
static void hold_window_size(void) {
    if (!g_state.window) return;
    int w = 0, h = 0;
    gtk_window_get_size(g_state.window, &w, &h);
    if (w <= 0 || h <= 0) return;
    gtk_window_set_default_size(g_state.window, w, h);
    app_logger_log("Window held at %dx%d", w, h);
}

// The lock holds the frame's size, and holds it onto what it frames.
//
// Moving stays allowed, because with the lock on a move no longer costs
// anything: the windows under the frame travel with it (see carry_service.c),
// so a macro's coordinates still land where they landed when it was recorded.
// A resize is the one that cannot be made harmless that way — it changes where
// every pixel inside the frame sits, and no amount of moving the windows below
// puts them back.
void app_update_window_lock(void) {
    // A fullscreen window is already held to the screen and has no headerbar to
    // drag it by, so there is neither a resize to take away nor anything below
    // to carry.
    if (g_state.is_fullscreen) return;
    gboolean locked = g_state.is_locked || g_state.is_executing;
    // What the frame is held at is the size it has this moment, said where GTK
    // reads a fixed size from (hold_window_size). Before the window is on
    // screen — a lock restored at startup — that is the frame it is opening
    // with, which is the right answer there too.
    if (locked) hold_window_size();
    // A maximized frame comes off the maximized state first, over the same
    // pixels it was already covering: the lock would otherwise move it. A
    // window that cannot be resized is one a window manager will not keep
    // maximized either, and it answers the fixed size below by unmaximizing —
    // openbox, the window manager in the microVM, puts the frame back where it
    // was maximized *from* while the lock holds it at the size it has now. A
    // frame filling the screen lands mostly off it, toolbar and all.
    gboolean placing = locked && unmaximize_in_place();
    gtk_window_set_resizable(g_state.window, !locked);
    // A frame still being put back is handed to carry by the placement itself,
    // once it has stopped moving (start_carry_when_placed).
    if (!placing) carry_service_set_enabled(locked);
    // The input shape only keeps GTK's resize handles while resizing is allowed.
    app_update_click_through();
}

// Sets the window lock and syncs the toolbar button (icon, tooltip).
static void apply_locked(gboolean on) {
    if (g_state.is_locked == on) return;
    g_state.is_locked = on;
    set_button_icon(g_state.lock_btn, on ? ICONS_LOCKED : ICONS_UNLOCKED);
    gtk_widget_set_tooltip_text(
        g_state.lock_btn,
        on ? "Window Locked — fixed size, and dragging carries the windows below (click to unlock)"
           : "Window Unlocked (click to lock)");
    app_update_window_lock();
}

// The lock as the instance now stands, written to instance.json so the next
// run starts the way this one was left: a window locked to hold a macro's
// coordinates would otherwise come back loose and have to be locked again.
static void set_locked(gboolean on) {
    if (g_state.is_locked == on) return;
    apply_locked(on);
    settings_save_window_locked(on);
}

// Sets click-through and syncs the toolbar button (icon, tooltip, shape).
//
// Written to instance.json like the lock, so the next run starts the way this
// one was left: an overlay left sitting over the app it drives would otherwise
// come back swallowing the clicks meant for what is underneath.
static void set_click_through(gboolean on) {
    if (g_state.is_click_through == on) return;
    g_state.is_click_through = on;
    set_clickthrough_icon();
    gtk_widget_set_tooltip_text(g_state.clickthrough_btn,
                                on ? "Click-Through On (click to disable)"
                                   : "Click-Through Off (click to enable)");
    app_update_click_through();
    // Not from a fullscreen run: the window is the whole screen there and goes
    // on passing clicks through whatever this says, so what is written down
    // would be a state the run never actually had.
    if (!g_state.is_fullscreen) settings_save_click_through(on);
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

// Moves and resizes the frame in one go, lock and all.
//
// A locked window is one GTK sizes itself, from the size it was given to open
// at (see hold_window_size), and it refuses gtk_window_resize outright — so the
// lock comes off for the length of the placement and the new size is written
// down as the size it opens at before it goes back on.
static void place_frame(int x, int y, int w, int h) {
    // A maximized frame is one the window manager places, and it answers
    // gtk_window_resize and gtk_window_move with nothing at all. It comes off
    // the maximized state first, over the same pixels it was already covering
    // — the move below overwrites the WM's own restore.
    if (gtk_window_is_maximized(g_state.window))
        gtk_window_unmaximize(g_state.window);
    gboolean resizable = gtk_window_get_resizable(g_state.window);
    if (!resizable) gtk_window_set_resizable(g_state.window, TRUE);
    gtk_window_resize(g_state.window, w, h);
    gtk_window_move(g_state.window, x, y);
    if (!resizable) {
        gtk_window_set_default_size(g_state.window, w, h);
        gtk_window_set_resizable(g_state.window, FALSE);
    }
}

// Takes the headerbar off the window — toolbar, title and window buttons with
// it — and puts it back. What is left is the content area and a small black dot
// in its top-right corner (content_view.c draws it, app_update_click_through
// keeps its pixels live), and clicking the dot brings it all back.
//
// The frame gives up exactly the headerbar it was wearing: moved down by its
// height and shortened by it, so the content stands over the same pixels it did
// a moment before. The content is what a screenshot is of and what every click
// is aimed through, so a macro recorded before the menu went away still lands
// where it was aimed.
void app_set_menu_hidden(gboolean hidden) {
    // Nothing to hide in fullscreen: the headerbar is never shown there, and
    // the frame is the screen rather than something to take a strip off.
    if (g_state.is_fullscreen) return;
    if (g_state.is_menu_hidden == hidden) return;
    if (!g_state.window || !g_state.headerbar) return;

    int x = 0, y = 0, w = 0, h = 0;
    gtk_window_get_position(g_state.window, &x, &y);
    gtk_window_get_size(g_state.window, &w, &h);

    if (hidden) {
        GtkAllocation hb;
        gtk_widget_get_allocation(g_state.headerbar, &hb);
        g_state.hidden_bar_height = hb.height;
        g_state.is_menu_hidden = TRUE;
        gtk_widget_set_no_show_all(g_state.headerbar, TRUE);
        gtk_widget_hide(g_state.headerbar);
    } else {
        g_state.is_menu_hidden = FALSE;
        gtk_widget_set_no_show_all(g_state.headerbar, FALSE);
        gtk_widget_show(g_state.headerbar);
    }

    int bar = hidden ? g_state.hidden_bar_height : -g_state.hidden_bar_height;
    app_logger_log("Menu %s — frame %dx%d+%d+%d becomes %dx%d+%d+%d",
                   hidden ? "hidden" : "shown", w, h, x, y, w, h - bar, x, y + bar);

    // Carry follows the frame, and it cannot tell this from a drag: the frame
    // is about to move a headerbar's worth down the screen with a button that
    // was down a moment ago (the press that opened this), which is the shape of
    // a quick drag. It goes off for the length of the placement and is seeded
    // again from where the frame comes to rest — the same wait the lock uses.
    carry_service_set_enabled(FALSE);
    place_frame(x, y + bar, w, h - bar);
    if (place_settle_source) g_source_remove(place_settle_source);
    place_settle_source =
        g_timeout_add(PLACE_SETTLE_MS, start_carry_when_placed, NULL);

    app_update_click_through();
    // The dot's hand cursor goes with the dot, and the press that was on it is
    // over: this is what puts the pointer back to the mode's own cursor.
    content_view_update_cursor_style();
    gtk_widget_queue_draw(g_state.content);
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

// Syncs the record button's look and tooltip with g_state.is_recording.
static void sync_record_button(void) {
    set_active_class(g_state.record_btn, "pob-recording", g_state.is_recording);
    gtk_widget_set_tooltip_text(g_state.record_btn,
                                g_state.is_recording ? "Recording (click to stop)"
                                                     : "Record Macro");
}

// Starts macro recording, either over an emptied main.macro.psl or appending to
// what is already in it.
static void start_recording(gboolean clearing_macro) {
    if (clearing_macro) {
        settings_clear_macro();
    } else {
        // Everything recorded next is a relative move from (20, 20), where a
        // replay starts. The kept lines leave the cursor wherever they leave
        // it, so send it home between them and what follows.
        gchar *macro = settings_get_macro();
        g_strstrip(macro);
        if (*macro != '\0') settings_append_macro("resetCursor()");
        g_free(macro);
    }
    g_state.is_recording = TRUE;
    core_bridge_recording_changed(TRUE);
    // Recorded coordinates are relative to the window, so a nudge or a resize
    // partway through aims the replay at the wrong pixels. Unlocking again is
    // the user's call.
    set_locked(TRUE);
    // Same behavior as macOS: starting to record outside a session enables
    // click-through so interactions reach the app below the overlay.
    if (!g_state.is_executing) set_click_through(TRUE);
    content_view_show_message("Recording started");
    sync_record_button();
}

static void stop_recording(void) {
    g_state.is_recording = FALSE;
    core_bridge_recording_changed(FALSE);
    content_view_show_message("Recording stopped");
    sync_record_button();
}

// ── the same three, asked for from the terminal ─────────────────────────────
//
// The toolbar's lock, click-through and record buttons, pressed by `pob lock`,
// `pob clickthrough` and `pob record`. They go through the functions the button
// handlers go through rather than at g_state directly, so everything that
// follows a press follows this too.

void app_set_locked(gboolean locked) {
    set_locked(locked);
}

void app_set_click_through(gboolean on) {
    set_click_through(on);
}

void app_set_recording(gboolean recording) {
    if (recording == g_state.is_recording) return;
    if (recording) {
        // No Clear/Keep dialog: there is nobody at the window to answer it, so
        // the answer is the one that cannot lose work — what is in the macro is
        // kept and the recording is appended to it.
        start_recording(FALSE);
    } else {
        stop_recording();
    }
}

// ── dialogs ─────────────────────────────────────────────────────────────────

static void on_alert_response(GtkDialog *dialog, gint response, gpointer data) {
    (void)response;
    (void)data;
    gtk_widget_destroy(GTK_WIDGET(dialog));
}

void app_show_alert_dialog(const char *title, const char *message) {
    GtkWidget *dialog = gtk_message_dialog_new(
        g_state.window, GTK_DIALOG_MODAL | GTK_DIALOG_DESTROY_WITH_PARENT,
        GTK_MESSAGE_ERROR, GTK_BUTTONS_OK, "%s", title && *title ? title : "Pob");
    if (message && *message)
        gtk_message_dialog_format_secondary_text(GTK_MESSAGE_DIALOG(dialog), "%s", message);
    g_signal_connect(dialog, "response", G_CALLBACK(on_alert_response), NULL);
    gtk_widget_show_all(dialog);
}

enum {
    RESPONSE_RECORD_CLEAR = 1,
    RESPONSE_RECORD_KEEP = 2,
};

static void on_record_warning_response(GtkDialog *dialog, gint response, gpointer data) {
    (void)data;
    if (response == RESPONSE_RECORD_CLEAR)
        start_recording(TRUE);
    else if (response == RESPONSE_RECORD_KEEP)
        start_recording(FALSE);
    gtk_widget_destroy(GTK_WIDGET(dialog));
}

static void show_record_warning_dialog(void) {
    GtkWidget *dialog = gtk_message_dialog_new(
        g_state.window, GTK_DIALOG_MODAL | GTK_DIALOG_DESTROY_WITH_PARENT,
        GTK_MESSAGE_WARNING, GTK_BUTTONS_NONE, "Warning");
    gtk_message_dialog_format_secondary_text(
        GTK_MESSAGE_DIALOG(dialog),
        "main.macro.psl has recorded actions. Clear them before recording?");
    gtk_dialog_add_button(GTK_DIALOG(dialog), "Cancel", GTK_RESPONSE_CANCEL);
    gtk_dialog_add_button(GTK_DIALOG(dialog), "Keep", RESPONSE_RECORD_KEEP);
    gtk_dialog_add_button(GTK_DIALOG(dialog), "Clear", RESPONSE_RECORD_CLEAR);
    g_signal_connect(dialog, "response", G_CALLBACK(on_record_warning_response), NULL);
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

static void on_instance_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    settings_open_instance_folder();
}

static void on_logs_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    settings_open_logs_folder();
}

// Alt-click asks for the app log instead — same button, the wider record.
// The state comes from the event that raised "clicked", which GtkButton emits
// while that event is still current.
static void on_inslog_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    GdkModifierType state = 0;
    if (gtk_get_current_event_state(&state) && (state & GDK_MOD1_MASK))
        settings_open_app_log();
    else
        settings_open_instance_log();
}

static void on_macro_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    settings_open_macro_file();
}

static void on_record_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    if (g_state.is_recording) {
        stop_recording();
        return;
    }
    gchar *macro = settings_get_macro();
    g_strstrip(macro);
    if (*macro == '\0')
        start_recording(FALSE);
    else
        // Recording appends, so whatever is in main.macro.psl already would
        // replay in front of everything recorded next.
        show_record_warning_dialog();
    g_free(macro);
}

static void on_play_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    if (g_state.is_executing) {
        core_bridge_stop_execution();
        return;
    }
    set_locked(TRUE);
    core_bridge_run_macro();
}

static void on_target_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    app_set_targeting(!g_state.is_targeting);
}

static void on_crop_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    app_set_cropping(!g_state.is_cropping);
}

static void on_screenshot_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    core_bridge_take_screenshot();
}

static void on_clickthrough_realize(GtkWidget *w, gpointer d) {
    (void)w; (void)d;
    set_clickthrough_icon();
}

static void on_clickthrough_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    set_click_through(!g_state.is_click_through);
}

static void on_lock_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    set_locked(!g_state.is_locked);
}

static void on_reset_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    mouse_reset_cursor();
    content_view_show_message("Mouse position reset");
}

static void on_hidemenu_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    app_set_menu_hidden(TRUE);
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
    // A locked window still drags: the lock holds its size, and what it frames
    // comes along with it. Only resizing is taken away, which
    // gtk_window_set_resizable and the input shape handle between them.
    return FALSE;
}

static GtkWidget *build_inslog_button(void) {
    GtkWidget *btn = gtk_button_new();
    GtkWidget *label = gtk_label_new(".log");
    gtk_style_context_add_class(gtk_widget_get_style_context(label), "pob-inslog-label");
    gtk_container_add(GTK_CONTAINER(btn), label);
    gtk_widget_set_tooltip_text(btn, "Instance Log — Alt for app.log");
    gtk_widget_set_valign(btn, GTK_ALIGN_CENTER);
    return btn;
}

// The app name at the very leading edge, and the way to the About box. macOS
// carries both in the menu bar at the top-left of the screen; X11 has no such
// place, so the headerbar does — click the name for the version, the same
// panel the titlebar menu's "About Pob" opens.
static void on_app_name_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    show_about_dialog();
}

static GtkWidget *build_app_name_button(void) {
    GtkWidget *btn = gtk_button_new_with_label("Pob");
    gtk_style_context_add_class(gtk_widget_get_style_context(btn),
                                "pob-app-name");
    gtk_widget_set_valign(btn, GTK_ALIGN_CENTER);
    gtk_widget_set_tooltip_text(btn, "About Pob");
    g_signal_connect(btn, "clicked", G_CALLBACK(on_app_name_clicked), NULL);
    return btn;
}

// This instance's id (pb-xxxx) as small monospaced text — the same badge the
// macOS toolbar shows beside the window buttons. It names the instance's
// ~/.pob/<instance>/ directory; clicking copies the dashboard it answers at,
// like the coordinate labels copy what they point at.
static GtkWidget *instance_id_btn;

// Where this instance answers on the network, as the core reports it, or NULL
// while the server is off.
static gchar *server_url;

// The dashboard: the server's bare root, which answers with the index page —
// what is running here, and links on to the control and view pages. The
// address the core reports names the machine in its path, and a GET there is a
// picture of the screen rather than somewhere to land, so the path is dropped
// and the origin is what gets handed out. Caller frees.
static gchar *dashboard_url(void) {
    if (!server_url) return NULL;
    const char *scheme = strstr(server_url, "://");
    const char *host = scheme ? scheme + 3 : server_url;
    const char *slash = strchr(host, '/');
    return slash ? g_strndup(server_url, (gsize)(slash - server_url))
                 : g_strdup(server_url);
}

// With the server off there is no address, and the id shown is copied instead
// — the name of its ~/.pob/<instance>/ directory, and what `pob show <id>`
// takes.
static void on_instance_id_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    gchar *dashboard = dashboard_url();
    const char *text = dashboard ? dashboard : settings_instance_id();
    GtkClipboard *cb = gtk_clipboard_get(GDK_SELECTION_CLIPBOARD);
    gtk_clipboard_set_text(cb, text, -1);
    gtk_clipboard_store(cb);
    gchar *msg = g_strdup_printf("Copied %s", text);
    content_view_show_message(msg);
    g_free(msg);
    g_free(dashboard);
}

// The tooltip says what a click would put on the clipboard, so it follows the
// server up and down.
static void sync_instance_id_tooltip(void) {
    if (!instance_id_btn) return;
    gchar *dashboard = dashboard_url();
    gchar *tip = dashboard
        ? g_strdup_printf("Instance %s — click to copy %s",
                          settings_instance_id(), dashboard)
        : g_strdup_printf("Instance %s — click to copy", settings_instance_id());
    gtk_widget_set_tooltip_text(instance_id_btn, tip);
    g_free(tip);
    g_free(dashboard);
}

void app_set_server_url(const char *url) {
    g_free(server_url);
    server_url = (url && *url) ? g_strdup(url) : NULL;
    sync_instance_id_tooltip();
}

static GtkWidget *build_instance_id_button(void) {
    GtkWidget *btn = gtk_button_new_with_label(settings_instance_id());
    gtk_style_context_add_class(gtk_widget_get_style_context(btn),
                                "pob-instance-id");
    gtk_widget_set_valign(btn, GTK_ALIGN_CENTER);
    instance_id_btn = btn;
    sync_instance_id_tooltip();
    g_signal_connect(btn, "clicked", G_CALLBACK(on_instance_id_clicked), NULL);
    return btn;
}

// Custom window controls: GTK's own "titlebutton" widgets take their size
// and glyphs from the desktop theme (PiXflat pads them to ~32px), so instead
// the headerbar shows no theme controls and packs our own icon_button()s —
// same compact style as the toolbar.
static GtkWidget *maximize_btn;

static void on_minimize_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    gtk_window_iconify(g_state.window);
}

static void on_maximize_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    if (gtk_window_is_maximized(g_state.window))
        gtk_window_unmaximize(g_state.window);
    else
        gtk_window_maximize(g_state.window);
}

static void on_close_clicked(GtkButton *b, gpointer d) {
    (void)b; (void)d;
    gtk_window_close(g_state.window);
}

// Keep the maximize button's icon in sync with the real window state (the
// WM can maximize/restore without our button, e.g. by double-clicking).
static gboolean on_window_state(GtkWidget *w, GdkEventWindowState *ev, gpointer d) {
    (void)w; (void)d;
    if (ev->changed_mask & GDK_WINDOW_STATE_MAXIMIZED) {
        gboolean maxed = ev->new_window_state & GDK_WINDOW_STATE_MAXIMIZED;
        set_button_icon(maximize_btn, maxed ? ICONS_RESTORE : ICONS_MAXIMIZE);
        gtk_widget_set_tooltip_text(maximize_btn, maxed ? "Restore" : "Maximize");
    }
    return FALSE;
}

static void build_headerbar(void) {
    GtkWidget *hb = gtk_header_bar_new();
    g_state.headerbar = hb;
    // No theme-drawn window controls — compact replacements are packed below.
    gtk_header_bar_set_show_close_button(GTK_HEADER_BAR(hb), FALSE);
    // Hide the title text, like macOS titleVisibility(.hidden).
    gtk_header_bar_set_custom_title(GTK_HEADER_BAR(hb), gtk_label_new(""));

    GtkWidget *settings_btn = icon_button(ICONS_SETTINGS, "Settings");
    gchar *instance_tip = g_strdup_printf("Instance — ~/.pob/%s",
                                          settings_instance_id());
    GtkWidget *instance_btn = icon_button(ICONS_INSTANCE, instance_tip);
    g_free(instance_tip);
    GtkWidget *logs_btn = icon_button(ICONS_LOGS, "Logs");
    GtkWidget *inslog_btn = build_inslog_button();
    GtkWidget *macro_btn = icon_button(ICONS_MACRO, "Macro PSL");
    g_state.record_btn = icon_button(ICONS_RECORD, "Record Macro");
    g_state.play_btn = icon_button(ICONS_PLAY, "Execute");
    g_state.target_btn = icon_button(ICONS_TARGET, "Target");
    g_state.crop_btn = icon_button(ICONS_CROP, "Crop");
    GtkWidget *screenshot_btn = icon_button(ICONS_SCREENSHOT, "Screenshot");
    g_state.clickthrough_btn = icon_button(ICONS_HAND,
                                           g_state.is_click_through
                                               ? "Click-Through On (click to disable)"
                                               : "Click-Through Off (click to enable)");
    set_clickthrough_icon(); // slashed hand only when starting OFF
    // Re-render once realized so the slash picks up the final theme color.
    g_signal_connect(g_state.clickthrough_btn, "realize",
                     G_CALLBACK(on_clickthrough_realize), NULL);
    g_state.lock_btn = icon_button(ICONS_UNLOCKED, "Window Unlocked (click to lock)");
    GtkWidget *reset_btn = icon_button(ICONS_RESET, "Reset Mouse Position");
    GtkWidget *hidemenu_btn = icon_button(ICONS_HIDE_MENU, "Hide Menu");
    GtkWidget *minimize_btn = icon_button(ICONS_MINIMIZE, "Minimize");
    maximize_btn = icon_button(ICONS_MAXIMIZE, "Maximize");
    GtkWidget *close_btn = icon_button(ICONS_CLOSE, "Close");

    g_signal_connect(settings_btn, "clicked", G_CALLBACK(on_settings_clicked), NULL);
    g_signal_connect(instance_btn, "clicked", G_CALLBACK(on_instance_clicked), NULL);
    g_signal_connect(logs_btn, "clicked", G_CALLBACK(on_logs_clicked), NULL);
    g_signal_connect(inslog_btn, "clicked", G_CALLBACK(on_inslog_clicked), NULL);
    g_signal_connect(macro_btn, "clicked", G_CALLBACK(on_macro_clicked), NULL);
    g_signal_connect(g_state.record_btn, "clicked", G_CALLBACK(on_record_clicked), NULL);
    g_signal_connect(g_state.play_btn, "clicked", G_CALLBACK(on_play_clicked), NULL);
    g_signal_connect(g_state.target_btn, "clicked", G_CALLBACK(on_target_clicked), NULL);
    g_signal_connect(g_state.crop_btn, "clicked", G_CALLBACK(on_crop_clicked), NULL);
    g_signal_connect(screenshot_btn, "clicked", G_CALLBACK(on_screenshot_clicked), NULL);
    g_signal_connect(g_state.clickthrough_btn, "clicked", G_CALLBACK(on_clickthrough_clicked), NULL);
    g_signal_connect(g_state.lock_btn, "clicked", G_CALLBACK(on_lock_clicked), NULL);
    g_signal_connect(reset_btn, "clicked", G_CALLBACK(on_reset_clicked), NULL);
    g_signal_connect(hidemenu_btn, "clicked", G_CALLBACK(on_hidemenu_clicked), NULL);
    g_signal_connect(minimize_btn, "clicked", G_CALLBACK(on_minimize_clicked), NULL);
    g_signal_connect(maximize_btn, "clicked", G_CALLBACK(on_maximize_clicked), NULL);
    g_signal_connect(close_btn, "clicked", G_CALLBACK(on_close_clicked), NULL);
    g_signal_connect(g_state.window, "window-state-event",
                     G_CALLBACK(on_window_state), NULL);

    // App name first, then the instance id — the same spot as the macOS badge,
    // with the name macOS keeps in the menu bar in front of it. pack_start
    // packs left to right, so this list reads leftmost first.
    gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), build_app_name_button());
    gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), build_instance_id_button());

    // Everything else packs right, in the same left-to-right order as the
    // macOS toolbar (file group, then actions) followed by the window
    // controls. pack_end packs right-to-left, so this list is that order
    // reversed: close first ends up rightmost.
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), close_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), maximize_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), minimize_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), hidemenu_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), reset_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), g_state.lock_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), g_state.clickthrough_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), screenshot_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), g_state.crop_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), g_state.target_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), g_state.play_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), g_state.record_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), macro_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), inslog_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), logs_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), instance_btn);
    gtk_header_bar_pack_end(GTK_HEADER_BAR(hb), settings_btn);

    gtk_widget_add_events(hb, GDK_BUTTON_PRESS_MASK);
    g_signal_connect(hb, "button-press-event",
                     G_CALLBACK(on_headerbar_button_press), NULL);
    // Input shape depends on the headerbar geometry — track it.
    g_signal_connect_swapped(hb, "size-allocate",
                             G_CALLBACK(app_update_click_through), NULL);

    gtk_window_set_titlebar(g_state.window, hb);
}

// ── window frame persistence ────────────────────────────────────────────────

// What a brand new instance opens at, before it has a frame of its own.
//
// 1024x768 rather than something smaller, because the frame is the screen as
// far as a macro is concerned: every coordinate a macro holds is a position
// inside it, and a frame that has to be dragged bigger before the first macro
// is recorded is a frame whose coordinates were all written against a size
// nobody meant. It is also exactly the microVM's screen (vm/msb/run.sh), so an
// instance started here is one that fits when the same ~/.pob is copied in.
//
// The same size on the other two shells — macos/Sources/Services/PobInstance.swift
// and win/src/App.xaml.cs.
#define WINDOW_START_WIDTH 1024
#define WINDOW_START_HEIGHT 768

// A saved frame is not always one this screen can hold, so it comes back cut
// down to what the work area can take of it.
//
// ~/.pob travels. The microVM is handed a copy of it whole (vm/msb/run.sh), so
// the frame an instance was left at on a Mac is the frame the guest's Pob opens
// with on its 1024x768 screen — and the lock travels in that copy too, which
// makes it a window a third of which is below the bottom edge and no drag to
// bring it back, since the lock is what holds it at that size (hold_window_size).
static void clamp_frame_to_workarea(int *x, int *y, int *w, int *h) {
    GdkDisplay *display = gdk_display_get_default();
    if (!display) return;
    GdkMonitor *monitor = gdk_display_get_monitor_at_point(display, *x, *y);
    if (!monitor) monitor = gdk_display_get_primary_monitor(display);
    if (!monitor) return;

    GdkRectangle area;
    gdk_monitor_get_workarea(monitor, &area);
    if (area.width <= 0 || area.height <= 0) return;

    int cw = MIN(*w, area.width);
    int ch = MIN(*h, area.height);
    int cx = CLAMP(*x, area.x, area.x + area.width - cw);
    int cy = CLAMP(*y, area.y, area.y + area.height - ch);
    if (cw == *w && ch == *h && cx == *x && cy == *y) return;

    app_logger_log("Saved frame %dx%d+%d+%d does not fit the %dx%d work area — "
                   "opening at %dx%d+%d+%d",
                   *w, *h, *x, *y, area.width, area.height, cw, ch, cx, cy);
    *w = cw;
    *h = ch;
    *x = cx;
    *y = cy;
}

// The starting size, less whatever of it this screen has not got. A default big
// enough to hang off the screen would be a window with its titlebar out of
// reach on the machines that can least afford it.
static void starting_size(int *w, int *h) {
    *w = WINDOW_START_WIDTH;
    *h = WINDOW_START_HEIGHT;

    GdkDisplay *display = gdk_display_get_default();
    if (!display) return;
    GdkMonitor *monitor = gdk_display_get_primary_monitor(display);
    if (!monitor) monitor = gdk_display_get_monitor(display, 0);
    if (!monitor) return;

    GdkRectangle area;
    gdk_monitor_get_workarea(monitor, &area);
    if (area.width <= 0 || area.height <= 0) return;
    *w = MIN(*w, area.width);
    *h = MIN(*h, area.height);
}

static gboolean save_frame_now(gpointer data) {
    (void)data;
    save_frame_timeout = 0;
    // The frame is not saved in fullscreen: it is the screen rather than
    // anything the user placed, and writing it down would bring the next
    // ordinary launch up as a window the size of the display.
    if (g_state.is_fullscreen) return G_SOURCE_REMOVE;
    int x, y, w, h;
    gtk_window_get_position(g_state.window, &x, &y);
    gtk_window_get_size(g_state.window, &w, &h);
    // With the menu hidden the frame is the content and nothing else. What is
    // written down is the frame with its headerbar back on, so the next run —
    // which starts with the toolbar showing — opens the content over the same
    // pixels rather than a headerbar's worth short of them.
    if (g_state.is_menu_hidden) {
        y -= g_state.hidden_bar_height;
        h += g_state.hidden_bar_height;
    }
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
    // First: the window below is carried by the frame's delta, and anything
    // done in front of it only widens the gap between the frame and what it is
    // holding.
    carry_service_window_configured();
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
        ".pob-inslog-label { font-family: monospace; font-size: 7pt; }\n"
        // App name at the leading edge: bare text like the instance id beside
        // it, a touch larger and in the UI font. Same qualified selector, for
        // the same reason — the compact-button rule below would otherwise win.
        "window.pob-window headerbar button.pob-app-name {\n"
        "  font-size: 9pt; font-weight: bold;\n"
        "  min-width: 0; min-height: 0; padding: 2px 4px; margin-right: 2px;\n"
        "  border: none; box-shadow: none;\n"
        "  background-image: none; background-color: transparent;\n"
        "}\n"
        // Instance id, matching the macOS badge: bare text, with the theme's
        // button background taken off it. Selector is qualified past the
        // compact-button rule below, which would otherwise win on specificity.
        "window.pob-window headerbar button.pob-instance-id {\n"
        "  font-family: monospace; font-size: 8pt; font-weight: bold;\n"
        "  min-width: 0; min-height: 0; padding: 2px 6px; margin-right: 4px;\n"
        "  border: none; box-shadow: none;\n"
        "  background-image: none; background-color: transparent;\n"
        "}\n"
        // Compact titlebar: kill the theme's headerbar min-height/padding
        // and the 6px margins it puts on headerbar buttons.
        "window.pob-window headerbar {\n"
        "  min-height: 0; padding: 4px 4px;\n"
        "}\n"
        // Compact toolbar buttons, closer to the macOS unified-compact look.
        "window.pob-window headerbar button {\n"
        "  min-width: 22px; min-height: 22px; padding: 1px; margin: 0;\n"
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

// Asks the compositor not to draw a shadow behind this window.
//
// A drop shadow is drawn as a rectangle *behind* the window, and a compositor
// only takes the window's own pixels out of it when the window is opaque
// enough to hide it anyway. Pob's content area is 20% gray over nothing, so
// there is nothing to hide it: the shadow shows straight through the part of
// the window that is supposed to be a hole, and the app being driven turns
// gray. Measured under xcompmgr -c, its 0.75-black shadow took the desktop's
// own 128 gray down to 57.
//
// picom and compton read this property; xcompmgr does not, and has no
// per-window opt-out at all — there, the shadow has to come off the
// compositor (run it without -c, which still composites the ARGB visual the
// translucent content needs, and only gives up the shadows).
static void disable_compositor_shadow(GdkWindow *gw) {
    if (!gw) return;
    Display *dpy = GDK_DISPLAY_XDISPLAY(gdk_display_get_default());
    Atom prop = XInternAtom(dpy, "_COMPTON_SHADOW", False);
    long value = 0;
    XChangeProperty(dpy, GDK_WINDOW_XID(gw), prop, XA_CARDINAL, 32,
                    PropModeReplace, (unsigned char *)&value, 1);
}

// Logs the facts transparency depends on, so instance.log answers "why is the
// window opaque" without guesswork.
static void on_window_realize(GtkWidget *w, gpointer data) {
    (void)data;
    GdkWindow *gw = gtk_widget_get_window(w);
    int depth = gw ? gdk_visual_get_depth(gdk_window_get_visual(gw)) : 0;
    app_logger_log("Window realized: visual depth=%d (32 = ARGB ok), composited=%d",
                   depth,
                   gdk_screen_is_composited(gtk_widget_get_screen(w)));
    disable_compositor_shadow(gw);
}

static void on_activate(GtkApplication *app, gpointer data) {
    (void)data;
    if (g_state.window) {
        gtk_window_present(g_state.window);
        return;
    }

    // One Pob drives a desktop: there is one pointer and one focused window
    // to drive it with, so a second copy would only fight the first for both.
    // GApplication normally hands a second launch to the running one and this
    // never fires; it is here for the case where it cannot — no session bus,
    // which is ordinary enough in a container.
    if (!settings_claim_instance()) {
        GtkWidget *dialog = gtk_message_dialog_new(
            NULL, GTK_DIALOG_MODAL, GTK_MESSAGE_WARNING, GTK_BUTTONS_OK,
            "Pob is already running");
        gtk_message_dialog_format_secondary_text(
            GTK_MESSAGE_DIALOG(dialog),
            "Only one Pob can run at a time — it drives the desktop, and there "
            "is one pointer to drive it with. Use the window that is already open.");
        gtk_dialog_run(GTK_DIALOG(dialog));
        gtk_widget_destroy(dialog);
        g_application_quit(G_APPLICATION(app));
        return;
    }

    install_css();
    // Before the window exists: this sets the default every window inherits.
    install_app_icon();

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
        app_logger_error("no RGBA visual — the overlay will not be transparent");
    if (!gdk_screen_is_composited(screen))
        app_logger_error("no compositor detected — the overlay will not be transparent "
                         "(on Raspberry Pi OS: raspi-config → Advanced Options → Compositor, "
                         "or install picom)");
    g_signal_connect(screen, "composited-changed",
                     G_CALLBACK(on_composited_changed), NULL);
    g_signal_connect(win, "draw", G_CALLBACK(on_window_draw), NULL);

    // Click-through as this instance was left, read before the headerbar is
    // built from it so its button starts on the icon the state says. It
    // defaults to ON: the overlay sits on top of the app being driven, so
    // passing clicks through is the useful resting state. The headerbar stays
    // interactive (input shape) either way.
    //
    // Fullscreen takes it as read: the window is the whole screen, so an
    // instance last left catching clicks would come back having taken the
    // desktop away with nothing on screen to give it back.
    g_state.is_click_through =
        g_state.is_fullscreen ? TRUE : settings_get_click_through();

    build_headerbar();
    g_state.content = content_view_new();
    gtk_container_add(GTK_CONTAINER(win), g_state.content);
    // Input shape depends on the content geometry too — a vertical-only resize
    // leaves the headerbar allocation untouched.
    g_signal_connect_swapped(g_state.content, "size-allocate",
                             G_CALLBACK(app_update_click_through), NULL);

    gtk_window_set_keep_above(g_state.window, TRUE); // macOS: window.level = .floating

    if (g_state.is_fullscreen) {
        // The window manager's own fullscreen, so the screen is covered whole
        // — panels, taskbars and docks included, which a window merely sized to
        // the screen would still be shown under. GTK3 drops the CSD titlebar
        // for a fullscreen window itself; it is hidden here as well, since that
        // is a promise of the toolkit rather than of every window manager.
        gtk_window_set_decorated(g_state.window, FALSE);
        gtk_widget_set_no_show_all(g_state.headerbar, TRUE);
        gtk_widget_hide(g_state.headerbar);
        gtk_window_fullscreen(g_state.window);
    } else {
        int x, y, w, h;
        if (settings_get_window_frame(&x, &y, &w, &h)) {
            clamp_frame_to_workarea(&x, &y, &w, &h);
            gtk_window_set_default_size(g_state.window, w, h);
            gtk_window_move(g_state.window, x, y);
        } else {
            int w, h;
            starting_size(&w, &h);
            gtk_window_set_default_size(g_state.window, w, h);
            gtk_window_set_position(g_state.window, GTK_WIN_POS_CENTER);
        }

        // The lock comes back with the frame: applied rather than set, since it
        // is what instance.json already says.
        apply_locked(settings_get_window_locked());

        // After the frame is placed, so the first drag is measured from where
        // the window actually starts rather than from wherever GTK first put
        // it. A fullscreen window is never dragged, so nothing is ever carried
        // under it either.
        carry_service_seed();
    }

    g_signal_connect(win, "configure-event", G_CALLBACK(on_configure), NULL);
    g_signal_connect_swapped(win, "realize", G_CALLBACK(app_update_click_through), NULL);
    g_signal_connect(win, "realize", G_CALLBACK(on_window_realize), NULL);

    gtk_widget_show_all(win);
    if (g_state.is_fullscreen) gtk_widget_hide(g_state.headerbar);

    // What the macOS shell checks its Accessibility grant for, in the form this
    // one can fail: input is synthesised through XTest, and an X server without
    // the extension discards all of it without saying so — the virtual cursor
    // still walks to the target, so it looks like a working Pob that does
    // nothing. Nothing to grant here; the fix is the session or the server.
    if (!mouse_service_xtest_available()) {
        app_logger_error("XTest extension not available — clicks and keystrokes will be discarded");
        app_show_alert_dialog(
            "Pob cannot control this desktop",
            "This X server has no XTest extension, and Pob synthesises every click and "
            "keystroke through it — they will be discarded, silently, while the cursor "
            "still moves to where they were aimed.\n\n"
            "Log in to an Xorg session (under Wayland, Pob runs through XWayland and "
            "cannot drive native Wayland windows), and check that libxtst6 is installed.");
    }

    app_logger_event(g_state.is_fullscreen ? "Pob started (fullscreen)" : "Pob started");
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
    // The other half of "Pob started": app.log is the record of the app coming
    // up and going down, so the way out is written too.
    app_logger_event("Pob stopped");
}

// Takes --fullscreen out of the command line and says whether it was there.
//
// Taken out rather than read where it stands: what is left goes to
// g_application_run, which knows only GTK's own options and refuses the rest
// with "Unknown option" before a window is ever built.
static gboolean take_fullscreen_flag(int *argc, char **argv) {
    gboolean found = FALSE;
    int kept = 1;
    for (int i = 1; i < *argc; i++) {
        if (g_strcmp0(argv[i], "--fullscreen") == 0) {
            found = TRUE;
            continue;
        }
        argv[kept++] = argv[i];
    }
    argv[kept] = NULL;
    *argc = kept;
    return found;
}

int main(int argc, char **argv) {
    XInitThreads(); // two X connections: GDK (main) + the mouse worker
    gdk_set_allowed_backends("x11");

    memset(&g_state, 0, sizeof(g_state));
    g_state.is_fullscreen = take_fullscreen_flag(&argc, argv);

    // Unique, not NON_UNIQUE: a second launch should reach the Pob already
    // running and present its window, not start a rival for the pointer.
    // (G_APPLICATION_DEFAULT_FLAGS is the GLib 2.74 name for what
    // G_APPLICATION_FLAGS_NONE meant; the old one is still needed to build on
    // distributions that predate it.)
#if GLIB_CHECK_VERSION(2, 74, 0)
    GApplicationFlags app_flags = G_APPLICATION_DEFAULT_FLAGS;
#else
    GApplicationFlags app_flags = G_APPLICATION_FLAGS_NONE;
#endif
    GtkApplication *app = gtk_application_new("com.gcc3.pob", app_flags);
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
