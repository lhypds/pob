#include "mouse_service.h"
#include "app_logger.h"
#include "content_view.h"
#include "core_bridge.h"
#include "screenshot_service.h"

#include <X11/XKBlib.h>
#include <X11/Xlib.h>
#include <X11/extensions/XTest.h>
#include <X11/keysym.h>
#include <stdlib.h>
#include <string.h>

// ── virtual cursor state ────────────────────────────────────────────────────

static GMutex pos_mutex;
static double virtual_x = 0;
static double virtual_y = 0;

void mouse_get_virtual_pos(double *x, double *y) {
    g_mutex_lock(&pos_mutex);
    *x = virtual_x;
    *y = virtual_y;
    g_mutex_unlock(&pos_mutex);
}

// clamp_to_window holds a position inside the Pob window. Everything the
// cursor addresses is inside that window — it is what the screenshots show and
// what the clicks are aimed through — so a position outside it can only act on
// something nobody asked for and that no screenshot would ever reveal.
// Relative moves are what make this necessary: a trackpad drag, or a run of
// nudges from a model, adds up in one direction and walks off the edge.
//
// Until a capture context exists there is nothing to clamp to, and the
// position is left as it is.
static void clamp_to_window(double *x, double *y) {
    ShotContext ctx = screenshot_get_context();
    if (!ctx.valid || ctx.width <= 0 || ctx.height <= 0) return;
    // The last pixel is width - 1: a cursor at width would be the first pixel
    // outside the window.
    if (*x < 0) *x = 0;
    if (*y < 0) *y = 0;
    if (*x > ctx.width - 1) *x = ctx.width - 1;
    if (*y > ctx.height - 1) *y = ctx.height - 1;
}

static void set_virtual_pos(double x, double y) {
    clamp_to_window(&x, &y);
    g_mutex_lock(&pos_mutex);
    virtual_x = x;
    virtual_y = y;
    g_mutex_unlock(&pos_mutex);
}

void mouse_reset_cursor(void) {
    set_virtual_pos(20, 20);
    content_view_cursor_target_changed(20, 20);
}

void mouse_move_by(double dx, double dy) {
    g_mutex_lock(&pos_mutex);
    double x = virtual_x + dx, y = virtual_y + dy;
    g_mutex_unlock(&pos_mutex);
    // Clamped outside the lock: reading the capture context takes a lock of
    // its own, and taking the two in one order here and the other order
    // anywhere else is how a deadlock gets built.
    clamp_to_window(&x, &y);
    g_mutex_lock(&pos_mutex);
    virtual_x = x;
    virtual_y = y;
    g_mutex_unlock(&pos_mutex);
    content_view_cursor_target_changed(x, y);
}

// Marshals a display-position update onto the GTK main loop (drag ends on
// the worker thread but the overlay animation is main-thread only).
typedef struct {
    double x, y;
} PosUpdate;

static gboolean notify_display_pos(gpointer data) {
    PosUpdate *u = data;
    content_view_cursor_target_changed(u->x, u->y);
    g_free(u);
    return G_SOURCE_REMOVE;
}

static void post_display_pos(double x, double y) {
    PosUpdate *u = g_new(PosUpdate, 1);
    u->x = x;
    u->y = y;
    g_main_context_invoke(NULL, notify_display_pos, u);
}

// ── worker thread plumbing ──────────────────────────────────────────────────

typedef struct {
    MouseJobType type;
    char *id;
    double dx, dy;
    char *text;
} MouseJob;

static GAsyncQueue *job_queue = NULL;
static GThread *worker = NULL;

void mouse_enqueue_job(MouseJobType type, const char *id, double dx, double dy, const char *text) {
    MouseJob *job = g_new0(MouseJob, 1);
    job->type = type;
    job->id = g_strdup(id);
    job->dx = dx;
    job->dy = dy;
    job->text = g_strdup(text ? text : "");
    g_async_queue_push(job_queue, job);
}

static void job_free(MouseJob *job) {
    g_free(job->id);
    g_free(job->text);
    g_free(job);
}

// ── X11 primitives (worker thread, own Display connection) ─────────────────

