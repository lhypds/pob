#include "app_logger.h"
#include "settings_service.h"

#include <glib.h>
#include <glib/gstdio.h>
#include <stdarg.h>
#include <stdio.h>

static GMutex log_mutex;

static void append_line(const char *path, const char *line) {
    FILE *f = g_fopen(path, "a");
    if (f) {
        fputs(line, f);
        fclose(f);
    }
}

static void write_message(const char *level, gboolean to_app_log,
                          const char *message) {
    GDateTime *now = g_date_time_new_now_utc();
    gchar *timestamp = g_date_time_format(now, "%Y-%m-%dT%H:%M:%S.%fZ");

    g_mutex_lock(&log_mutex);
    if (to_app_log) {
        gchar *path = g_build_filename(settings_project_root(), "app.log", NULL);
        gchar *line = g_strcmp0(level, "INFO") == 0
                          ? g_strdup_printf("[%s] %s\n", timestamp, message)
                          : g_strdup_printf("[%s] %s %s\n", timestamp, level, message);
        append_line(path, line);
        g_free(line);
        g_free(path);
    }
    // The instance log names its level in a column of its own, the way
    // pob-core writes INSTANCE START and the rest of its events.
    gchar *instance_path = g_build_filename(settings_project_root(),
                                            settings_instance_id(),
                                            "instance.log", NULL);
    gchar *instance_line = g_strdup_printf("[%s] %s %s\n", timestamp, level, message);
    append_line(instance_path, instance_line);
    g_free(instance_line);
    g_free(instance_path);
    g_mutex_unlock(&log_mutex);

    g_free(timestamp);
    g_date_time_unref(now);
}

void app_logger_log(const char *fmt, ...) {
    va_list args;
    va_start(args, fmt);
    gchar *message = g_strdup_vprintf(fmt, args);
    va_end(args);
    write_message("INFO", FALSE, message);
    g_free(message);
}

void app_logger_event(const char *fmt, ...) {
    va_list args;
    va_start(args, fmt);
    gchar *message = g_strdup_vprintf(fmt, args);
    va_end(args);
    write_message("INFO", TRUE, message);
    g_free(message);
}

void app_logger_error(const char *fmt, ...) {
    va_list args;
    va_start(args, fmt);
    gchar *message = g_strdup_vprintf(fmt, args);
    va_end(args);
    write_message("ERROR", TRUE, message);
    g_free(message);
}
