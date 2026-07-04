#include "content_view.h"
#include "app.h"

#include <math.h>
#include <string.h>

// ── view state ──────────────────────────────────────────────────────────────

static GtkWidget *view = NULL;

// Targeting: last pointer position in widget (logical) coordinates.
static gboolean has_mouse_pos = FALSE;
static double mouse_x = 0, mouse_y = 0;

// Cropping drag, widget coordinates.
static gboolean crop_dragging = FALSE;
static gboolean has_crop_rect = FALSE;
static double crop_start_x = 0, crop_start_y = 0;
static double crop_cur_x = 0, crop_cur_y = 0;

// Virtual cursor animation (screenshot/device pixels).
static double anim_x = 20, anim_y = 20;
static double anim_from_x = 20, anim_from_y = 20;
static double anim_to_x = 20, anim_to_y = 20;
static gint64 anim_start_us = 0;
static gboolean animating = FALSE;

// Screenshot flash.
static double flash_opacity = 0;
static gint64 flash_start_us = 0;
static gboolean flashing = FALSE;

static guint tick_id = 0;

#define ANIM_DURATION_US (100 * 1000)  // matches .easeOut(duration: 0.1)
#define FLASH_DURATION_US (400 * 1000) // matches .easeOut(duration: 0.4)

// ── animation plumbing ──────────────────────────────────────────────────────

static gboolean on_tick(GtkWidget *widget, GdkFrameClock *clock, gpointer data) {
    (void)clock;
    (void)data;
    gint64 now = g_get_monotonic_time();
    gboolean active = FALSE;

    if (animating) {
        double t = (double)(now - anim_start_us) / ANIM_DURATION_US;
        if (t >= 1.0) {
            anim_x = anim_to_x;
            anim_y = anim_to_y;
            animating = FALSE;
        } else {
            double e = 1.0 - pow(1.0 - t, 3); // ease-out cubic
            anim_x = anim_from_x + (anim_to_x - anim_from_x) * e;
            anim_y = anim_from_y + (anim_to_y - anim_from_y) * e;
            active = TRUE;
        }
    }

    if (flashing) {
        double t = (double)(now - flash_start_us) / FLASH_DURATION_US;
        if (t >= 1.0) {
            flash_opacity = 0;
            flashing = FALSE;
        } else {
            double e = 1.0 - pow(1.0 - t, 3);
            flash_opacity = 0.5 * (1.0 - e);
            active = TRUE;
        }
    }

    gtk_widget_queue_draw(widget);
    if (!active) {
        tick_id = 0;
        return G_SOURCE_REMOVE;
    }
    return G_SOURCE_CONTINUE;
}

static void ensure_tick(void) {
    if (!tick_id && view)
        tick_id = gtk_widget_add_tick_callback(view, on_tick, NULL, NULL);
}

void content_view_cursor_target_changed(double x, double y) {
    anim_from_x = anim_x;
    anim_from_y = anim_y;
    anim_to_x = x;
    anim_to_y = y;
    anim_start_us = g_get_monotonic_time();
    animating = TRUE;
    ensure_tick();
}

void content_view_reset_anim(void) {
    anim_x = anim_from_x = anim_to_x = 20;
    anim_y = anim_from_y = anim_to_y = 20;
    animating = FALSE;
    if (view) gtk_widget_queue_draw(view);
}

void content_view_flash(void) {
    flash_opacity = 0.5;
    flash_start_us = g_get_monotonic_time();
    flashing = TRUE;
    ensure_tick();
}

// ── transient toast message ─────────────────────────────────────────────────

static gchar *toast_text = NULL;
static guint toast_timeout = 0;

static gboolean toast_expired(gpointer data) {
    (void)data;
    toast_timeout = 0;
    g_clear_pointer(&toast_text, g_free);
    if (view) gtk_widget_queue_draw(view);
    return G_SOURCE_REMOVE;
}

void content_view_show_message(const char *text) {
    g_clear_pointer(&toast_text, g_free);
    toast_text = g_strdup(text);
    if (toast_timeout) g_source_remove(toast_timeout);
    toast_timeout = g_timeout_add(2000, toast_expired, NULL);
    if (view) gtk_widget_queue_draw(view);
}

// ── drawing helpers ─────────────────────────────────────────────────────────

