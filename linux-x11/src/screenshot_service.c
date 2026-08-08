#include "screenshot_service.h"
#include "app.h"
#include "app_logger.h"
#include "content_view.h"
#include "core_bridge.h"
#include "frame_channel.h"
#include "mouse_service.h"

#include <X11/Xatom.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <cairo.h>
#include <gdk/gdkx.h>
#include <string.h>

// ── capture context ─────────────────────────────────────────────────────────

static GMutex ctx_mutex;
static ShotContext current_ctx = {FALSE, 0, 0, 0, 0, 1};

ShotContext screenshot_get_context(void) {
    g_mutex_lock(&ctx_mutex);
    ShotContext c = current_ctx;
    g_mutex_unlock(&ctx_mutex);
    return c;
}

static void set_context(int ox, int oy, int w, int h, int scale) {
    g_mutex_lock(&ctx_mutex);
    current_ctx.valid = TRUE;
    current_ctx.origin_x = ox;
    current_ctx.origin_y = oy;
    current_ctx.width = w;
    current_ctx.height = h;
    current_ctx.scale = scale;
    g_mutex_unlock(&ctx_mutex);
}

// ── pending request (core sends one capture at a time) ─────────────────────

typedef struct {
    char *id;
    gboolean with_cursor;
    gboolean has_crop;
    double crop_x, crop_y, crop_w, crop_h;
    char *format;  // "png" (the agent's) or "jpeg" (the view page's)
    int max_width; // shrink to at most this many pixels across; 0 leaves it
    int quality;   // JPEG quality 1-100; 0 takes the default below
} PendingShot;

static PendingShot *pending = NULL;

// ── keeping the window out of the picture ───────────────────────────────────
//
// X11 has no "capture everything below this window" the way macOS does, so
// the window has to be off the screen when the grab happens — and it has to
// go as a *window*, not merely stop drawing, or its compositor drop shadow
// stays lying on the desktop for the grab to read (see set_x_window_opacity).
//
// Doing that per frame is what made the view page's stream strobe, so it is
// done per *burst*: the window goes at the first frame and stays gone while
// frames keep arriving, coming back once they stop. A stream costs one fade,
// not one per frame — and every frame after the first is grabbed with no wait
// at all, which is most of what the old 80 ms per capture was spending.
static gboolean window_hidden = FALSE;
static guint restore_timeout = 0;
static gint64 last_capture_us = 0; // when the previous capture was asked for
static gint64 last_gap_us = 0;     // and how long before it the one before was

// One compositor frame, generously: how long to let the window's disappearance
// reach the screen before grabbing it. Paid once per burst.
#define COMPOSITOR_SETTLE_MS 80

// How long the window stays away after a capture: two frame intervals, so the
// next frame of a stream always arrives before it comes back and nothing
// blinks mid-stream. Frames further apart than STREAM_GAP are not a stream —
// the agent's own screenshots, seconds apart — and bring the window straight
// back, which is what this always did for a single capture.
#define RESTORE_MAX_MS 1500
#define STREAM_GAP_MAX_US (1500 * 1000)

static guint restore_delay_ms(void) {
    if (last_gap_us <= 0 || last_gap_us > STREAM_GAP_MAX_US) return 0;
    gint64 ms = last_gap_us * 2 / 1000;
    return (guint)MIN(ms, RESTORE_MAX_MS);
}

// The opacity the *compositor* reads. gtk_widget_set_opacity() is no use for
// this: on a toplevel with an RGBA visual — which is every Pob window, since
// that is what the translucent content area needs — GTK applies the alpha
// when it draws, and never sets _NET_WM_WINDOW_OPACITY at all. The window's
// own pixels go, and the compositor carries on drawing its drop shadow behind
// them at full strength; the grab then reads the shadow instead of the
// desktop, which is where the desktop's own 128 gray came back as 36 under
// xcompmgr -c. This property is the one that takes the shadow with the
// window, because a compositor scales the one by the other.
static void set_x_window_opacity(gboolean opaque) {
    if (!g_state.window) return;
    GdkWindow *gw = gtk_widget_get_window(GTK_WIDGET(g_state.window));
    if (!gw) return;

    Display *dpy = GDK_DISPLAY_XDISPLAY(gdk_display_get_default());
    Atom prop = XInternAtom(dpy, "_NET_WM_WINDOW_OPACITY", False);
    unsigned long value = opaque ? 0xffffffffUL : 0UL;
    XChangeProperty(dpy, GDK_WINDOW_XID(gw), prop, XA_CARDINAL, 32,
                    PropModeReplace, (unsigned char *)&value, 1);
    XFlush(dpy);
}

