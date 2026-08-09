// Spawns and talks to the Go core (pob-core) over stdin/stdout using
// line-delimited JSON-RPC, mirroring the macOS CoreBridge. The Go side owns
// the agent loop, LLM calls, logs and the MCP server; this bridge answers
// its perception/operation requests (screenshot, mouse, keyboard, UI
// dialogs) and forwards user commands (run / stop / recording) the other way.
#ifndef POB_CORE_BRIDGE_H
#define POB_CORE_BRIDGE_H

#include <glib.h>

void core_bridge_start(void); // main thread, after the window is realized
void core_bridge_stop(void);

// Commands (shell -> Go notifications).
void core_bridge_run_macro(void);
void core_bridge_stop_execution(void);
void core_bridge_recording_changed(gboolean recording);
void core_bridge_take_screenshot(void);


// Thread-safe JSON-RPC responders, usable from the mouse worker thread.
void core_bridge_respond_position(const char *id); // {"x": .., "y": ..}
void core_bridge_respond_empty(const char *id);    // {}
// The fallback for a captured frame, used when the frame channel is not up:
// the picture as base64 on this line, with meta_json's whole-number members
// (the picture's size, and its source's) folded in beside it.
void core_bridge_respond_image(const char *id, const char *image_base64,
                               const char *meta_json);
void core_bridge_respond_error(const char *id, const char *message);

#endif
