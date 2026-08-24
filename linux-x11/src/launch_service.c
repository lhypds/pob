// Opens an application and puts its window in the frame — what a macro's
// launch("firefox") asks for.
//
// The overlay is a hole punched over somebody else's desktop, and every
// position a macro holds is a position inside that hole. Which means every one
// of them was written down while some window sat in a particular place under
// the frame, put there by a person, once. A macro that opens the window itself
// and places it where the frame is does not need that person again — and a
// macro that does not need that person is one a schedule can run.
//
// So this is two jobs, and they are one call because the second needs the
// first. Opening is the shell's; placing is _NET_MOVERESIZE_WINDOW, the same
// way carry_service.c moves a window on every drag. What ties them together is
// the process: the window to place is a window of what was just started, and
// nothing but the side that started it knows which that is.
//
// Whose window is whose is the part X does not simply answer. _NET_WM_PID is
// the direct way and most applications set it, but plenty of them fork before
// they open anything — a wrapper script, a launcher stub, a `--new-window` sent
// to a copy already running — so the pid on the window is a descendant of the
// one that was spawned, or is nothing to do with it at all. Hence two searches
// below: down the process tree first, and then by the name the application
// gives its own windows.
//
// The macOS and Windows halves of this live in
// macos/Sources/Services/LaunchService.swift and
// win/src/Services/LaunchService.cs.
#include "launch_service.h"

#include "app.h"
#include "app_logger.h"
#include "core_bridge.h"

#include <X11/Xatom.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <gdk/gdkx.h>
#include <stdio.h>
#include <string.h>
#include <sys/wait.h>

// How long a launch waits for the application to put a window on screen.
//
// An application already running answers on the first poll, and a cold start of
// something large — a browser, an office suite, an IDE — is seconds rather than
// tenths of them on a machine that has other things to do. Twenty is past all
// of that and still short of a macro looking hung: what usually reaches it is
// an application that opened no window at all, which is a thing to be told
// about rather than waited on.
#define LAUNCH_WINDOW_WAIT_MS 20000

// How often the wait looks. A window that has just appeared is worth finding
// within a frame or two of appearing, since the statement under this one is
// about to click into it.
#define LAUNCH_POLL_MS 200

// How long the window is left alone after being placed before it is measured.
// A move is a request to the window manager rather than a thing that has
// happened, and a window read back the instant after asking is a window still
// standing where it was.
#define LAUNCH_SETTLE_MS 200

// How many times the window is asked before the answer is taken for a refusal.
//
// Once was what this used to be, and once is not enough for an application that
// answers a move against the window it had before the last one — see the fit
// rounds in macos/Sources/Services/LaunchService.swift, where Firefox does
// exactly that. A window manager that places windows where it likes is the same
// shape of problem from this side, and the same thing settles both: ask again
// from where the window now actually is. Eight rounds is past every application
// that converges at all, and eight of them twice over — the size and then the
// position alone — is a few seconds against a wait already allowed twenty.
#define LAUNCH_FIT_ATTEMPTS 8

// Windows smaller than this on either side are not what was opened — the splash
// and scratch windows an application puts up while it starts, which appear
// before the real one and would otherwise be what the frame got.
#define LAUNCH_MIN_WINDOW_SIZE 40

// How far the window may end up from what it was asked for and still count as
// fitted. A window manager rounds to its own increments and a scaled display
// rounds again; two pixels is under anything a person could see and over
// everything rounding can do.
#define LAUNCH_FIT_TOLERANCE 2

// How far up the process tree a window's pid is followed looking for the
// process that was spawned. A wrapper script inside a launcher inside a
// snap is three; sixteen is past every arrangement anybody ships, and the bound
// is what stops a /proc that is lying from being walked forever.
#define LAUNCH_MAX_ANCESTRY 16

// _NET_MOVERESIZE_WINDOW data[0]: which of x/y/width/height are being set, and
// who is asking. Gravity goes in the low byte.
#define LAUNCH_MOVERESIZE_X (1L << 8)
#define LAUNCH_MOVERESIZE_Y (1L << 9)
#define LAUNCH_MOVERESIZE_W (1L << 10)
#define LAUNCH_MOVERESIZE_H (1L << 11)