// Converts the virtual cursor (screenshot pixels) to root-window coordinates
// using the most recent capture context. Returns FALSE when no screenshot
// has been taken yet — the action is skipped, matching the macOS shell.
static gboolean to_root(double px, double py, int *rx, int *ry) {
    ShotContext ctx = screenshot_get_context();
    if (!ctx.valid) return FALSE;
    *rx = ctx.origin_x + (int)px;
    *ry = ctx.origin_y + (int)py;
    return TRUE;
}

static void save_pointer(Display *dpy, int *x, int *y) {
    Window root_ret, child_ret;
    int wx, wy;
    unsigned int mask;
    XQueryPointer(dpy, DefaultRootWindow(dpy), &root_ret, &child_ret, x, y, &wx, &wy, &mask);
}

static void restore_pointer(Display *dpy, int x, int y) {
    XWarpPointer(dpy, None, DefaultRootWindow(dpy), 0, 0, 0, 0, x, y);
    XSync(dpy, False);
}

static void fake_button(Display *dpy, unsigned int button, Bool press) {
    XTestFakeButtonEvent(dpy, button, press, CurrentTime);
    XSync(dpy, False);
}

static void move_pointer(Display *dpy, int x, int y) {
    XTestFakeMotionEvent(dpy, -1, x, y, CurrentTime);
    XSync(dpy, False);
}

static void do_click(Display *dpy, unsigned int button) {
    int rx, ry;
    double px, py;
    mouse_get_virtual_pos(&px, &py);
    if (!to_root(px, py, &rx, &ry)) return;

    int sx, sy;
    save_pointer(dpy, &sx, &sy);
    move_pointer(dpy, rx, ry);
    fake_button(dpy, button, True);
    g_usleep(50 * 1000); // match macOS: 50 ms between down and up
    fake_button(dpy, button, False);
    restore_pointer(dpy, sx, sy);
}

static void do_double_click(Display *dpy) {
    int rx, ry;
    double px, py;
    mouse_get_virtual_pos(&px, &py);
    if (!to_root(px, py, &rx, &ry)) return;

    int sx, sy;
    save_pointer(dpy, &sx, &sy);
    move_pointer(dpy, rx, ry);
    for (int i = 0; i < 2; i++) {
        fake_button(dpy, Button1, True);
        g_usleep(30 * 1000);
        fake_button(dpy, Button1, False);
        if (i == 0) g_usleep(50 * 1000);
    }
    restore_pointer(dpy, sx, sy);
}

static void do_drag(Display *dpy, double dx, double dy) {
    double px, py;
    mouse_get_virtual_pos(&px, &py);
    double end_x = px + dx, end_y = py + dy;

    int rx, ry, ex, ey;
    if (to_root(px, py, &rx, &ry) && to_root(end_x, end_y, &ex, &ey)) {
        int sx, sy;
        save_pointer(dpy, &sx, &sy);
        move_pointer(dpy, rx, ry);
        fake_button(dpy, Button1, True);
        g_usleep(50 * 1000);
        const int steps = 20; // match macOS: 20 interpolated moves, ~16 ms apart
        for (int i = 1; i <= steps; i++) {
            double t = (double)i / steps;
            move_pointer(dpy, rx + (int)((ex - rx) * t), ry + (int)((ey - ry) * t));
            // Keep the overlay arrow tracking the real pointer so the two
            // don't show as separate cursors during the drag.
            post_display_pos(px + (end_x - px) * t, py + (end_y - py) * t);
            g_usleep(16 * 1000);
        }
        fake_button(dpy, Button1, False);
        restore_pointer(dpy, sx, sy);
    }

    set_virtual_pos(end_x, end_y);
    // Read it back rather than reusing end_x/end_y: a drag ending past the
    // window edge is held at the edge, and the drawn cursor has to agree with
    // where the cursor actually is.
    mouse_get_virtual_pos(&end_x, &end_y);
    post_display_pos(end_x, end_y);
}