static void rounded_rect(cairo_t *cr, double x, double y, double w, double h, double r) {
    cairo_new_sub_path(cr);
    cairo_arc(cr, x + w - r, y + r, r, -G_PI / 2, 0);
    cairo_arc(cr, x + w - r, y + h - r, r, 0, G_PI / 2);
    cairo_arc(cr, x + r, y + h - r, r, G_PI / 2, G_PI);
    cairo_arc(cr, x + r, y + r, r, G_PI, 3 * G_PI / 2);
    cairo_close_path(cr);
}

// Black 75% pill with white 11 px monospaced text, centered at (cx, cy) —
// the same style as the macOS coordinate labels.
static void draw_label(cairo_t *cr, double cx, double cy, const char *text) {
    cairo_save(cr);
    cairo_select_font_face(cr, "monospace", CAIRO_FONT_SLANT_NORMAL,
                           CAIRO_FONT_WEIGHT_BOLD);
    cairo_set_font_size(cr, 11);

    cairo_text_extents_t ext;
    cairo_text_extents(cr, text, &ext);
    double pad_h = 6, pad_v = 3;
    double w = ext.width + pad_h * 2;
    double h = 11 + pad_v * 2;
    double x = cx - w / 2, y = cy - h / 2;

    cairo_set_source_rgba(cr, 0, 0, 0, 0.75);
    rounded_rect(cr, x, y, w, h, 4);
    cairo_fill(cr);

    cairo_set_source_rgb(cr, 1, 1, 1);
    cairo_move_to(cr, x + pad_h - ext.x_bearing, cy + 11.0 / 2 - 2);
    cairo_show_text(cr, text);
    cairo_restore(cr);
}

static int widget_scale(void) {
    return view ? gtk_widget_get_scale_factor(view) : 1;
}

// System arrow cursor for the overlay; shares the fallback shape with the
// screenshot compositor but at natural (logical) size.
static cairo_surface_t *overlay_cursor(double *hot_x, double *hot_y,
                                       double *w, double *h) {
    static cairo_surface_t *cached = NULL;
    static double chx = 0, chy = 0, cw = 0, chh = 0;
    if (!cached) {
        GdkCursor *cursor =
            gdk_cursor_new_from_name(gdk_display_get_default(), "default");
        if (cursor) {
            double hx = 0, hy = 0;
            cairo_surface_t *surf = gdk_cursor_get_surface(cursor, &hx, &hy);
            if (surf && cairo_surface_get_type(surf) == CAIRO_SURFACE_TYPE_IMAGE) {
                cached = surf;
                chx = hx;
                chy = hy;
                cw = cairo_image_surface_get_width(surf);
                chh = cairo_image_surface_get_height(surf);
            } else if (surf) {
                cairo_surface_destroy(surf);
            }
            g_object_unref(cursor);
        }
    }
    if (!cached) {
        cached = cairo_image_surface_create(CAIRO_FORMAT_ARGB32, 24, 36);
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
        chx = 0;
        chy = 0;
        cw = 24;
        chh = 36;
    }
    *hot_x = chx;
    *hot_y = chy;
    *w = cw;
    *h = chh;
    return cached;
}

// ── draw ────────────────────────────────────────────────────────────────────