// Source indication: who is asking for a window to be moved, resized, activated
// or taken out of a state. A pager is a program that arranges other people's
// windows, which is exactly what this is.
#define LAUNCH_SOURCE_PAGER_ID 2
#define LAUNCH_SOURCE_PAGER (LAUNCH_SOURCE_PAGER_ID << 12)

// _NET_WM_STATE data[0].
#define LAUNCH_STATE_REMOVE 0

// ── one launch ──────────────────────────────────────────────────────────────

// A launch in progress. One at a time is all a macro can ask for — the
// statement waits for the answer — but the struct is per-request anyway, since
// the core is not the only thing that could ever send one.
typedef struct Launch {
    gchar *id;     // the JSON-RPC id the answer goes back on
    gchar *target; // as it was written in the statement
    gchar *name;   // the application's own name, lowercased: what a WM_CLASS is matched against
    int gap;       // the margin left around the window, in device pixels
    GPid pid;      // the process that was spawned, or 0 once it has been closed
    int reported_pid; // that pid as the answer names it, kept past the closing
    // Whether a window has already been asked to come back from wherever the
    // window manager had put it. Once per launch: coming back is an animation,
    // and asking again every poll would be arguing with it.
    gboolean roused;
    gint64 deadline; // g_get_monotonic_time() microseconds
    Window client;   // the window found, while it settles
    GdkRectangle rect;
    int attempt;              // rounds of the current phase gone by
    gboolean size_given_up;   // whether the rounds have moved on to the position alone
    guint poll_source;
    guint child_source;
} Launch;

static void launch_free(Launch *wait) {
    if (wait->poll_source) g_source_remove(wait->poll_source);
    if (wait->child_source) g_source_remove(wait->child_source);
    if (wait->pid) g_spawn_close_pid(wait->pid);
    g_free(wait->id);
    g_free(wait->target);
    g_free(wait->name);
    g_free(wait);
}

// The two ways a launch ends: with an application in the frame, or with a
// sentence about why it is not. Both free the wait.
static void launch_done(Launch *wait, gboolean fitted, const char *note) {
    core_bridge_respond_launch(wait->id, wait->target, wait->reported_pid, fitted, note);
    launch_free(wait);
}

static void launch_refused(Launch *wait, const char *message) {
    core_bridge_respond_error(wait->id, message);
    launch_free(wait);
}

// ── X helpers ───────────────────────────────────────────────────────────────
//
// The twins of these live in carry_service.c. They are small enough, and
// different enough in what they are asked for, that each module keeps its own
// rather than one of them keeping the other's.

static Display *display(void) {
    GdkDisplay *gdisplay = gdk_display_get_default();
    return gdisplay ? GDK_DISPLAY_XDISPLAY(gdisplay) : NULL;
}

static Atom launch_atom(const char *name) {
    Display *dpy = display();
    return dpy ? XInternAtom(dpy, name, False) : None;
}

// Reads a 32-bit property whole. On success the caller XFree()s *items.
static gboolean read_card32(Window win, Atom prop, Atom type,
                            unsigned long *count, unsigned char **items) {
    Display *dpy = display();
    if (!dpy || prop == None) return FALSE;

    Atom actual_type = None;
    int actual_format = 0;
    unsigned long bytes_after = 0;
    *items = NULL;
    *count = 0;
    if (XGetWindowProperty(dpy, win, prop, 0, 4096, False, type, &actual_type,
                           &actual_format, count, &bytes_after,
                           items) != Success)
        return FALSE;
    if (actual_format != 32 || *count == 0) {
        if (*items) XFree(*items);
        *items = NULL;
        return FALSE;
    }
    return TRUE;
}