static void do_scroll(Display *dpy, double dx, double dy) {
    int rx, ry;
    double px, py;
    mouse_get_virtual_pos(&px, &py);
    if (!to_root(px, py, &rx, &ry)) return;

    int sx, sy;
    save_pointer(dpy, &sx, &sy);
    move_pointer(dpy, rx, ry);

    // X11 scrolls in wheel notches; ~40 px per notch approximates the macOS
    // pixel-unit scroll amounts.
    int v_clicks = (int)(ABS(dy) / 40.0);
    int h_clicks = (int)(ABS(dx) / 40.0);
    if (dy != 0 && v_clicks < 1) v_clicks = 1;
    if (dx != 0 && h_clicks < 1) h_clicks = 1;

    unsigned int v_button = dy > 0 ? Button5 : Button4; // dy > 0 = scroll down
    unsigned int h_button = dx > 0 ? 7 : 6;             // dx > 0 = scroll right

    for (int i = 0; i < v_clicks; i++) {
        fake_button(dpy, v_button, True);
        fake_button(dpy, v_button, False);
        g_usleep(10 * 1000);
    }
    for (int i = 0; i < h_clicks; i++) {
        fake_button(dpy, h_button, True);
        fake_button(dpy, h_button, False);
        g_usleep(10 * 1000);
    }
    restore_pointer(dpy, sx, sy);
}

// ── keyboard synthesis ──────────────────────────────────────────────────────

// Finds a keycode with no keysyms bound, used as a scratch slot for typing
// characters that have no key on the current layout (CJK etc.) — the same
// technique xdotool uses.
static int find_spare_keycode(Display *dpy) {
    static int cached = 0;
    if (cached) return cached;

    int min_kc, max_kc;
    XDisplayKeycodes(dpy, &min_kc, &max_kc);
    int syms_per;
    KeySym *map = XGetKeyboardMapping(dpy, min_kc, max_kc - min_kc + 1, &syms_per);
    if (!map) return 0;
    for (int kc = max_kc; kc >= min_kc; kc--) {
        gboolean empty = TRUE;
        for (int i = 0; i < syms_per; i++) {
            if (map[(kc - min_kc) * syms_per + i] != NoSymbol) {
                empty = FALSE;
                break;
            }
        }
        if (empty) {
            cached = kc;
            break;
        }
    }
    XFree(map);
    return cached;
}

static void fake_key(Display *dpy, KeyCode kc, Bool press) {
    XTestFakeKeyEvent(dpy, kc, press, CurrentTime);
    XSync(dpy, False);
}

static void tap_key(Display *dpy, KeyCode kc, gboolean shift) {
    KeyCode shift_kc = XKeysymToKeycode(dpy, XK_Shift_L);
    if (shift) fake_key(dpy, shift_kc, True);
    fake_key(dpy, kc, True);
    fake_key(dpy, kc, False);
    if (shift) fake_key(dpy, shift_kc, False);
}

static KeySym keysym_for_unichar(gunichar ch) {
    // Latin-1 maps directly; everything else uses the X11 Unicode range.
    if (ch < 0x100) return (KeySym)ch;
    return (KeySym)(ch | 0x01000000);
}

static void do_type(Display *dpy, const char *text) {
    if (!g_utf8_validate(text, -1, NULL)) {
        app_logger_log("typeText: invalid UTF-8");
        return;
    }

    int spare = find_spare_keycode(dpy);
    gboolean used_spare = FALSE;

    for (const char *p = text; *p; p = g_utf8_next_char(p)) {
        gunichar ch = g_utf8_get_char(p);
        if (ch == '\n') {
            tap_key(dpy, XKeysymToKeycode(dpy, XK_Return), FALSE);
            g_usleep(12 * 1000);
            continue;
        }

        KeySym ks = keysym_for_unichar(ch);
        KeyCode kc = XKeysymToKeycode(dpy, ks);

        if (kc != 0) {
            // The layout has this character; figure out whether it needs Shift.
            gboolean shift = FALSE;
            if (XkbKeycodeToKeysym(dpy, kc, 0, 0) != ks &&
                XkbKeycodeToKeysym(dpy, kc, 0, 1) == ks)
                shift = TRUE;
            tap_key(dpy, kc, shift);
        } else if (spare != 0) {
            // Temporarily bind the character to the scratch keycode.
            KeySym syms[1] = {ks};
            XChangeKeyboardMapping(dpy, spare, 1, syms, 1);
            XSync(dpy, False);
            used_spare = TRUE;
            tap_key(dpy, (KeyCode)spare, FALSE);
        } else {
            app_logger_log("typeText: no keycode available for U+%04X", ch);
        }
        g_usleep(12 * 1000);
    }

    if (used_spare) {
        KeySym none[1] = {NoSymbol};
        XChangeKeyboardMapping(dpy, spare, 1, none, 1);
        XSync(dpy, False);
    }
}

