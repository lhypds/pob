// Carries the windows Pob frames along when the frame itself is dragged.
//
// The overlay is a hole punched over somebody else's desktop: the content area
// is what a screenshot shows and what every click is aimed through, so how the
// frame sits over what is under it is the whole arrangement. Moving the frame
// normally leaves all of that where it was and the arrangement with it — the
// picture slides off the apps it was framing. Carrying moves everything under
// the frame by the frame's own delta instead, and the scene stays as it was
// set up.
//
// This is what the lock turns on, and half of what the lock means: a locked
// frame keeps its size and keeps what it frames, which together are what let a
// macro's coordinates survive the window being nudged (see main.c's
// app_update_window_lock).
//
// What counts as under the frame is what the frame shows: a window is carried
// when it overlaps the content area at all, the same test that decides whether
// it turns up in a screenshot. A window Pob only shows a corner of is a window
// Pob is framing, which is the ordinary case of a frame parked over part of
// something bigger than itself.
//
// The windows below are found in _NET_CLIENT_LIST_STACKING rather than by
// walking XQueryTree: the WM's list holds managed clients only, so the menus,
// tooltips and drag icons that come and go over an app — override-redirect
// windows, every one — are never mistaken for the things being framed.
#include "carry_service.h"

#include "app.h"
#include "app_logger.h"

#include <X11/Xatom.h>
#include <X11/Xlib.h>
#include <gdk/gdkx.h>
#include <string.h>

// How often the frame is looked at when nothing is being dragged, and how often
// while something is.
//
// The frame has to be *looked at* rather than waited for, and this is the whole
// reason: a reparenting window manager — openbox, and most of them — drags its
// own frame around with the client sitting still inside it, so the client is
// sent no ConfigureNotify for any of it. What it gets is one synthetic configure
// when the button comes up, which is a move reported after the drag it belonged
// to is over. Waiting for events means carrying nothing, on every WM that works
// this way.
//
// So: ten looks a second at rest, which is one XQueryPointer, and forty while a
// drag is being followed, which is what keeps the windows under the frame inside
// a frame's worth of where the frame is.
#define CARRY_IDLE_POLL_MS 100
#define CARRY_DRAG_POLL_MS 25

// Windows smaller than this on either side are not carried — the 1x1 markers
// some apps park on screen, and the rest of the client list's degenerate
// furniture. Anything a person could actually see inside the frame clears it
// comfortably.
#define CARRY_MIN_WINDOW_SIZE 40

// A ceiling on how many windows one drag will carry. Each one costs a round
// trip to the WM per move, and the frame is redrawn as fast as the pointer
// moves while it is dragged — past some number of windows the drag itself would
// start to stutter. A frame would have to be parked over most of a full desktop
// to reach this.
#define CARRY_MAX_WINDOWS 16

// _NET_MOVERESIZE_WINDOW data[0]: which of x/y/width/height are being set, and
// who is asking. Gravity goes in the low byte.
#define CARRY_MOVERESIZE_X (1L << 8)
#define CARRY_MOVERESIZE_Y (1L << 9)
#define CARRY_SOURCE_PAGER (2L << 12)

static gboolean enabled;

// ── the latch ───────────────────────────────────────────────────────────────
//
// The windows carried through one drag, and where each of them and the frame
// stood when that drag began. Every move puts each window at
// `its origin + (frame now − frame then)` rather than nudging it by each step's
// delta: a drag is hundreds of configures, and each one rounded by the WM
// would walk the windows away from the frame a fraction at a time.
//
// `latch_count` is zero when the search found nothing to carry. That case is
// latched too — the alternative is walking the whole stacking list again on
// every move of a drag over bare desktop.
typedef struct CarriedWindow {
    Window client;
    int x, y; // device pixels, root coordinates
} CarriedWindow;

static gboolean latch_held;
static CarriedWindow latch_windows[CARRY_MAX_WINDOWS];
static int latch_count;
static int latch_frame_x, latch_frame_y; // logical pixels, as GTK reports

// The poll, and whether it is following a drag right now.
static guint poll_source;
static guint poll_interval;
static gboolean dragging;

// How far the drag being followed has moved what it is carrying, for the line
// written when it ends.
static int carried_dx, carried_dy;