// A request to the window manager about somebody else's window. Every one of
// them is a ClientMessage sent to the root window with the redirect mask on,
// which is the door EWMH leaves open for a program that arranges windows.
static void send_to_root(Window win, Atom message, long d0, long d1, long d2,
                         long d3, long d4) {
    Display *dpy = display();
    if (!dpy || message == None) return;

    XEvent event;
    memset(&event, 0, sizeof(event));
    event.xclient.type = ClientMessage;
    event.xclient.window = win;
    event.xclient.message_type = message;
    event.xclient.format = 32;
    event.xclient.data.l[0] = d0;
    event.xclient.data.l[1] = d1;
    event.xclient.data.l[2] = d2;
    event.xclient.data.l[3] = d3;
    event.xclient.data.l[4] = d4;
    XSendEvent(dpy, DefaultRootWindow(dpy), False,
               SubstructureRedirectMask | SubstructureNotifyMask, &event);
}

static gboolean wm_supports(const char *name) {
    Display *dpy = display();
    if (!dpy) return FALSE;

    Atom wanted = launch_atom(name);
    unsigned long count = 0;
    unsigned char *items = NULL;
    if (!read_card32(DefaultRootWindow(dpy), launch_atom("_NET_SUPPORTED"),
                     XA_ATOM, &count, &items))
        return FALSE;

    Atom *atoms = (Atom *)items;
    gboolean supported = FALSE;
    for (unsigned long i = 0; i < count && !supported; i++)
        supported = atoms[i] == wanted;
    XFree(items);
    return supported;
}

// The client's own top-left in root coordinates, and its size — device pixels,
// the space every X coordinate here is in. The decoration the window manager
// draws around it is deliberately left out: the move below places the client by
// static gravity, so the frame's thickness never enters the arithmetic.
static gboolean client_geometry(Window win, GdkRectangle *out) {
    Display *dpy = display();
    if (!dpy) return FALSE;

    XWindowAttributes attrs;
    if (!XGetWindowAttributes(dpy, win, &attrs)) return FALSE;
    if (attrs.map_state != IsViewable) return FALSE;

    Window child = None;
    int root_x = 0, root_y = 0;
    if (!XTranslateCoordinates(dpy, win, DefaultRootWindow(dpy), 0, 0, &root_x,
                               &root_y, &child))
        return FALSE;

    out->x = root_x;
    out->y = root_y;
    out->width = attrs.width;
    out->height = attrs.height;
    return TRUE;
}

// Docks, desktops, menus and the rest of the furniture are managed clients too.
// A window with no type at all is an ordinary one.
static gboolean is_ordinary(Window win) {
    unsigned long count = 0;
    unsigned char *items = NULL;
    if (!read_card32(win, launch_atom("_NET_WM_WINDOW_TYPE"), XA_ATOM, &count,
                     &items))
        return TRUE;

    Atom normal = launch_atom("_NET_WM_WINDOW_TYPE_NORMAL");
    Atom *atoms = (Atom *)items;
    gboolean ok = FALSE;
    for (unsigned long i = 0; i < count && !ok; i++) ok = atoms[i] == normal;
    XFree(items);
    return ok;
}

// ── whose window is this ────────────────────────────────────────────────────

// The parent of a process, out of /proc. Zero when there is no answer — a
// process that has already gone, or a kernel that keeps none of this.
static pid_t parent_of(pid_t pid) {
    gchar *path = g_strdup_printf("/proc/%d/status", (int)pid);
    FILE *f = fopen(path, "r");
    g_free(path);
    if (!f) return 0;

    pid_t parent = 0;
    char line[256];
    while (fgets(line, sizeof(line), f)) {
        int value = 0;
        if (sscanf(line, "PPid:\t%d", &value) == 1) {
            parent = (pid_t)value;
            break;
        }
    }
    fclose(f);
    return parent;
}

// Whether `pid` is the process that was spawned, or came from it.
//
// The tree is walked upwards rather than downwards because upwards is the
// direction /proc answers in one read per level, and because a launcher that
// exits leaves its child reparented to init — at which point the walk stops at
// 1 and says no, which is the honest answer and why there is a second search
// after this one.
static gboolean descends_from(pid_t pid, pid_t ancestor) {
    if (ancestor <= 0) return FALSE;
    for (int i = 0; i < LAUNCH_MAX_ANCESTRY && pid > 1; i++) {
        if (pid == ancestor) return TRUE;
        pid = parent_of(pid);
    }
    return FALSE;
}

