#include "core_bridge.h"
#include "app.h"
#include "app_logger.h"
#include "content_view.h"
#include "mouse_service.h"
#include "screenshot_service.h"
#include "settings_service.h"

#include <json-glib/json-glib.h>
#include <string.h>
#include <unistd.h>

static GPid core_pid = 0;
static int stdin_fd = -1;
static GIOChannel *stdout_channel = NULL;
static guint stdout_watch = 0;
static guint child_watch = 0;

static GMutex write_mutex;

// Pending ui.confirmMaxStep request id (main thread only).
static gchar *max_step_request_id = NULL;

// ── writing ─────────────────────────────────────────────────────────────────

static void write_line(const gchar *json) {
    g_mutex_lock(&write_mutex);
    if (stdin_fd >= 0) {
        gsize len = strlen(json);
        gchar *line = g_malloc(len + 1);
        memcpy(line, json, len);
        line[len] = '\n';
        gsize off = 0;
        while (off < len + 1) {
            gssize n = write(stdin_fd, line + off, len + 1 - off);
            if (n <= 0) break;
            off += (gsize)n;
        }
        g_free(line);
    }
    g_mutex_unlock(&write_mutex);
}

static void send_builder(JsonBuilder *builder) {
    JsonNode *root = json_builder_get_root(builder);
    JsonGenerator *gen = json_generator_new();
    json_generator_set_root(gen, root);
    gchar *json = json_generator_to_data(gen, NULL);
    write_line(json);
    g_free(json);
    g_object_unref(gen);
    json_node_unref(root);
    g_object_unref(builder);
}

static JsonBuilder *begin_message(void) {
    JsonBuilder *b = json_builder_new();
    json_builder_begin_object(b);
    json_builder_set_member_name(b, "jsonrpc");
    json_builder_add_string_value(b, "2.0");
    return b;
}

static void notify(const char *method, JsonBuilder *(*add_params)(JsonBuilder *, gpointer), gpointer data) {
    JsonBuilder *b = begin_message();
    json_builder_set_member_name(b, "method");
    json_builder_add_string_value(b, method);
    if (add_params) {
        json_builder_set_member_name(b, "params");
        json_builder_begin_object(b);
        add_params(b, data);
        json_builder_end_object(b);
    }
    json_builder_end_object(b);
    send_builder(b);
}

// ── responders (thread-safe) ────────────────────────────────────────────────

static JsonBuilder *begin_response(const char *id) {
    JsonBuilder *b = begin_message();
    json_builder_set_member_name(b, "id");
    json_builder_add_string_value(b, id);
    json_builder_set_member_name(b, "result");
    json_builder_begin_object(b);
    return b;
}

static void end_response(JsonBuilder *b) {
    json_builder_end_object(b); // result
    json_builder_end_object(b); // root
    send_builder(b);
}

void core_bridge_respond_position(const char *id) {
    double x, y;
    mouse_get_virtual_pos(&x, &y);
    JsonBuilder *b = begin_response(id);
    json_builder_set_member_name(b, "x");
    json_builder_add_double_value(b, x);
    json_builder_set_member_name(b, "y");
    json_builder_add_double_value(b, y);
    end_response(b);
}

void core_bridge_respond_empty(const char *id) {
    JsonBuilder *b = begin_response(id);
    end_response(b);
}

void core_bridge_respond_image(const char *id, const char *png_base64) {
    JsonBuilder *b = begin_response(id);
    json_builder_set_member_name(b, "image");
    json_builder_add_string_value(b, png_base64);
    end_response(b);
}

void core_bridge_respond_error(const char *id, const char *message) {
    JsonBuilder *b = begin_message();
    json_builder_set_member_name(b, "id");
    json_builder_add_string_value(b, id);
    json_builder_set_member_name(b, "error");
    json_builder_begin_object(b);
    json_builder_set_member_name(b, "code");
    json_builder_add_int_value(b, -32603);
    json_builder_set_member_name(b, "message");
    json_builder_add_string_value(b, message);
    json_builder_end_object(b);
    json_builder_end_object(b);
    send_builder(b);
}

// ── commands (shell -> Go) ──────────────────────────────────────────────────

static JsonBuilder *add_recording(JsonBuilder *b, gpointer data) {
    json_builder_set_member_name(b, "recording");
    json_builder_add_boolean_value(b, GPOINTER_TO_INT(data));
    return b;
}

static JsonBuilder *add_nothing(JsonBuilder *b, gpointer data) {
    (void)data;
    return b;
}

void core_bridge_run_instruction(gboolean recording) {
    notify("run.instruction", add_recording, GINT_TO_POINTER(recording));
}

void core_bridge_run_macro(void) {
    notify("run.macro", add_nothing, NULL);
}

void core_bridge_stop_execution(void) {
    core_bridge_resolve_max_step(FALSE);
    notify("run.stop", add_nothing, NULL);
}

