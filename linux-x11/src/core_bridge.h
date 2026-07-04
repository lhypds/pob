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
void core_bridge_run_instruction(gboolean recording);
void core_bridge_run_macro(void);
void core_bridge_stop_execution(void);
void core_bridge_recording_changed(gboolean recording);

// Answers the pending ui.confirmMaxStep request (no-op when none pending).
void core_bridge_resolve_max_step(gboolean should_continue);

// Thread-safe JSON-RPC responders, usable from the mouse worker thread.
void core_bridge_respond_position(const char *id); // {"x": .., "y": ..}
void core_bridge_respond_empty(const char *id);    // {}
void core_bridge_respond_image(const char *id, const char *png_base64);
void core_bridge_respond_error(const char *id, const char *message);

#endif