// Where the frame stood at the last look, and how big it was. A latch is
// anchored there rather than at where the frame is now: by the time a move is
// noticed the frame has already left, and anchoring to where it landed would
// bake that first step in as a permanent offset between the frame and what it
// carries.
static gboolean previous_seeded;
static int previous_x, previous_y, previous_w, previous_h;

// Whether a button was holding the frame at the last look.
//
// A drag is a move with a button on it, and the two are not seen at the same
// instant: the window manager places the frame where the pointer left it, and
// the look that first sees the new position can be the one after the button
// came up. A quick drag — a flick of the titlebar, over in a tenth of a second
// — is exactly that shape, and asking only about the button *now* calls it a
// move nobody made and carries nothing.
static gboolean previous_down;

// ── X helpers ───────────────────────────────────────────────────────────────

static Display *display(void) {
    GdkDisplay *gdisplay = gdk_display_get_default();
    return gdisplay ? GDK_DISPLAY_XDISPLAY(gdisplay) : NULL;
}

static Atom carry_atom(const char *name) {
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

// Whether the window manager advertises _NET_MOVERESIZE_WINDOW. Read once:
// the answer is a property of the WM running, and a WM that restarts mid-drag
// is not a case worth a round trip per move.
static gboolean wm_supports_moveresize(void) {
    static gboolean known, supported;
    if (known) return supported;
    known = TRUE;

    Display *dpy = display();
    if (!dpy) return FALSE;
    Atom wanted = carry_atom("_NET_MOVERESIZE_WINDOW");
    unsigned long count = 0;
    unsigned char *items = NULL;
    if (!read_card32(DefaultRootWindow(dpy), carry_atom("_NET_SUPPORTED"),
                     XA_ATOM, &count, &items))
        return FALSE;

    Atom *atoms = (Atom *)items;
    for (unsigned long i = 0; i < count && !supported; i++)
        if (atoms[i] == wanted) supported = TRUE;
    XFree(items);
    return supported;
}

// ── what may be carried ─────────────────────────────────────────────────────

// Docks, desktops and the rest of the furniture are managed clients too, and
// the panel along an edge is exactly the sort of thing a frame parked near one
// would sit over. A window with no type at all is an ordinary one.
static gboolean has_carriable_type(Window win) {
    unsigned long count = 0;
    unsigned char *items = NULL;
    if (!read_card32(win, carry_atom("_NET_WM_WINDOW_TYPE"), XA_ATOM, &count,
                     &items))
        return TRUE;

    Atom normal = carry_atom("_NET_WM_WINDOW_TYPE_NORMAL");
    Atom dialog = carry_atom("_NET_WM_WINDOW_TYPE_DIALOG");
    Atom utility = carry_atom("_NET_WM_WINDOW_TYPE_UTILITY");
    Atom *atoms = (Atom *)items;
    gboolean ok = FALSE;
    for (unsigned long i = 0; i < count && !ok; i++)
        ok = atoms[i] == normal || atoms[i] == dialog || atoms[i] == utility;
    XFree(items);
    return ok;
}

// Full-screen and maximized windows are placed by the window manager, not by
// whoever asks. Some WMs ignore a move; others honour it by breaking the state
// the user put the window in. Neither is what Carry is for, so they are passed
// over and the frame moves alone.
static gboolean is_wm_placed(Window win) {
    unsigned long count = 0;
    unsigned char *items = NULL;
    if (!read_card32(win, carry_atom("_NET_WM_STATE"), XA_ATOM, &count, &items))
        return FALSE;

    Atom fullscreen = carry_atom("_NET_WM_STATE_FULLSCREEN");
    Atom max_horz = carry_atom("_NET_WM_STATE_MAXIMIZED_HORZ");
    Atom max_vert = carry_atom("_NET_WM_STATE_MAXIMIZED_VERT");
    Atom *atoms = (Atom *)items;
    gboolean placed = FALSE;
    for (unsigned long i = 0; i < count && !placed; i++)
        placed = atoms[i] == fullscreen || atoms[i] == max_horz ||
                 atoms[i] == max_vert;
    XFree(items);
    return placed;
}

// The client's own top-left in root coordinates, and its size — device pixels,
// the space every X coordinate here is in. The decoration the WM draws around
// it is deliberately left out: the move below places the client by static
// gravity, so the frame's thickness never enters the arithmetic.
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

// ── finding the window below ────────────────────────────────────────────────

// Pob's own toplevel, as the WM lists it.
static Window own_window(void) {
    GdkWindow *gdk_win = gtk_widget_get_window(GTK_WIDGET(g_state.window));
    return gdk_win ? GDK_WINDOW_XID(gdk_win) : None;
}

// Every ordinary window overlapping `rect` that may be moved, and where each of
// them stands, written into `out`. _NET_CLIENT_LIST_STACKING runs bottom to
// top, so the search walks it backwards and `out` comes back front to back.
//
// A window that may not be moved is passed over rather than ending the search:
// it is one window in the frame staying behind, not a reason to leave the rest
// of them behind with it.
static int windows_under(const GdkRectangle *rect, CarriedWindow *out, int max) {
    Display *dpy = display();
    if (!dpy) return 0;

    unsigned long count = 0;
    unsigned char *items = NULL;
    if (!read_card32(DefaultRootWindow(dpy),
                     carry_atom("_NET_CLIENT_LIST_STACKING"), XA_WINDOW, &count,
                     &items))
        return 0;

    Window *stack = (Window *)items;
    Window own = own_window();
    int found = 0;
    for (unsigned long i = count; i-- > 0 && found < max;) {
        Window candidate = stack[i];
        if (candidate == own) continue;

        GdkRectangle client, unused;
        if (!client_geometry(candidate, &client)) continue;
        if (client.width < CARRY_MIN_WINDOW_SIZE ||
            client.height < CARRY_MIN_WINDOW_SIZE)
            continue;
        if (!gdk_rectangle_intersect(&client, rect, &unused)) continue;
        if (!has_carriable_type(candidate) || is_wm_placed(candidate)) continue;

        out[found].client = candidate;
        out[found].x = client.x;
        out[found].y = client.y;
        found++;
    }
    XFree(items);
    return found;
}

// ── moving ──────────────────────────────────────────────────────────────────

static void move_client(Window win, int x, int y) {
    Display *dpy = display();
    if (!dpy) return;
    Window root = DefaultRootWindow(dpy);

    if (wm_supports_moveresize()) {
        // StaticGravity: x and y are the client window's own top-left, so
        // whatever the WM draws around it never enters the arithmetic.
        XEvent event;
        memset(&event, 0, sizeof(event));
        event.xclient.type = ClientMessage;
        event.xclient.window = win;
        event.xclient.message_type = carry_atom("_NET_MOVERESIZE_WINDOW");
        event.xclient.format = 32;
        event.xclient.data.l[0] = StaticGravity | CARRY_MOVERESIZE_X |
                                  CARRY_MOVERESIZE_Y | CARRY_SOURCE_PAGER;
        event.xclient.data.l[1] = x;
        event.xclient.data.l[2] = y;
        XSendEvent(dpy, root, False,
                   SubstructureRedirectMask | SubstructureNotifyMask, &event);
        XFlush(dpy);
        return;
    }

    // No EWMH move: a plain ConfigureRequest instead, which a reparenting WM
    // reads through the window's own gravity — normally north-west, which puts
    // the *frame* where the request points. _NET_FRAME_EXTENTS is what that
    // costs, so take it back off the position asked for.
    long left = 0, top = 0;
    unsigned long count = 0;
    unsigned char *items = NULL;
    if (read_card32(win, carry_atom("_NET_FRAME_EXTENTS"), XA_CARDINAL, &count,
                    &items)) {
        if (count >= 4) {
            long *extents = (long *)items;
            left = extents[0];
            top = extents[2];
        }
        XFree(items);
    }
    XMoveWindow(dpy, win, x - (int)left, y - (int)top);
    XFlush(dpy);
}

// Carry follows drags, hence the held button. A window the WM places on its own
// — mapping one at startup, putting one back on screen when a monitor goes
// away, undoing a maximize — moves with nobody holding it, and dragging some
// app along with that is nobody's intent.
static gboolean pointer_button_down(void) {
    Display *dpy = display();
    if (!dpy) return FALSE;

    Window root = DefaultRootWindow(dpy), child = None;
    int root_x = 0, root_y = 0, win_x = 0, win_y = 0;
    unsigned int mask = 0;
    if (!XQueryPointer(dpy, root, &root, &child, &root_x, &root_y, &win_x,
                       &win_y, &mask))
        return FALSE;
    return (mask & Button1Mask) != 0;
}

// ── the latch ───────────────────────────────────────────────────────────────

static void release_latch(void) {
    // The other half of the pair logged when the latch was taken, and the one
    // that says what actually happened: a drag ends here however it ended — the
    // button coming up, a resize, the lock being turned off mid-drag.
    if (dragging && latch_count > 0)
        app_logger_log("Carry: carried %d window%s %+d,%+d with the frame",
                       latch_count, latch_count == 1 ? "" : "s", carried_dx,
                       carried_dy);
    latch_held = FALSE;
    latch_count = 0;
    dragging = FALSE;
    carried_dx = 0;
    carried_dy = 0;
}

// Finds the windows under the frame as the frame stood at the anchor. A latch
// is always taken — an empty one when there is nothing under the frame to carry
// — so the search runs once per drag either way.
//
// The set is decided once, when the frame is picked up, and held for the whole
// drag. Re-running the search as the frame travelled would quietly change what
// is being carried halfway through: a slow drag across a busy desktop would
// gather windows as it went.
static void acquire_latch(int anchor_x, int anchor_y, int x, int y, int scale) {
    latch_held = TRUE;
    latch_count = 0;
    latch_frame_x = anchor_x;
    latch_frame_y = anchor_y;

    GdkRectangle rect;
    if (!app_content_rect(&rect)) return;
    // The search wants the content area where the drag started, not where this
    // first step has already put it: a fast grab can cover half a screen
    // between two looks, by which time the frame may be over something else
    // entirely.
    rect.x += (anchor_x - x) * scale;
    rect.y += (anchor_y - y) * scale;

    latch_count = windows_under(&rect, latch_windows, CARRY_MAX_WINDOWS);
}

// ── following the frame ─────────────────────────────────────────────────────

// Where the frame is *now*, in the logical pixels GTK counts in, asked of the X
// server rather than of GTK.
//
// gtk_window_get_position answers from what the window has been told, and
// through a WM-driven drag it is told nothing until the end (see the poll
// constants above). gdk_window_get_origin is an XTranslateCoordinates — one
// round trip, and the truth while the drag is happening.
static gboolean frame_origin(int *x, int *y) {
    GtkWidget *win = GTK_WIDGET(g_state.window);
    GdkWindow *gdk_win = win ? gtk_widget_get_window(win) : NULL;
    if (!gdk_win) return FALSE;
    gdk_window_get_origin(gdk_win, x, y);
    return TRUE;
}

// One look at the frame: has it moved, is a button holding it, and if both then
// everything under it goes along by the same amount.
//
// Called from the poll and from a configure event alike — a WM that does report
// a drag as it happens gets its report acted on at once rather than at the next
// tick, and one that does not is no worse off.
static void follow_frame(void) {
    if (!enabled || !g_state.window) return;

    int x = 0, y = 0;
    if (!frame_origin(&x, &y)) return;
    int w = 0, h = 0;
    gtk_window_get_size(g_state.window, &w, &h);

    gboolean seeded = previous_seeded;
    int anchor_x = previous_x, anchor_y = previous_y;
    gboolean resized = seeded && (w != previous_w || h != previous_h);
    gboolean moved = seeded && (x != previous_x || y != previous_y);

    previous_x = x;
    previous_y = y;
    previous_w = w;
    previous_h = h;
    previous_seeded = TRUE;

    gboolean down = pointer_button_down();
    gboolean was_down = previous_down;
    previous_down = down;

    // A resize is not a move: it changes what the frame covers rather than
    // where it sits, and the windows below are meant to stay put under it.
    // Dragging a top or left edge moves the origin as it resizes, which is the
    // one case where both look true at once.
    if (resized) {
        release_latch();
        return;
    }

    if (!dragging) {
        // Nothing is being carried yet, so this is only a drag once the frame
        // has actually moved with a button held. A window the WM places on its
        // own — mapping one at startup, putting one back when a monitor goes
        // away, undoing a maximize — moves with nobody holding it, and dragging
        // some app along with that is nobody's intent.
        //
        // "Held" reaches one look back, which is what makes a quick drag one:
        // the move and the button coming up land in the same tick often enough
        // that requiring the button now would drop every drag short enough not
        // to span two looks.
        if (!moved || !(down || was_down)) return;
        dragging = TRUE;
    } else if (!moved) {
        // A drag can stand still, and while it does the windows under the frame
        // are already where this frame puts them. Asking the window manager to
        // put them there again forty times a second is traffic for nothing.
        if (!down) release_latch();
        return;
    }

    int scale = gtk_widget_get_scale_factor(GTK_WIDGET(g_state.window));

    GdkDisplay *gdisplay = gdk_display_get_default();
    // A window being carried can be unmapped or destroyed between the search
    // and the move; without the trap the BadWindow that follows would take Pob
    // down with it.
    gdk_x11_display_error_trap_push(gdisplay);

    if (!latch_held) {
        acquire_latch(anchor_x, anchor_y, x, y, scale);
        // Two lines a drag, and they are the two questions a report of this not
        // working turns into: was anything found under the frame, and did it
        // travel. A frame dragged over bare desktop says "nothing", a frame that
        // says it carried a window and did not means the window manager put it
        // back, and no line at all means the drag was never seen as one.
        if (latch_count > 0)
            app_logger_log("Carry: the frame picked up %d window%s under it",
                           latch_count, latch_count == 1 ? "" : "s");
        else
            app_logger_log("Carry: nothing under the frame to carry");
    }

    // GTK reports the frame in logical pixels and X places windows in device
    // ones, so on a scaled display the frame's delta is worth more than its
    // face value by the time it reaches the windows below.
    int dx = (x - latch_frame_x) * scale;
    int dy = (y - latch_frame_y) * scale;
    carried_dx = dx;
    carried_dy = dy;
    for (int i = 0; i < latch_count; i++)
        move_client(latch_windows[i].client, latch_windows[i].x + dx,
                    latch_windows[i].y + dy);

    gdk_x11_display_error_trap_pop_ignored(gdisplay);

    // The button came up: that last move was the end of the drag, and it is
    // carried before the latch goes — the frame's final step is as much part of
    // the drag as the ones with the button still down.
    if (!down) release_latch();
}

static gboolean on_poll(gpointer data) {
    (void)data;
    if (!enabled) {
        poll_source = 0;
        poll_interval = 0;
        return G_SOURCE_REMOVE;
    }
    follow_frame();

    // A drag is followed closely and a resting frame is looked at cheaply, so
    // the interval changes with what is happening. Changing it means a new
    // source: a GLib timeout keeps the period it was made with.
    guint wanted = dragging ? CARRY_DRAG_POLL_MS : CARRY_IDLE_POLL_MS;
    if (wanted != poll_interval) {
        poll_interval = wanted;
        poll_source = g_timeout_add(wanted, on_poll, NULL);
        return G_SOURCE_REMOVE;
    }
    return G_SOURCE_CONTINUE;
}

static void start_polling(void) {
    if (poll_source) return;
    poll_interval = CARRY_IDLE_POLL_MS;
    poll_source = g_timeout_add(poll_interval, on_poll, NULL);
}

static void stop_polling(void) {
    if (!poll_source) return;
    g_source_remove(poll_source);
    poll_source = 0;
    poll_interval = 0;
}

// ── entry points ────────────────────────────────────────────────────────────

gboolean carry_service_is_enabled(void) { return enabled; }

void carry_service_set_enabled(gboolean on) {
    if (enabled == on) return;
    enabled = on;
    if (on) {
        // Where the frame stands now is where its next drag starts from, and
        // the poll is what will notice that drag: nothing else is told about
        // one. Both are only worth having while the lock is on, which is the
        // only time anything is carried.
        //
        // The lock is applied before the window is on screen, and a frame with
        // no X window yet has no position to remember; the first poll takes it.
        if (frame_origin(&previous_x, &previous_y)) {
            gtk_window_get_size(g_state.window, &previous_w, &previous_h);
            previous_seeded = TRUE;
        }
        // Nothing was holding the frame before there was anything watching it.
        // A button remembered from the last time the lock was on would make the
        // first move after this one a drag it never belonged to.
        previous_down = FALSE;
        start_polling();
        return;
    }
    // Turning it off mid-drag has to let go of what it was holding, or the rest
    // of that drag would still be carrying it.
    stop_polling();
    release_latch();
}

void carry_service_seed(void) {
    if (!g_state.window) return;
    if (!frame_origin(&previous_x, &previous_y))
        gtk_window_get_position(g_state.window, &previous_x, &previous_y);
    gtk_window_get_size(g_state.window, &previous_w, &previous_h);
    previous_seeded = TRUE;
    previous_down = FALSE;
    release_latch();
}

void carry_service_window_configured(void) { follow_frame(); }
