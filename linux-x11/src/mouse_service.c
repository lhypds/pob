#include "mouse_service.h"
#include "app_logger.h"
#include "content_view.h"
#include "core_bridge.h"
#include "screenshot_service.h"

#include <X11/XKBlib.h>
#include <X11/Xlib.h>
#include <X11/extensions/XTest.h>
#include <X11/keysym.h>
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

static void set_virtual_pos(double x, double y) {
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
    virtual_x += dx;
    virtual_y += dy;
    double x = virtual_x, y = virtual_y;
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
            g_usleep(16 * 1000);
        }
        fake_button(dpy, Button1, False);
        restore_pointer(dpy, sx, sy);
    }

    set_virtual_pos(end_x, end_y);
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

// Special-key names accepted by the core's keyPress tool. "cmd+<letter>"
// maps to Ctrl+<letter> — the Unix equivalent of the macOS Command shortcuts.
static gboolean resolve_key(const char *name, KeySym *ks, gboolean *ctrl) {
    static const struct {
        const char *name;
        KeySym sym;
    } plain[] = {
        {"return", XK_Return}, {"enter", XK_Return},
        {"tab", XK_Tab},       {"space", XK_space},
        {"delete", XK_BackSpace}, {"backspace", XK_BackSpace},
        {"escape", XK_Escape}, {"esc", XK_Escape},
        {"left", XK_Left},     {"right", XK_Right},
        {"down", XK_Down},     {"up", XK_Up},
        {"home", XK_Home},     {"end", XK_End},
        {"pageup", XK_Prior},  {"pagedown", XK_Next},
        {"f1", XK_F1},   {"f2", XK_F2},   {"f3", XK_F3},   {"f4", XK_F4},
        {"f5", XK_F5},   {"f6", XK_F6},   {"f7", XK_F7},   {"f8", XK_F8},
        {"f9", XK_F9},   {"f10", XK_F10}, {"f11", XK_F11}, {"f12", XK_F12},
    };

    *ctrl = FALSE;
    for (gsize i = 0; i < G_N_ELEMENTS(plain); i++) {
        if (g_str_equal(name, plain[i].name)) {
            *ks = plain[i].sym;
            return TRUE;
        }
    }
    if (g_str_has_prefix(name, "cmd+") && strlen(name) == 5 &&
        name[4] >= 'a' && name[4] <= 'z') {
        *ks = (KeySym)(XK_a + (name[4] - 'a'));
        *ctrl = TRUE;
        return TRUE;
    }
    return FALSE;
}

static void do_key_press(Display *dpy, const char *key) {
    gchar *lower = g_ascii_strdown(key, -1);
    KeySym ks;
    gboolean ctrl;
    if (!resolve_key(lower, &ks, &ctrl)) {
        app_logger_log("Unknown key: %s", key);
        g_free(lower);
        return;
    }
    g_free(lower);

    KeyCode kc = XKeysymToKeycode(dpy, ks);
    if (kc == 0) return;
    KeyCode ctrl_kc = XKeysymToKeycode(dpy, XK_Control_L);

    if (ctrl) fake_key(dpy, ctrl_kc, True);
    fake_key(dpy, kc, True);
    g_usleep(30 * 1000); // match macOS: 30 ms hold
    fake_key(dpy, kc, False);
    if (ctrl) fake_key(dpy, ctrl_kc, False);
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