static pid_t window_pid(Window win) {
    unsigned long count = 0;
    unsigned char *items = NULL;
    if (!read_card32(win, launch_atom("_NET_WM_PID"), XA_CARDINAL, &count,
                     &items))
        return 0;
    pid_t pid = (pid_t)(*(unsigned long *)items);
    XFree(items);
    return pid;
}

// Whether the application calls its own windows what the statement called it.
//
// WM_CLASS is how an application names itself to the desktop — it is what a
// taskbar groups by and what a `.desktop` file's StartupWMClass points at — so
// for the common case where the command and the application share a name, this
// finds the window a forked launcher left behind with nothing to trace it by.
static gboolean class_matches(Window win, const char *name) {
    Display *dpy = display();
    if (!dpy || !name || !*name) return FALSE;

    XClassHint hint;
    memset(&hint, 0, sizeof(hint));
    if (!XGetClassHint(dpy, win, &hint)) return FALSE;

    gboolean matched = FALSE;
    if (hint.res_name) {
        gchar *lower = g_ascii_strdown(hint.res_name, -1);
        matched = g_str_equal(lower, name);
        g_free(lower);
        XFree(hint.res_name);
    }
    if (hint.res_class) {
        if (!matched) {
            gchar *lower = g_ascii_strdown(hint.res_class, -1);
            matched = g_str_equal(lower, name);
            g_free(lower);
        }
        XFree(hint.res_class);
    }
    return matched;
}

// The application's first window that is worth putting in the frame, or None
// while it has none.
//
// _NET_CLIENT_LIST_STACKING rather than a walk of XQueryTree: the window
// manager's list holds managed clients only, so the menus, tooltips and drag
// icons that come and go over an application — override-redirect windows, every
// one — are never mistaken for the thing that was opened. It runs bottom to
// top, so the search walks it backwards and finds the frontmost first.
static Window window_of(Launch *wait) {
    Display *dpy = display();
    if (!dpy) return None;

    unsigned long count = 0;
    unsigned char *items = NULL;
    if (!read_card32(DefaultRootWindow(dpy),
                     launch_atom("_NET_CLIENT_LIST_STACKING"), XA_WINDOW,
                     &count, &items))
        return None;

    GdkWindow *own_gdk = g_state.window
        ? gtk_widget_get_window(GTK_WIDGET(g_state.window)) : NULL;
    Window own = own_gdk ? GDK_WINDOW_XID(own_gdk) : None;
    Window *stack = (Window *)items;

    // Two passes, best evidence first: a window that says it belongs to the
    // process that was spawned is that process's window, and a window that
    // merely shares a name with the command might be anybody's.
    Window found = None;
    for (int pass = 0; pass < 2 && found == None; pass++) {
        for (unsigned long i = count; i-- > 0;) {
            Window candidate = stack[i];
            if (candidate == own) continue;
            if (!is_ordinary(candidate)) continue;

            gboolean mine = pass == 0
                ? descends_from(window_pid(candidate), wait->pid)
                : class_matches(candidate, wait->name);
            if (!mine) continue;

            // Iconified is a place the window manager puts a window rather
            // than a size it happens to be, and there is nothing to measure or
            // place while it lasts — an application that was minimized and is
            // being launched again is a common enough way to arrive here.
            // Asked once to come back, passed over for now, and found on a
            // later poll: coming back is an animation, and the window is not
            // anybody's to place until it ends.
            GdkRectangle client;
            if (!client_geometry(candidate, &client)) {
                if (!wait->roused) {
                    wait->roused = TRUE;
                    send_to_root(candidate, launch_atom("_NET_ACTIVE_WINDOW"),
                                 LAUNCH_SOURCE_PAGER_ID, CurrentTime, 0, 0, 0);
                }
                continue;
            }
            if (client.width < LAUNCH_MIN_WINDOW_SIZE ||
                client.height < LAUNCH_MIN_WINDOW_SIZE)
                continue;

            found = candidate;
            break;
        }
    }
    XFree(items);
    return found;
}

// ── fitting ─────────────────────────────────────────────────────────────────