// Key names accepted by the core's keyPress tool. A name is a *position* on
// the board rather than a character — "slash" is wherever the layout puts it —
// so the active layout decides what gets typed, which is what lets a keyboard
// elsewhere forward keys verbatim.
static gboolean resolve_key_name(const char *name, KeySym *ks) {
    static const struct {
        const char *name;
        KeySym sym;
    } plain[] = {
        {"return", XK_Return}, {"enter", XK_Return},
        {"tab", XK_Tab},       {"space", XK_space},
        {"delete", XK_BackSpace}, {"backspace", XK_BackSpace},
        {"forwarddelete", XK_Delete}, {"insert", XK_Insert},
        {"escape", XK_Escape}, {"esc", XK_Escape},
        {"left", XK_Left},     {"right", XK_Right},
        {"down", XK_Down},     {"up", XK_Up},
        {"home", XK_Home},     {"end", XK_End},
        {"pageup", XK_Prior},  {"pagedown", XK_Next},
        {"capslock", XK_Caps_Lock}, {"printscreen", XK_Print},
        {"scrolllock", XK_Scroll_Lock}, {"pause", XK_Pause},
        {"menu", XK_Menu},
        {"minus", XK_minus},   {"equals", XK_equal},
        {"leftbracket", XK_bracketleft}, {"rightbracket", XK_bracketright},
        {"backslash", XK_backslash}, {"semicolon", XK_semicolon},
        {"quote", XK_apostrophe}, {"grave", XK_grave},
        {"comma", XK_comma},   {"period", XK_period}, {"slash", XK_slash},
    };

    for (gsize i = 0; i < G_N_ELEMENTS(plain); i++) {
        if (g_str_equal(name, plain[i].name)) {
            *ks = plain[i].sym;
            return TRUE;
        }
    }
    if (strlen(name) == 1 && name[0] >= 'a' && name[0] <= 'z') {
        *ks = (KeySym)(XK_a + (name[0] - 'a'));
        return TRUE;
    }
    if (strlen(name) == 1 && name[0] >= '0' && name[0] <= '9') {
        *ks = (KeySym)(XK_0 + (name[0] - '0'));
        return TRUE;
    }
    if (name[0] == 'f' && name[1] != '\0') {
        char *end = NULL;
        long n = strtol(name + 1, &end, 10);
        // XK_F1..XK_F24 run consecutively, so the number is enough.
        if (end && *end == '\0' && n >= 1 && n <= 24) {
            *ks = (KeySym)(XK_F1 + (n - 1));
            return TRUE;
        }
    }
    return FALSE;
}

// Modifiers a chord may hold. "cmd" keeps meaning Ctrl here — the Unix
// equivalent of the macOS Command shortcuts, and what a macro or an MCP call
// has always meant by one. "gui" is the other thing you might mean: the
// physical key beside the space bar, which on this machine is Super.
static gboolean resolve_modifier(const char *name, KeySym *ks) {
    static const struct {
        const char *name;
        KeySym sym;
    } mods[] = {
        {"cmd", XK_Control_L},   {"command", XK_Control_L},
        {"ctrl", XK_Control_L},  {"control", XK_Control_L},
        {"alt", XK_Alt_L},       {"option", XK_Alt_L},  {"opt", XK_Alt_L},
        {"shift", XK_Shift_L},
        {"gui", XK_Super_L},     {"win", XK_Super_L},
        {"super", XK_Super_L},   {"meta", XK_Super_L},
    };
    for (gsize i = 0; i < G_N_ELEMENTS(mods); i++) {
        if (g_str_equal(name, mods[i].name)) {
            *ks = mods[i].sym;
            return TRUE;
        }
    }
    return FALSE;
}

