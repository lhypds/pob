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

#include <X11/Xatom.h>
#include <X11/Xlib.h>
#include <gdk/gdkx.h>
#include <string.h>

// How often a held latch is checked for the end of the drag that took it.
#define CARRY_LATCH_POLL_MS 100

// How many of those polls a held button with a frame standing still gives up
// its latch after anyway. Only a stuck button reading gets this far — it is the
// backstop that keeps one bad reading from carrying into the next drag.
#define CARRY_LATCH_IDLE_POLLS 10

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
static guint latch_drag_watch;
static gboolean moved_since_poll;
static int idle_polls;

// Where the frame stood before the configure now being reported, and how big
// it was. A latch is anchored at that position rather than the current one: by
// the time the first move of a drag arrives the frame has already left, and
// anchoring to where it landed would bake that first step in as a permanent
// offset between the frame and what it carries.
static gboolean previous_seeded;
static int previous_x, previous_y, previous_w, previous_h;

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
    GdkDisplay *gdisplay = gdk_display_get_default();
    GdkWindow *gdk_win = gtk_widget_get_window(GTK_WIDGET(g_state.window));
    if (!gdisplay || !gdk_win) return FALSE;

    GdkSeat *seat = gdk_display_get_default_seat(gdisplay);
    GdkDevice *pointer = seat ? gdk_seat_get_pointer(seat) : NULL;
    if (!pointer) return FALSE;

    GdkModifierType mask = 0;
    gdk_window_get_device_position(gdk_win, pointer, NULL, NULL, &mask);
    return (mask & GDK_BUTTON1_MASK) != 0;
}

// ── the latch ───────────────────────────────────────────────────────────────

static void release_latch(void) {
    if (latch_drag_watch) {
        g_source_remove(latch_drag_watch);
        latch_drag_watch = 0;
    }
    latch_held = FALSE;
    latch_count = 0;
    moved_since_poll = FALSE;
    idle_polls = 0;
}

static gboolean on_drag_watch(gpointer data) {
    (void)data;
    if (!pointer_button_down()) goto done;
    idle_polls = moved_since_poll ? 0 : idle_polls + 1;
    moved_since_poll = FALSE;
    if (idle_polls < CARRY_LATCH_IDLE_POLLS) return G_SOURCE_CONTINUE;

done:
    latch_drag_watch = 0; // release_latch must not remove the source firing it
    release_latch();
    return G_SOURCE_REMOVE;
}

// Holds the latch for exactly as long as the drag that took it.
//
// Letting it lapse on a lull instead would quietly change what is being carried
// halfway through a drag: a frame that pauses and then moves on re-runs the
// search and picks up whatever has since come under it, so a slow drag across a
// busy desktop would gather windows as it went. The set is decided once, when
// the frame is picked up.
static void start_drag_watch(void) {
    moved_since_poll = TRUE;
    if (latch_drag_watch) return;
    latch_drag_watch = g_timeout_add(CARRY_LATCH_POLL_MS, on_drag_watch, NULL);
}

// Finds the windows under the frame as the frame stood at the anchor. A latch
// is always taken — an empty one when there is nothing under the frame to carry
// — so the search runs once per drag either way.
static void acquire_latch(int anchor_x, int anchor_y, int x, int y, int scale) {
    latch_held = TRUE;
    latch_count = 0;
    latch_frame_x = anchor_x;
    latch_frame_y = anchor_y;

    GdkRectangle rect;
    if (!app_content_rect(&rect)) return;
    // The search wants the content area where the drag started, not where this
    // first step has already put it: a fast grab can cover half a screen before
    // the first configure arrives, by which time the frame may be over
    // something else entirely.
    rect.x += (anchor_x - x) * scale;
    rect.y += (anchor_y - y) * scale;

    latch_count = windows_under(&rect, latch_windows, CARRY_MAX_WINDOWS);
}

// ── entry points ────────────────────────────────────────────────────────────

gboolean carry_service_is_enabled(void) { return enabled; }

void carry_service_set_enabled(gboolean on) {
    if (enabled == on) return;
    enabled = on;
    // Turning it off mid-drag has to let go of what it was holding, or the rest
    // of that drag would still be carrying it.
    if (!on) release_latch();
}

void carry_service_seed(void) {
    if (!g_state.window) return;
    gtk_window_get_position(g_state.window, &previous_x, &previous_y);
    gtk_window_get_size(g_state.window, &previous_w, &previous_h);
    previous_seeded = TRUE;
    release_latch();
}

void carry_service_window_configured(void) {
    if (!g_state.window) return;

    int x = 0, y = 0, w = 0, h = 0;
    gtk_window_get_position(g_state.window, &x, &y);
    gtk_window_get_size(g_state.window, &w, &h);

    int anchor_x = previous_x, anchor_y = previous_y;
    gboolean resized = previous_seeded && (w != previous_w || h != previous_h);
    gboolean moved = previous_seeded && (x != previous_x || y != previous_y);

    // Kept current whether or not anything is carried: switching Carry on
    // mid-session must not measure its first drag from wherever the frame
    // happened to be when the window was built.
    previous_x = x;
    previous_y = y;
    previous_w = w;
    previous_h = h;
    previous_seeded = TRUE;

    // Dragging the top or left edge moves the position as it resizes, and a
    // resize is not a move: it changes what the frame covers rather than where
    // it sits, and the window below is meant to stay put under it.
    if (!enabled || !moved || resized || !pointer_button_down()) return;

    // GTK reports the frame in logical pixels and X places windows in device
    // ones, so on a scaled display the frame's delta is worth more than its
    // face value by the time it reaches the windows below.
    int scale = gtk_widget_get_scale_factor(GTK_WIDGET(g_state.window));

    GdkDisplay *gdisplay = gdk_display_get_default();
    // A window being carried can be unmapped or destroyed between the search
    // and the move; without the trap the BadWindow that follows would take Pob
    // down with it.
    gdk_x11_display_error_trap_push(gdisplay);

    if (!latch_held) acquire_latch(anchor_x, anchor_y, x, y, scale);
    start_drag_watch();

    int dx = (x - latch_frame_x) * scale;
    int dy = (y - latch_frame_y) * scale;
    for (int i = 0; i < latch_count; i++)
        move_client(latch_windows[i].client, latch_windows[i].x + dx,
                    latch_windows[i].y + dy);

    gdk_x11_display_error_trap_pop_ignored(gdisplay);
}