// Maximized and full-screen are places the window manager puts a window rather
// than sizes it happens to be, and a window in either will not take a place
// from anybody else. Asked first, because an application closed maximized comes
// back maximized.
static void make_ordinary(Window win) {
    Atom state = launch_atom("_NET_WM_STATE");
    send_to_root(win, state, LAUNCH_STATE_REMOVE,
                 (long)launch_atom("_NET_WM_STATE_FULLSCREEN"), 0,
                 LAUNCH_SOURCE_PAGER_ID, 0);
    send_to_root(win, state, LAUNCH_STATE_REMOVE,
                 (long)launch_atom("_NET_WM_STATE_MAXIMIZED_HORZ"),
                 (long)launch_atom("_NET_WM_STATE_MAXIMIZED_VERT"),
                 LAUNCH_SOURCE_PAGER_ID, 0);
}

// Asks for the window to be at `rect`, with or without the size — a round that
// has given up on the size asks for the corner alone, so that nothing is
// writing a size for the position to be knocked off by.
static void place_client(Window win, const GdkRectangle *rect,
                         gboolean with_size) {
    Display *dpy = display();
    if (!dpy) return;

    if (wm_supports("_NET_MOVERESIZE_WINDOW")) {
        // StaticGravity: x and y are the client window's own top-left, so
        // whatever the window manager draws around it never enters the
        // arithmetic.
        long flags = StaticGravity | LAUNCH_MOVERESIZE_X | LAUNCH_MOVERESIZE_Y |
                     LAUNCH_SOURCE_PAGER;
        if (with_size) flags |= LAUNCH_MOVERESIZE_W | LAUNCH_MOVERESIZE_H;
        // The width and height travel either way: with the flags off they are
        // what the window manager is told to leave alone.
        send_to_root(win, launch_atom("_NET_MOVERESIZE_WINDOW"), flags,
                     rect->x, rect->y, rect->width, rect->height);
        XFlush(dpy);
        return;
    }

    // No EWMH move: a plain ConfigureRequest instead, which a reparenting
    // window manager reads through the window's own gravity — normally
    // north-west, which puts the *frame* where the request points rather than
    // the client. _NET_FRAME_EXTENTS is what that costs, so take it back off
    // the position asked for and the client lands where the path above would
    // have put it. The size is the client's either way, which is what makes
    // the two paths agree about what "fitted" measures.
    long left = 0, top = 0;
    unsigned long count = 0;
    unsigned char *items = NULL;
    if (read_card32(win, launch_atom("_NET_FRAME_EXTENTS"), XA_CARDINAL, &count,
                    &items)) {
        if (count >= 4) {
            long *extents = (long *)items;
            left = extents[0];
            top = extents[2];
        }
        XFree(items);
    }
    if (with_size) {
        int width = rect->width > 0 ? rect->width : 1;
        int height = rect->height > 0 ? rect->height : 1;
        XMoveResizeWindow(dpy, win, rect->x - (int)left, rect->y - (int)top,
                          (unsigned)width, (unsigned)height);
    } else {
        XMoveWindow(dpy, win, rect->x - (int)left, rect->y - (int)top);
    }
    XFlush(dpy);
}

// The frame's content area with the launch gap taken off it — the rect the
// window is actually put in.
//
// The gap needs no scaling: app_content_rect answers in root device pixels,
// which under X11 is also what a screenshot pixel is, so the margin is the
// number it was written as.
//
// A gap is daylight around a window and not a reason to have no window: a frame
// too small to hold one with the whole margin on gets the margin it can afford,
// which keeps at least half of the frame in each direction for the window
// itself.
static gboolean content_rect_less_gap(int gap, GdkRectangle *out) {
    if (!app_content_rect(out)) return FALSE;
    if (gap <= 0) return TRUE;

    int affordable = MIN(out->width, out->height) / 4;
    int margin = CLAMP(gap, 0, MAX(affordable, 0));
    out->x += margin;
    out->y += margin;
    out->width -= 2 * margin;
    out->height -= 2 * margin;
    return TRUE;
}

// ── the wait ────────────────────────────────────────────────────────────────