static gboolean on_draw(GtkWidget *widget, cairo_t *cr, gpointer data) {
    (void)data;
    GtkAllocation alloc;
    gtk_widget_get_allocation(widget, &alloc);
    double W = alloc.width, H = alloc.height;
    int scale = widget_scale();

    // Translucent gray background (Color.gray.opacity(0.2)). Painted with
    // OPERATOR_SOURCE so the pixels become exactly 20%-alpha gray no matter
    // what the theme painted underneath — some themes (e.g. PiXflat) fill
    // the window with an opaque background-image that CSS overrides miss.
    cairo_save(cr);
    cairo_set_operator(cr, CAIRO_OPERATOR_SOURCE);
    cairo_set_source_rgba(cr, POB_COLOR_GRAY_R, POB_COLOR_GRAY_G,
                          POB_COLOR_GRAY_B, POB_COLOR_GRAY_A);
    cairo_paint(cr);
    cairo_restore(cr);

    // Self-diagnosis: transparency needs a compositor AND a 32-bit ARGB
    // visual. Draw what is missing as top-centered black pills with white
    // text — the same message style as the macOS shell.
    {
        const char *hints[2];
        int n = 0;
        if (!gdk_screen_is_composited(gtk_widget_get_screen(widget)))
            hints[n++] = "No compositor \xE2\x80\x94 transparency unavailable "
                         "(run: xcompmgr, or: picom --backend xrender -b)";
        GdkWindow *gdkwin = gtk_widget_get_window(widget);
        if (gdkwin && gdk_visual_get_depth(gdk_window_get_visual(gdkwin)) != 32)
            hints[n++] = "No ARGB visual \xE2\x80\x94 the window has no alpha channel";
        for (int i = 0; i < n; i++)
            draw_label(cr, W / 2, 20 + i * 24, hints[i]);

        // Transient action feedback, stacked below the diagnostics.
        if (toast_text)
            draw_label(cr, W / 2, 20 + n * 24, toast_text);
    }

    // Crop selection rectangle + size label.
    if (g_state.is_cropping && has_crop_rect) {
        double min_x = MIN(crop_start_x, crop_cur_x);
        double min_y = MIN(crop_start_y, crop_cur_y);
        double w = fabs(crop_cur_x - crop_start_x);
        double h = fabs(crop_cur_y - crop_start_y);

        cairo_set_source_rgba(cr, POB_COLOR_BLUE_R, POB_COLOR_BLUE_G,
                              POB_COLOR_BLUE_B, 0.08);
        cairo_rectangle(cr, min_x, min_y, w, h);
        cairo_fill(cr);

        cairo_set_source_rgb(cr, POB_COLOR_BLUE_R, POB_COLOR_BLUE_G,
                             POB_COLOR_BLUE_B);
        cairo_set_line_width(cr, 1);
        cairo_rectangle(cr, min_x + 0.5, min_y + 0.5, w, h);
        cairo_stroke(cr);

        gchar *text = g_strdup_printf(
            "(%d, %d) %d\xC3\x97%d", (int)(min_x * scale), (int)(min_y * scale),
            (int)(w * scale), (int)(h * scale));

        // Same clamping as the macOS view: prefer below the box, then above.
        double label_w = 180, label_h = 22, margin = 6;
        double cx = CLAMP(min_x + w / 2, label_w / 2 + margin,
                          W - label_w / 2 - margin);
        double below_y = min_y + h + 2 + label_h / 2;
        double above_y = min_y - 2 - label_h / 2;
        double min_allowed = margin + label_h / 2;
        double max_allowed = H - margin - label_h / 2;
        double cy;
        if (below_y <= max_allowed) cy = below_y;
        else if (above_y >= min_allowed) cy = above_y;
        else cy = CLAMP(below_y, min_allowed, max_allowed);

        draw_label(cr, cx, cy, text);
        g_free(text);
    }

    // Targeting coordinate label following the pointer.
    if (g_state.is_targeting && has_mouse_pos) {
        gchar *text = g_strdup_printf("(%d, %d)", (int)(mouse_x * scale),
                                      (int)(mouse_y * scale));
        double est_w = 100, margin = 6;
        double raw_x = mouse_x + 55;
        double cx = MAX(est_w / 2 + margin, MIN(raw_x, W - est_w / 2 - margin));
        double cy = MAX(14, mouse_y - 14);
        draw_label(cr, cx, cy, text);
        g_free(text);
    }

    // Virtual cursor overlay while the agent executes.
    if (g_state.is_executing) {
        double hx, hy, cw, ch;
        cairo_surface_t *surf = overlay_cursor(&hx, &hy, &cw, &ch);
        if (surf) {
            double vx = anim_x / scale, vy = anim_y / scale;
            cairo_save(cr);
            cairo_set_source_surface(cr, surf, vx - hx, vy - hy);
            cairo_paint(cr);
            cairo_restore(cr);
        }
    }

    // Screenshot flash.
    if (flash_opacity > 0) {
        cairo_set_source_rgba(cr, 1, 1, 1, flash_opacity);
        cairo_paint(cr);
    }
    return FALSE;
}

// ── input ───────────────────────────────────────────────────────────────────

static void copy_to_clipboard(const char *text) {
    GtkClipboard *cb = gtk_clipboard_get(GDK_SELECTION_CLIPBOARD);
    gtk_clipboard_set_text(cb, text, -1);
    gtk_clipboard_store(cb);
}