void core_bridge_recording_changed(gboolean recording) {
    notify("recording.changed", add_recording, GINT_TO_POINTER(recording));
}

// Toolbar screenshot button: the Go core flashes, captures and saves the
// shot, and records a take_screenshot() macro line while recording.
void core_bridge_take_screenshot(void) {
    notify("screenshot.take", add_nothing, NULL);
}

void core_bridge_resolve_max_step(gboolean should_continue) {
    if (!max_step_request_id) return;
    gchar *id = max_step_request_id;
    max_step_request_id = NULL;

    JsonBuilder *b = begin_response(id);
    json_builder_set_member_name(b, "continue");
    json_builder_add_boolean_value(b, should_continue);
    end_response(b);
    g_free(id);
}

// ── dispatch (main thread) ──────────────────────────────────────────────────

static double member_double(JsonObject *obj, const char *name, double fallback) {
    if (!obj || !json_object_has_member(obj, name)) return fallback;
    return json_object_get_double_member_with_default(obj, name, fallback);
}

static void dispatch(JsonObject *msg) {
    const gchar *method = NULL;
    if (json_object_has_member(msg, "method"))
        method = json_object_get_string_member_with_default(msg, "method", NULL);
    if (!method) return;

    JsonObject *params = NULL;
    if (json_object_has_member(msg, "params")) {
        JsonNode *p = json_object_get_member(msg, "params");
        if (JSON_NODE_HOLDS_OBJECT(p)) params = json_node_get_object(p);
    }

    const gchar *id = NULL;
    if (json_object_has_member(msg, "id"))
        id = json_object_get_string_member_with_default(msg, "id", NULL);

    if (g_str_equal(method, "session.state")) {
        gboolean executing = params &&
            json_object_get_boolean_member_with_default(params, "executing", FALSE);
        app_set_executing(executing);

    } else if (g_str_equal(method, "screenshot.capture")) {
        if (!id) return;
        gboolean with_cursor = params
            ? json_object_get_boolean_member_with_default(params, "withCursor", TRUE)
            : TRUE;
        gboolean has_crop = FALSE;
        double cx = 0, cy = 0, cw = 0, ch = 0;
        if (params && json_object_has_member(params, "crop")) {
            JsonNode *cn = json_object_get_member(params, "crop");
            if (JSON_NODE_HOLDS_OBJECT(cn)) {
                JsonObject *crop = json_node_get_object(cn);
                cx = member_double(crop, "x", 0);
                cy = member_double(crop, "y", 0);
                cw = member_double(crop, "width", 0);
                ch = member_double(crop, "height", 0);
                has_crop = TRUE;
            }
        }
        screenshot_handle_capture(id, with_cursor, has_crop, cx, cy, cw, ch);

    } else if (g_str_equal(method, "cursor.reset")) {
        if (!id) return;
        mouse_reset_cursor();
        core_bridge_respond_position(id);

    } else if (g_str_equal(method, "cursor.move")) {
        if (!id) return;
        mouse_move_by(member_double(params, "dx", 0), member_double(params, "dy", 0));
        core_bridge_respond_position(id);

    } else if (g_str_equal(method, "mouse.click")) {
        if (id) mouse_enqueue_job(MOUSE_JOB_CLICK, id, 0, 0, NULL);

    } else if (g_str_equal(method, "mouse.rightClick")) {
        if (id) mouse_enqueue_job(MOUSE_JOB_RIGHT_CLICK, id, 0, 0, NULL);

    } else if (g_str_equal(method, "mouse.doubleClick")) {
        if (id) mouse_enqueue_job(MOUSE_JOB_DOUBLE_CLICK, id, 0, 0, NULL);

    } else if (g_str_equal(method, "mouse.drag")) {
        if (id) mouse_enqueue_job(MOUSE_JOB_DRAG, id,
                                  member_double(params, "dx", 0),
                                  member_double(params, "dy", 0), NULL);

    } else if (g_str_equal(method, "mouse.scroll")) {
        if (id) mouse_enqueue_job(MOUSE_JOB_SCROLL, id,
                                  member_double(params, "dx", 0),
                                  member_double(params, "dy", 0), NULL);

    } else if (g_str_equal(method, "keyboard.type")) {
        const gchar *text = params
            ? json_object_get_string_member_with_default(params, "text", "")
            : "";
        if (id) mouse_enqueue_job(MOUSE_JOB_TYPE, id, 0, 0, text);

    } else if (g_str_equal(method, "keyboard.keyPress")) {
        const gchar *key = params
            ? json_object_get_string_member_with_default(params, "key", "")
            : "";
        if (id) mouse_enqueue_job(MOUSE_JOB_KEY_PRESS, id, 0, 0, key);

    } else if (g_str_equal(method, "ui.flash")) {
        content_view_flash();
        if (id) core_bridge_respond_empty(id);

    } else if (g_str_equal(method, "ui.confirmMaxStep")) {
        if (!id) return;
        g_free(max_step_request_id);
        max_step_request_id = g_strdup(id);
        app_show_max_step_dialog();

    } else {
        if (id) {
            gchar *msg_text = g_strdup_printf("Unknown method: %s", method);
            core_bridge_respond_error(id, msg_text);
            g_free(msg_text);
        }
    }
}