// Measures the window now that the window manager has had its moment with it,
// and either ends the launch or asks again.
//
// Asking again is the whole of what makes a fit reliable. A move is a request,
// and what comes back from one is not always what was asked for: a window
// manager rounds it, an application answers it against the window it had before
// the request before this one, and either way the window has moved but is not
// where the frame is. There is nothing there to detect and no offset to correct
// for — what settles it is asking again, from where the window now actually is.
static gboolean on_settle(gpointer data) {
    Launch *wait = data;
    wait->poll_source = 0;

    GdkDisplay *gdisplay = gdk_display_get_default();
    // The window can be unmapped or destroyed between being placed and being
    // measured; without the trap the BadWindow that follows would take Pob down
    // with it.
    gdk_x11_display_error_trap_push(gdisplay);

    GdkRectangle now;
    gboolean read = client_geometry(wait->client, &now);
    gdk_x11_display_error_trap_pop_ignored(gdisplay);

    if (!read) {
        launch_done(wait, FALSE, "its window would not say where it ended up");
        return G_SOURCE_REMOVE;
    }

    gboolean placed = ABS(now.x - wait->rect.x) <= LAUNCH_FIT_TOLERANCE &&
                      ABS(now.y - wait->rect.y) <= LAUNCH_FIT_TOLERANCE;
    gboolean sized = ABS(now.width - wait->rect.width) <= LAUNCH_FIT_TOLERANCE &&
                     ABS(now.height - wait->rect.height) <= LAUNCH_FIT_TOLERANCE;

    if (placed && sized) {
        launch_done(wait, TRUE, "");
        return G_SOURCE_REMOVE;
    }
    if (placed && wait->size_given_up) {
        gchar *note = g_strdup_printf("its window would not resize past %d×%d",
                                      now.width, now.height);
        launch_done(wait, TRUE, note);
        g_free(note);
        return G_SOURCE_REMOVE;
    }

    if (++wait->attempt >= LAUNCH_FIT_ATTEMPTS) {
        if (wait->size_given_up) {
            launch_done(wait, FALSE, "its window would not move to the frame");
            return G_SOURCE_REMOVE;
        }
        // The size is the half a window is allowed to refuse: a browser has a
        // width it will not go under, and a frame narrower than that is a frame
        // it cannot fill. What it may not refuse is the corner — every
        // coordinate under the statement is measured from the frame's top-left —
        // so the rounds go on for the position alone.
        wait->size_given_up = TRUE;
        wait->attempt = 0;
    }

    gdk_x11_display_error_trap_push(gdisplay);
    place_client(wait->client, &wait->rect, !wait->size_given_up);
    gdk_x11_display_error_trap_pop_ignored(gdisplay);

    wait->poll_source = g_timeout_add(LAUNCH_SETTLE_MS, on_settle, wait);
    return G_SOURCE_REMOVE;
}

static gboolean on_poll(gpointer data) {
    Launch *wait = data;

    GdkDisplay *gdisplay = gdk_display_get_default();
    gdk_x11_display_error_trap_push(gdisplay);
    Window client = window_of(wait);
    gdk_x11_display_error_trap_pop_ignored(gdisplay);

    if (client == None) {
        if (g_get_monotonic_time() < wait->deadline) return G_SOURCE_CONTINUE;
        wait->poll_source = 0;
        gchar *note = g_strdup_printf(
            "%s put no window on screen within %d seconds", wait->target,
            LAUNCH_WINDOW_WAIT_MS / 1000);
        launch_done(wait, FALSE, note);
        g_free(note);
        return G_SOURCE_REMOVE;
    }

    // The frame is read now rather than when the launch started: a cold start
    // is seconds long, and the frame is a window somebody can pick up and move
    // in that time. What the window is fitted to is where the frame is when it
    // is fitted.
    if (!content_rect_less_gap(wait->gap, &wait->rect)) {
        wait->poll_source = 0;
        launch_done(wait, FALSE, "Pob's own window is not on screen to fit it to");
        return G_SOURCE_REMOVE;
    }

    gdk_x11_display_error_trap_push(gdisplay);
    make_ordinary(client);
    send_to_root(client, launch_atom("_NET_ACTIVE_WINDOW"), LAUNCH_SOURCE_PAGER_ID,
                 CurrentTime, 0, 0, 0);
    place_client(client, &wait->rect, TRUE);
    gdk_x11_display_error_trap_pop_ignored(gdisplay);

    wait->client = client;
    wait->poll_source = g_timeout_add(LAUNCH_SETTLE_MS, on_settle, wait);
    return G_SOURCE_REMOVE;
}