static void set_window_hidden(gboolean hidden) {
    window_hidden = hidden;
    set_x_window_opacity(!hidden);
    // Belt and braces for the compositors that ignore the property, and it
    // holds the flash back until there is something to see it on: with no
    // compositor at all there is no shadow either, and drawing nothing is
    // then the only thing keeping the tint out of the shot.
    if (g_state.window)
        gtk_widget_set_opacity(GTK_WIDGET(g_state.window), hidden ? 0.0 : 1.0);
    content_view_set_capture_hidden(hidden);
}

static gboolean on_restore_window(gpointer data) {
    (void)data;
    restore_timeout = 0;
    set_window_hidden(FALSE);
    return G_SOURCE_REMOVE;
}

// Called when a capture is done with the screen, one way or the other.
static void schedule_window_restore(void) {
    if (restore_timeout) g_source_remove(restore_timeout);
    restore_timeout = g_timeout_add(restore_delay_ms(), on_restore_window, NULL);
}

// ── XImage → cairo surface ──────────────────────────────────────────────────

static cairo_surface_t *ximage_to_surface(XImage *img) {
    cairo_surface_t *surface =
        cairo_image_surface_create(CAIRO_FORMAT_RGB24, img->width, img->height);
    if (cairo_surface_status(surface) != CAIRO_STATUS_SUCCESS) return NULL;

    unsigned char *dst = cairo_image_surface_get_data(surface);
    int dst_stride = cairo_image_surface_get_stride(surface);

    if (img->bits_per_pixel == 32 && img->red_mask == 0xff0000 &&
        img->green_mask == 0x00ff00 && img->blue_mask == 0x0000ff) {
        // Common case: 32-bit BGRX little-endian — same memory layout as
        // CAIRO_FORMAT_RGB24, copy row by row.
        for (int y = 0; y < img->height; y++)
            memcpy(dst + y * dst_stride,
                   (unsigned char *)img->data + y * img->bytes_per_line,
                   (size_t)img->width * 4);
    } else {
        // Fallback for unusual visuals: slow but correct.
        for (int y = 0; y < img->height; y++) {
            guint32 *row = (guint32 *)(dst + y * dst_stride);
            for (int x = 0; x < img->width; x++) {
                unsigned long p = XGetPixel(img, x, y);
                guint32 r = (guint32)((p & img->red_mask) * 255 / img->red_mask);
                guint32 g = (guint32)((p & img->green_mask) * 255 / (img->green_mask ? img->green_mask : 1));
                guint32 b = (guint32)((p & img->blue_mask) * 255 / (img->blue_mask ? img->blue_mask : 1));
                row[x] = (r << 16) | (g << 8) | b;
            }
        }
    }
    cairo_surface_mark_dirty(surface);
    return surface;
}

// ── cursor image ────────────────────────────────────────────────────────────

