#include "screenshot_service.h"
#include "app.h"
#include "app_logger.h"
#include "core_bridge.h"
#include "mouse_service.h"

#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <cairo.h>
#include <gdk/gdkx.h>
#include <string.h>

// ── capture context ─────────────────────────────────────────────────────────

static GMutex ctx_mutex;
static ShotContext current_ctx = {FALSE, 0, 0, 1};

ShotContext screenshot_get_context(void) {
    g_mutex_lock(&ctx_mutex);
    ShotContext c = current_ctx;
    g_mutex_unlock(&ctx_mutex);
    return c;
}

static void set_context(int ox, int oy, int scale) {
    g_mutex_lock(&ctx_mutex);
    current_ctx.valid = TRUE;
    current_ctx.origin_x = ox;
    current_ctx.origin_y = oy;
    current_ctx.scale = scale;
    g_mutex_unlock(&ctx_mutex);
}

// ── pending request (core sends one capture at a time) ─────────────────────

typedef struct {
    char *id;
    gboolean with_cursor;
    gboolean has_crop;
    double crop_x, crop_y, crop_w, crop_h;
} PendingShot;

static PendingShot *pending = NULL;

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
// screenshot pixels. macOS renders the cursor 88 px tall on 2× displays;
// 44 × scale keeps the same apparent size on every density.
static void draw_cursor_into(cairo_t *cr, double px, double py, int scale) {
    double hx, hy, cw, ch;
    cairo_surface_t *surf = cursor_surface(&hx, &hy, &cw, &ch);
    if (!surf || ch <= 0) return;

    double target_h = 44.0 * scale;
    double s = target_h / ch;

    cairo_save(cr);
    cairo_translate(cr, px - hx * s, py - hy * s);
    cairo_scale(cr, s, s);
    cairo_set_source_surface(cr, surf, 0, 0);
    cairo_paint(cr);
    cairo_restore(cr);
}

// ── PNG encoding ────────────────────────────────────────────────────────────

static cairo_status_t png_writer(void *closure, const unsigned char *data,
                                 unsigned int length) {
    g_byte_array_append((GByteArray *)closure, data, length);
    return CAIRO_STATUS_SUCCESS;
}

static gchar *surface_to_png_base64(cairo_surface_t *surface) {
    GByteArray *bytes = g_byte_array_new();
    if (cairo_surface_write_to_png_stream(surface, png_writer, bytes) !=
        CAIRO_STATUS_SUCCESS) {
        g_byte_array_unref(bytes);
        return NULL;
    }
    gchar *b64 = g_base64_encode(bytes->data, bytes->len);
    g_byte_array_unref(bytes);
    return b64;
}

// ── capture flow ────────────────────────────────────────────────────────────

static void finish_pending(void) {
    if (!pending) return;
    g_free(pending->id);
    g_free(pending);
    pending = NULL;
}

static gboolean do_capture(gpointer data) {
    (void)data;
    if (!pending) return G_SOURCE_REMOVE;

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

    gtk_widget_set_opacity(win, 1.0);

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

    set_context(dev_x, dev_y, scale);

    cairo_surface_t *surface = ximage_to_surface(img);
    XDestroyImage(img);
    if (!surface) {
        core_bridge_respond_error(pending->id, "Screenshot encoding failed");
        finish_pending();
        return G_SOURCE_REMOVE;
    }

    if (pending->with_cursor) {
        double px, py;
        mouse_get_virtual_pos(&px, &py);
        cairo_t *cr = cairo_create(surface);
        draw_cursor_into(cr, px, py, scale);
        cairo_destroy(cr);
    }

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
    }

    gchar *b64 = surface_to_png_base64(surface);
    cairo_surface_destroy(surface);

    if (b64) {
        core_bridge_respond_image(pending->id, b64);
        g_free(b64);
    } else {
        core_bridge_respond_error(pending->id, "Screenshot encoding failed");
    }
    finish_pending();
    return G_SOURCE_REMOVE;
}

void screenshot_handle_capture(const char *id, gboolean with_cursor,
                               gboolean has_crop, double crop_x, double crop_y,
                               double crop_w, double crop_h) {
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

    // Hide the overlay for one compositor frame so the capture shows the
    // desktop beneath it (macOS: .optionOnScreenBelowWindow), then grab.
    gtk_widget_set_opacity(GTK_WIDGET(g_state.window), 0.0);
    gdk_display_sync(gdk_display_get_default());
    g_timeout_add(80, do_capture, NULL);
}