// The spawned process ended. On its own that says nothing — a launcher that
// hands the request to a copy already running and exits is the ordinary way
// several applications start — so a clean exit is left to the wait. A dirty one
// is not: `sh` could not run what it was given, and there will never be a
// window.
static void on_child_exit(GPid pid, gint status, gpointer data) {
    Launch *wait = data;
    wait->child_source = 0;
    g_spawn_close_pid(pid);
    wait->pid = 0;

    if (WIFEXITED(status) && WEXITSTATUS(status) == 0) return;

    gchar *message = g_strdup_printf(
        "%s would not open — the shell could not run it", wait->target);
    launch_refused(wait, message);
    g_free(message);
}

// ── entry point ─────────────────────────────────────────────────────────────

// The application's own name, out of the command that starts it: the first
// word, without its directory, lowercased. `firefox` from `firefox`,
// `gedit` from `/usr/bin/gedit`, `gnome-terminal` from
// `gnome-terminal --maximize`.
static gchar *application_name(const char *target) {
    gchar **words = g_strsplit_set(target, " \t", 2);
    gchar *base = g_path_get_basename(words[0] && *words[0] ? words[0] : target);
    gchar *lower = g_ascii_strdown(base, -1);
    g_free(base);
    g_strfreev(words);
    return lower;
}

void launch_service_handle(const char *id, const char *target, int gap) {
    gchar *wanted = g_strstrip(g_strdup(target ? target : ""));
    if (!*wanted) {
        core_bridge_respond_error(id, "launch was given no application to open");
        g_free(wanted);
        return;
    }

    // `sh -c exec` rather than the command on its own, so what may be written
    // in the statement is what may be typed at a prompt — a name found on PATH,
    // a full path, arguments and the shell's own quoting. `exec` is what keeps
    // the pid worth having: the shell becomes the application instead of
    // waiting on it, so the process that was spawned is the process whose
    // window is being waited for.
    gchar *line = g_strdup_printf("exec %s", wanted);
    gchar *argv[] = {(gchar *)"/bin/sh", (gchar *)"-c", line, NULL};

    GPid pid = 0;
    GError *error = NULL;
    // The child's own output goes nowhere: an application's chatter is not
    // Pob's log, and the terminal Pob was started from is not the application's
    // to write to. Everything above stderr GLib closes for us, so the pipes to
    // the core do not travel into it either.
    gboolean started = g_spawn_async(NULL, argv, NULL,
                                     G_SPAWN_DO_NOT_REAP_CHILD |
                                         G_SPAWN_STDOUT_TO_DEV_NULL |
                                         G_SPAWN_STDERR_TO_DEV_NULL,
                                     NULL, NULL, &pid, &error);
    g_free(line);

    if (!started) {
        gchar *message = g_strdup_printf("%s would not open: %s", wanted,
                                         error ? error->message : "the shell would not start");
        core_bridge_respond_error(id, message);
        g_free(message);
        if (error) g_error_free(error);
        g_free(wanted);
        return;
    }

    app_logger_log("Launch: %s (pid %d)", wanted, (int)pid);

    Launch *wait = g_new0(Launch, 1);
    wait->id = g_strdup(id);
    wait->target = wanted; // taken over, freed with the wait
    wait->name = application_name(wanted);
    wait->gap = gap;
    wait->pid = pid;
    wait->reported_pid = (int)pid;
    wait->deadline = g_get_monotonic_time() + (gint64)LAUNCH_WINDOW_WAIT_MS * 1000;
    wait->child_source = g_child_watch_add(pid, on_child_exit, wait);
    // The first look goes through the timeout like every other one: an
    // application already running is found a fifth of a second later, and the
    // statement is about to sit through the macro's own delay anyway.
    wait->poll_source = g_timeout_add(LAUNCH_POLL_MS, on_poll, wait);
}