// System arrow cursor as a cairo surface + hotspot; falls back to a drawn
// arrow when the cursor theme is unavailable.
static cairo_surface_t *cursor_surface(double *hot_x, double *hot_y,
                                       double *width, double *height) {
    static cairo_surface_t *cached = NULL;
    static double c_hot_x = 0, c_hot_y = 0, c_w = 0, c_h = 0;

    if (!cached) {
        GdkDisplay *display = gdk_display_get_default();
        GdkCursor *cursor = gdk_cursor_new_from_name(display, "default");
        if (cursor) {
            double hx = 0, hy = 0;
            cairo_surface_t *surf = gdk_cursor_get_surface(cursor, &hx, &hy);
            if (surf && cairo_surface_get_type(surf) == CAIRO_SURFACE_TYPE_IMAGE) {
                cached = surf;
                c_hot_x = hx;
                c_hot_y = hy;
                c_w = cairo_image_surface_get_width(surf);
                c_h = cairo_image_surface_get_height(surf);
            } else if (surf) {
                cairo_surface_destroy(surf);
            }
            g_object_unref(cursor);
        }
    }
    if (!cached) {
        // Hand-drawn classic arrow (hotspot at the tip, top-left).
        int w = 24, h = 36;
        cached = cairo_image_surface_create(CAIRO_FORMAT_ARGB32, w, h);
        cairo_t *cr = cairo_create(cached);
        cairo_move_to(cr, 0, 0);
        cairo_line_to(cr, 0, 26);
        cairo_line_to(cr, 6, 20);
        cairo_line_to(cr, 10, 30);
        cairo_line_to(cr, 14, 28);
        cairo_line_to(cr, 10, 19);
        cairo_line_to(cr, 18, 18);
        cairo_close_path(cr);
        cairo_set_source_rgb(cr, 0, 0, 0);
        cairo_fill_preserve(cr);
        cairo_set_source_rgb(cr, 1, 1, 1);
        cairo_set_line_width(cr, 1.5);
        cairo_stroke(cr);
        cairo_destroy(cr);
        c_hot_x = 0;
        c_hot_y = 0;
        c_w = w;
        c_h = h;
    }

    *hot_x = c_hot_x;
    *hot_y = c_hot_y;
    *width = c_w;
    *height = c_h;
    return cached;
}

// Draws the arrow cursor into the screenshot with its hotspot at (px, py)
// screenshot pixels. 22 × scale is about the size a desktop draws its own
// pointer, and the same apparent size on every density — the macOS and Windows
// shells composite theirs to match, so a frame looks the same wherever the
// view page is watching from.
static void draw_cursor_into(cairo_t *cr, double px, double py, double scale) {
    double hx, hy, cw, ch;
    cairo_surface_t *surf = cursor_surface(&hx, &hy, &cw, &ch);
    if (!surf || ch <= 0) return;

    double target_h = 22.0 * scale;
    double s = target_h / ch;

    cairo_save(cr);
    cairo_translate(cr, px - hx * s, py - hy * s);
    cairo_scale(cr, s, s);
    cairo_set_source_surface(cr, surf, 0, 0);
    cairo_paint(cr);
    cairo_restore(cr);
}

// ── encoding ────────────────────────────────────────────────────────────────

// PNG is the default because it is the agent's format: it reads the text in a
// frame, and a lossy encoder is no way to hand it one. A browser watching the
// machine wants the opposite trade, and asks for JPEG — which is the heavier
// half of what makes a watchable frame rate possible at all, being an order of
// magnitude cheaper to encode and to send.
#define DEFAULT_JPEG_QUALITY 70

static gboolean surface_to_bytes(cairo_surface_t *surface, const char *format,
                                 int quality, guchar **out, gsize *out_len) {
    int w = cairo_image_surface_get_width(surface);
    int h = cairo_image_surface_get_height(surface);
    GdkPixbuf *pixbuf = gdk_pixbuf_get_from_surface(surface, 0, 0, w, h);
    if (!pixbuf) return FALSE;

    gboolean jpeg = format && g_str_equal(format, "jpeg");
    GError *error = NULL;
    gboolean ok;
    if (jpeg) {
        int q = quality > 0 ? CLAMP(quality, 1, 100) : DEFAULT_JPEG_QUALITY;
        gchar *q_text = g_strdup_printf("%d", q);
        ok = gdk_pixbuf_save_to_buffer(pixbuf, (gchar **)out, out_len, "jpeg",
                                       &error, "quality", q_text, NULL);
        g_free(q_text);
    } else {
        ok = gdk_pixbuf_save_to_buffer(pixbuf, (gchar **)out, out_len, "png",
                                       &error, NULL);
    }
    g_object_unref(pixbuf);
    if (!ok) {
        if (error) g_error_free(error);
        return FALSE;
    }
    return TRUE;
}

// ── capture flow ────────────────────────────────────────────────────────────

static void finish_pending(void) {
    // The screen is free again: start the clock on bringing the window back,
    // which the next frame of a stream will push out ahead of itself.
    schedule_window_restore();

    if (!pending) return;
    g_free(pending->id);
    g_free(pending->format);
    g_free(pending);
    pending = NULL;
}

