// Which of Pob's two logs a shell message belongs in, matching the macOS
// AppLogger.
//
// <project root>/app.log is the machine's record across instances and is kept
// short on purpose: the app starting and stopping, an instance starting and
// stopping, and errors. Read on its own it should answer "did it come up, and
// did anything break" without scrolling.
//
// Everything else is detail, and detail belongs to the instance —
// <project root>/<instance>/instance.log, the file the toolbar's ins.log
// button opens and the one pob-core writes its own steps to. Every message
// logged here lands there whatever its level, so the shell's side of a run
// reads in order beside the core's.
//
// Lines are "[ISO8601] message" in app.log and "[ISO8601] LEVEL message" in
// instance.log, appended one at a time so two processes writing at once
// interleave without corrupting each other.
#ifndef POB_APP_LOGGER_H
#define POB_APP_LOGGER_H

// Detail: the instance log alone.
void app_logger_log(const char *fmt, ...);

// A line app.log is kept for — the app or an instance starting or stopping.
// Goes to both logs.
void app_logger_event(const char *fmt, ...);

// A failure. Goes to both logs, marked ERROR, so app.log answers what went
// wrong and instance.log keeps it beside the detail that led there.
void app_logger_error(const char *fmt, ...);

#endif
