// Timestamped append-only logging to <project root>/app.log, matching the
// macOS AppLogger format: "[ISO8601] message\n".
#ifndef POB_APP_LOGGER_H
#define POB_APP_LOGGER_H

void app_logger_log(const char *fmt, ...);

#endif