// Presses one key, optionally with modifiers held: "escape", "cmd+v",
// "ctrl+alt+shift+f5". Everything before the last "+" is a modifier; the last
// part is the key.
static void do_key_press(Display *dpy, const char *key) {
    gchar *lower = g_ascii_strdown(key, -1);
    gchar **parts = g_strsplit(lower, "+", -1);
    g_free(lower);

    guint count = g_strv_length(parts);
    KeySym ks;
    if (count == 0 || !resolve_key_name(parts[count - 1], &ks)) {
        app_logger_log("Unknown key: %s", key);
        g_strfreev(parts);
        return;
    }

    // Held one per modifier named, in the order given and released in reverse,
    // so a modifier never outlives one held under it.
    KeyCode mod_kc[8];
    guint mod_count = 0;
    for (guint i = 0; i + 1 < count && mod_count < G_N_ELEMENTS(mod_kc); i++) {
        KeySym mod_ks;
        if (!resolve_modifier(parts[i], &mod_ks)) {
            app_logger_log("Unknown modifier in key: %s", key);
            g_strfreev(parts);
            return;
        }
        KeyCode kc = XKeysymToKeycode(dpy, mod_ks);
        if (kc != 0) mod_kc[mod_count++] = kc;
    }
    g_strfreev(parts);

    KeyCode kc = XKeysymToKeycode(dpy, ks);
    if (kc == 0) return;

    for (guint i = 0; i < mod_count; i++) fake_key(dpy, mod_kc[i], True);
    fake_key(dpy, kc, True);
    g_usleep(30 * 1000); // match macOS: 30 ms hold
    fake_key(dpy, kc, False);
    for (guint i = mod_count; i > 0; i--) fake_key(dpy, mod_kc[i - 1], False);
}

// ── worker main ─────────────────────────────────────────────────────────────

static gpointer worker_main(gpointer data) {
    (void)data;
    Display *dpy = XOpenDisplay(NULL);
    if (!dpy) {
        app_logger_log("MouseService: cannot open X display in worker");
    } else {
        int ev, err, major, minor;
        if (!XTestQueryExtension(dpy, &ev, &err, &major, &minor))
            app_logger_log("MouseService: XTest extension not available");
    }

    for (;;) {
        MouseJob *job = g_async_queue_pop(job_queue);
        if (job->type == (MouseJobType)-1) { // shutdown sentinel
            job_free(job);
            break;
        }

        if (dpy) {
            switch (job->type) {
            case MOUSE_JOB_CLICK: do_click(dpy, Button1); break;
            case MOUSE_JOB_RIGHT_CLICK: do_click(dpy, Button3); break;
            case MOUSE_JOB_DOUBLE_CLICK: do_double_click(dpy); break;
            case MOUSE_JOB_DRAG: do_drag(dpy, job->dx, job->dy); break;
            case MOUSE_JOB_SCROLL: do_scroll(dpy, job->dx, job->dy); break;
            case MOUSE_JOB_TYPE: do_type(dpy, job->text); break;
            case MOUSE_JOB_KEY_PRESS: do_key_press(dpy, job->text); break;
            }
        }

        // Mouse actions answer with the (possibly updated) cursor position;
        // keyboard actions answer with an empty result — same as macOS.
        if (job->type == MOUSE_JOB_TYPE || job->type == MOUSE_JOB_KEY_PRESS)
            core_bridge_respond_empty(job->id);
        else
            core_bridge_respond_position(job->id);

        job_free(job);
    }

    if (dpy) XCloseDisplay(dpy);
    return NULL;
}

void mouse_service_init(void) {
    job_queue = g_async_queue_new();
    worker = g_thread_new("pob-mouse-worker", worker_main, NULL);
}

void mouse_service_shutdown(void) {
    if (!worker) return;
    MouseJob *sentinel = g_new0(MouseJob, 1);
    sentinel->type = (MouseJobType)-1;
    sentinel->id = g_strdup("");
    sentinel->text = g_strdup("");
    g_async_queue_push(job_queue, sentinel);
    g_thread_join(worker);
    worker = NULL;
}