static gboolean on_button_press(GtkWidget *widget, GdkEventButton *ev, gpointer data) {
    (void)widget;
    (void)data;
    if (ev->button != 1) return FALSE;

    if (g_state.is_targeting) {
        int scale = widget_scale();
        gchar *text = g_strdup_printf("(%d, %d)", (int)(ev->x * scale),
                                      (int)(ev->y * scale));
        copy_to_clipboard(text);
        gchar *msg = g_strdup_printf("Copied %s", text);
        content_view_show_message(msg);
        g_free(msg);
        g_free(text);
        has_mouse_pos = FALSE;
        app_set_targeting(FALSE);
        return TRUE;
    }

    if (g_state.is_cropping) {
        crop_dragging = TRUE;
        has_crop_rect = FALSE;
        crop_start_x = crop_cur_x = ev->x;
        crop_start_y = crop_cur_y = ev->y;
        return TRUE;
    }

    // Plain click: bring the overlay window forward (macOS onTapGesture).
    gtk_window_present(g_state.window);
    return FALSE;
}

static gboolean on_motion(GtkWidget *widget, GdkEventMotion *ev, gpointer data) {
    (void)data;
    if (g_state.is_targeting) {
        has_mouse_pos = TRUE;
        mouse_x = ev->x;
        mouse_y = ev->y;
        gtk_widget_queue_draw(widget);
    } else if (g_state.is_cropping && crop_dragging) {
        has_crop_rect = TRUE;
        crop_cur_x = ev->x;
        crop_cur_y = ev->y;
        gtk_widget_queue_draw(widget);
    }
    return FALSE;
}

static gboolean on_button_release(GtkWidget *widget, GdkEventButton *ev, gpointer data) {
    (void)widget;
    (void)data;
    if (ev->button != 1 || !g_state.is_cropping || !crop_dragging) return FALSE;
    crop_dragging = FALSE;

    double min_x = MIN(crop_start_x, ev->x);
    double min_y = MIN(crop_start_y, ev->y);
    double w = fabs(ev->x - crop_start_x);
    double h = fabs(ev->y - crop_start_y);
    has_crop_rect = FALSE;

    if (w > 2 && h > 2) {
        int scale = widget_scale();
        gchar *text = g_strdup_printf(
            "(%d, %d, %d, %d)", (int)(min_x * scale), (int)(min_y * scale),
            (int)(w * scale), (int)(h * scale));
        copy_to_clipboard(text);
        gchar *msg = g_strdup_printf("Copied %s", text);
        content_view_show_message(msg);
        g_free(msg);
        g_free(text);
        app_set_cropping(FALSE);
    } else {
        gtk_widget_queue_draw(widget);
    }
    return TRUE;
}

static gboolean on_leave(GtkWidget *widget, GdkEventCrossing *ev, gpointer data) {
    (void)ev;
    (void)data;
    if (g_state.is_targeting) {
        has_mouse_pos = FALSE;
        gtk_widget_queue_draw(widget);
    }
    return FALSE;
}

void content_view_update_cursor_style(void) {
    if (!view) return;
    GdkWindow *win = gtk_widget_get_window(view);
    if (!win) return;
    if (g_state.is_cropping) {
        GdkCursor *crosshair =
            gdk_cursor_new_from_name(gdk_display_get_default(), "crosshair");
        gdk_window_set_cursor(win, crosshair);
        if (crosshair) g_object_unref(crosshair);
    } else {
        gdk_window_set_cursor(win, NULL);
    }
    has_mouse_pos = FALSE;
    has_crop_rect = FALSE;
    crop_dragging = FALSE;
    gtk_widget_queue_draw(view);
}

// ── construction ────────────────────────────────────────────────────────────

GtkWidget *content_view_new(void) {
    view = gtk_drawing_area_new();
    gtk_widget_set_size_request(view, 400, 300); // macOS: minWidth/minHeight
    gtk_widget_add_events(view, GDK_BUTTON_PRESS_MASK | GDK_BUTTON_RELEASE_MASK |
                                    GDK_POINTER_MOTION_MASK |
                                    GDK_LEAVE_NOTIFY_MASK);
    g_signal_connect(view, "draw", G_CALLBACK(on_draw), NULL);
    g_signal_connect(view, "button-press-event", G_CALLBACK(on_button_press), NULL);
    g_signal_connect(view, "motion-notify-event", G_CALLBACK(on_motion), NULL);
    g_signal_connect(view, "button-release-event", G_CALLBACK(on_button_release), NULL);
    g_signal_connect(view, "leave-notify-event", G_CALLBACK(on_leave), NULL);
    return view;
}
