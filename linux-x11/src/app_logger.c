#include "app_logger.h"
#include "settings_service.h"

#include <glib.h>
#include <glib/gstdio.h>
#include <stdarg.h>
#include <stdio.h>

static GMutex log_mutex;

void app_logger_log(const char *fmt, ...) {
    va_list args;
    va_start(args, fmt);
    gchar *message = g_strdup_vprintf(fmt, args);
    va_end(args);

    GDateTime *now = g_date_time_new_now_utc();
    gchar *timestamp = g_date_time_format(now, "%Y-%m-%dT%H:%M:%SZ");
    gchar *path = g_build_filename(settings_project_root(), "app.log", NULL);

    g_mutex_lock(&log_mutex);
    FILE *f = g_fopen(path, "a");
    if (f) {
        fprintf(f, "[%s] %s\n", timestamp, message);
        fclose(f);
    }
    g_mutex_unlock(&log_mutex);

    g_free(path);
    g_free(timestamp);
    g_date_time_unref(now);
    g_free(message);
}