// ── reading ─────────────────────────────────────────────────────────────────

static gboolean on_stdout(GIOChannel *channel, GIOCondition cond, gpointer data) {
    (void)data;
    if (cond & G_IO_IN) {
        for (;;) {
            gchar *line = NULL;
            gsize len = 0;
            GIOStatus status = g_io_channel_read_line(channel, &line, &len, NULL, NULL);
            if (status != G_IO_STATUS_NORMAL || !line) {
                g_free(line);
                if (status == G_IO_STATUS_EOF) return FALSE;
                break; // AGAIN: wait for more data
            }
            JsonParser *parser = json_parser_new();
            if (json_parser_load_from_data(parser, line, (gssize)len, NULL)) {
                JsonNode *root = json_parser_get_root(parser);
                if (root && JSON_NODE_HOLDS_OBJECT(root))
                    dispatch(json_node_get_object(root));
            }
            g_object_unref(parser);
            g_free(line);
        }
    }
    if (cond & (G_IO_HUP | G_IO_ERR)) return FALSE;
    return TRUE;
}

static void on_child_exit(GPid pid, gint status, gpointer data) {
    (void)status;
    (void)data;
    app_logger_log("CoreBridge: pob-core exited");
    g_spawn_close_pid(pid);
    core_pid = 0;
    app_set_executing(FALSE);
}

// ── lifecycle ───────────────────────────────────────────────────────────────

// Packaged install: pob-core sits next to the shell binary.
// Dev workflow: built by restart.sh into <root>/core/bin/.
static gchar *locate_core_binary(void) {
    gchar *self = g_file_read_link("/proc/self/exe", NULL);
    if (self) {
        gchar *dir = g_path_get_dirname(self);
        gchar *bundled = g_build_filename(dir, "pob-core", NULL);
        g_free(dir);
        g_free(self);
        if (g_file_test(bundled, G_FILE_TEST_IS_EXECUTABLE)) return bundled;
        g_free(bundled);
    }
    gchar *dev = g_build_filename(settings_project_root(), "core", "bin", "pob-core", NULL);
    if (g_file_test(dev, G_FILE_TEST_IS_EXECUTABLE)) return dev;
    g_free(dev);
    return NULL;
}

void core_bridge_start(void) {
    const char *root = settings_project_root();
    gchar *binary = locate_core_binary();
    if (!binary) {
        app_logger_log("CoreBridge: pob-core binary not found — run ./linux-x11/setup.sh");
        return;
    }

    gchar *argv[] = {binary, "--root", (gchar *)root,
                     "--instance", (gchar *)settings_instance_id(), NULL};
    gint child_stdin = -1, child_stdout = -1;
    GError *error = NULL;
    gboolean ok = g_spawn_async_with_pipes(
        root, argv, NULL, G_SPAWN_DO_NOT_REAP_CHILD, NULL, NULL, &core_pid,
        &child_stdin, &child_stdout, NULL, &error);

    if (!ok) {
        app_logger_log("CoreBridge: failed to start pob-core: %s",
                       error ? error->message : "unknown error");
        g_clear_error(&error);
        g_free(binary);
        return;
    }

    stdin_fd = child_stdin;
    stdout_channel = g_io_channel_unix_new(child_stdout);
    g_io_channel_set_encoding(stdout_channel, NULL, NULL);
    g_io_channel_set_flags(stdout_channel, G_IO_FLAG_NONBLOCK, NULL);
    stdout_watch = g_io_add_watch(stdout_channel, G_IO_IN | G_IO_HUP | G_IO_ERR,
                                  on_stdout, NULL);
    child_watch = g_child_watch_add(core_pid, on_child_exit, NULL);

    app_logger_log("CoreBridge: pob-core started (%s)", binary);
    g_free(binary);
}

void core_bridge_stop(void) {
    if (stdout_watch) {
        g_source_remove(stdout_watch);
        stdout_watch = 0;
    }
    if (child_watch) {
        g_source_remove(child_watch);
        child_watch = 0;
    }
    if (stdout_channel) {
        g_io_channel_shutdown(stdout_channel, FALSE, NULL);
        g_io_channel_unref(stdout_channel);
        stdout_channel = NULL;
    }
    g_mutex_lock(&write_mutex);
    if (stdin_fd >= 0) {
        close(stdin_fd); // core exits on stdin EOF
        stdin_fd = -1;
    }
    g_mutex_unlock(&write_mutex);
    core_pid = 0;
}