// Answers with a captured frame: down the frame channel if it is up, and as
// base64 on the JSON-RPC line if not — which is what this shell did before the
// channel existed, and what an older core still expects.
static void deliver(const char *id, const guchar *bytes, gsize len, int width,
                    int height, int source_width, int source_height) {
    gchar *meta = g_strdup_printf(
        "{\"width\":%d,\"height\":%d,\"sourceWidth\":%d,\"sourceHeight\":%d}",
        width, height, source_width, source_height);
    if (!frame_channel_send(id, meta, bytes, len)) {
        gchar *b64 = g_base64_encode(bytes, len);
        core_bridge_respond_image(id, b64, meta);
        g_free(b64);
    }
    g_free(meta);
}

static gboolean do_capture(gpointer data) {
    (void)data;
    if (!pending) { // nothing to grab — don't leave the window hidden over it
        schedule_window_restore();
        return G_SOURCE_REMOVE;
    }

    GtkWidget *win = GTK_WIDGET(g_state.window);
    GtkWidget *content = g_state.content;

    // Content-area geometry in root coordinates (logical → device pixels).
    int rel_x = 0, rel_y = 0;
    gtk_widget_translate_coordinates(content, win, 0, 0, &rel_x, &rel_y);
    GdkWindow *gdk_win = gtk_widget_get_window(win);
    int origin_x = 0, origin_y = 0;
    gdk_window_get_origin(gdk_win, &origin_x, &origin_y);
    int scale = gtk_widget_get_scale_factor(win);

    GtkAllocation alloc;
    gtk_widget_get_allocation(content, &alloc);

    int dev_x = (origin_x + rel_x) * scale;
    int dev_y = (origin_y + rel_y) * scale;
    int dev_w = alloc.width * scale;
    int dev_h = alloc.height * scale;

    Display *dpy = GDK_DISPLAY_XDISPLAY(gdk_display_get_default());
    int screen = DefaultScreen(dpy);
    int screen_w = DisplayWidth(dpy, screen);
    int screen_h = DisplayHeight(dpy, screen);

    // Clamp to the root window; XGetImage errors on out-of-bounds rects.
    if (dev_x < 0) { dev_w += dev_x; dev_x = 0; }
    if (dev_y < 0) { dev_h += dev_y; dev_y = 0; }
    if (dev_x + dev_w > screen_w) dev_w = screen_w - dev_x;
    if (dev_y + dev_h > screen_h) dev_h = screen_h - dev_y;

    if (dev_w <= 0 || dev_h <= 0) {
        core_bridge_respond_error(pending->id, "Screenshot capture failed");
        finish_pending();
        return G_SOURCE_REMOVE;
    }

    XImage *img = XGetImage(dpy, DefaultRootWindow(dpy), dev_x, dev_y,
                            (unsigned int)dev_w, (unsigned int)dev_h,
                            AllPlanes, ZPixmap);
    if (!img) {
        core_bridge_respond_error(pending->id, "Screenshot capture failed");
        finish_pending();
        return G_SOURCE_REMOVE;
    }

    set_context(dev_x, dev_y, dev_w, dev_h, scale);

    cairo_surface_t *surface = ximage_to_surface(img);
    XDestroyImage(img);
    if (!surface) {
        core_bridge_respond_error(pending->id, "Screenshot encoding failed");
        finish_pending();
        return G_SOURCE_REMOVE;
    }

    // Crop, then shrink, then the cursor, then the encoder. The order matters
    // more than any single step: shrinking first means the cursor is drawn
    // onto — and the encoder runs over — a quarter of the pixels at half
    // width, and both of those cost strictly by the pixel.
    double crop_x = 0, crop_y = 0;
    if (pending->has_crop && pending->crop_w > 0 && pending->crop_h > 0) {
        int cw = (int)pending->crop_w, ch = (int)pending->crop_h;
        cairo_surface_t *cropped =
            cairo_image_surface_create(CAIRO_FORMAT_RGB24, cw, ch);
        cairo_t *cr = cairo_create(cropped);
        cairo_set_source_surface(cr, surface, -pending->crop_x, -pending->crop_y);
        cairo_paint(cr);
        cairo_destroy(cr);
        cairo_surface_destroy(surface);
        surface = cropped;
        crop_x = pending->crop_x;
        crop_y = pending->crop_y;
    }

    int source_w = cairo_image_surface_get_width(surface);
    int source_h = cairo_image_surface_get_height(surface);
    int out_w = source_w, out_h = source_h;
    // Only ever shrinks: asking for more pixels than were captured would
    // invent them, so a width larger than the window simply gets the window.
    if (pending->max_width > 0 && pending->max_width < source_w) {
        out_w = pending->max_width;
        out_h = MAX(1, (int)((double)source_h * out_w / source_w + 0.5));
    }
    double factor = (double)out_w / source_w;

    if (out_w != source_w) {
        cairo_surface_t *shrunk =
            cairo_image_surface_create(CAIRO_FORMAT_RGB24, out_w, out_h);
        cairo_t *cr = cairo_create(shrunk);
        cairo_scale(cr, factor, factor);
        cairo_set_source_surface(cr, surface, 0, 0);
        // Cheaper than BEST and, at these ratios, not tellable apart on a
        // frame that is about to be JPEG'd anyway.
        cairo_pattern_set_filter(cairo_get_source(cr), CAIRO_FILTER_GOOD);
        cairo_paint(cr);
        cairo_destroy(cr);
        cairo_surface_destroy(surface);
        surface = shrunk;
    }

    if (pending->with_cursor) {
        double px, py;
        mouse_get_virtual_pos(&px, &py);
        cairo_t *cr = cairo_create(surface);
        // Into the shrunk picture's own coordinates: past the crop's origin,
        // then scaled by however much the picture shrank. The cursor scales
        // with it, so it stays the same size relative to the screen.
        draw_cursor_into(cr, (px - crop_x) * factor, (py - crop_y) * factor,
                         scale * factor);
        cairo_destroy(cr);
    }

    guchar *bytes = NULL;
    gsize len = 0;
    gboolean encoded = surface_to_bytes(surface, pending->format,
                                        pending->quality, &bytes, &len);
    cairo_surface_destroy(surface);

    if (encoded) {
        deliver(pending->id, bytes, len, out_w, out_h, source_w, source_h);
        g_free(bytes);
    } else {
        core_bridge_respond_error(pending->id, "Screenshot encoding failed");
    }
    finish_pending();
    return G_SOURCE_REMOVE;
}

void screenshot_handle_capture(const char *id, gboolean with_cursor,
                               gboolean has_crop, double crop_x, double crop_y,
                               double crop_w, double crop_h, const char *format,
                               int max_width, int quality) {
    if (pending) { // should not happen — the core awaits each capture
        core_bridge_respond_error(id, "Capture already in progress");
        return;
    }
    if (!g_state.window || !gtk_widget_get_realized(GTK_WIDGET(g_state.window))) {
        core_bridge_respond_error(id, "Window not ready");
        return;
    }

    pending = g_new0(PendingShot, 1);
    pending->id = g_strdup(id);
    pending->with_cursor = with_cursor;
    pending->has_crop = has_crop;
    pending->crop_x = crop_x;
    pending->crop_y = crop_y;
    pending->crop_w = crop_w;
    pending->crop_h = crop_h;
    pending->format = g_strdup(format && *format ? format : "png");
    pending->max_width = max_width;
    pending->quality = quality;

    gint64 now = g_get_monotonic_time();
    if (last_capture_us) last_gap_us = now - last_capture_us;
    last_capture_us = now;

    if (restore_timeout) { // whatever this capture does, it is not done yet
        g_source_remove(restore_timeout);
        restore_timeout = 0;
    }

    if (window_hidden) {
        // Mid-burst: the window is already off the screen, so there is nothing
        // to wait for. This is the path every frame of a stream takes.
        do_capture(NULL);
        return;
    }

    // First of a burst: take the window off the screen so the capture shows
    // the desktop beneath it (macOS: .optionOnScreenBelowWindow), give the
    // compositor a frame to do it, then grab.
    set_window_hidden(TRUE);
    gdk_display_sync(gdk_display_get_default());
    g_timeout_add(COMPOSITOR_SETTLE_MS, do_capture, NULL);
}
