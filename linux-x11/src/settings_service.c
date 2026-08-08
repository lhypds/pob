#include "settings_service.h"

#include <errno.h>
#include <fcntl.h>
#include <gio/gio.h>
#include <glib/gstdio.h>
#include <json-glib/json-glib.h>
#include <string.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <unistd.h>

// ── project root ────────────────────────────────────────────────────────────

const char *settings_project_root(void) {
    static gchar *root = NULL;
    if (root) return root;

    // All Pob components share ~/.pob — the same root the pob CLI uses.
    root = g_build_filename(g_get_home_dir(), ".pob", NULL);
    g_mkdir_with_parents(root, 0755);
    return root;
}

static gchar *root_path(const char *name) {
    return g_build_filename(settings_project_root(), name, NULL);
}

// ── instance directory ──────────────────────────────────────────────────────

// Exclusive flock on <instance>/.lock, held for the process lifetime. It marks
// the directory as belonging to a running Pob, which is how a second Pob is
// detected — see settings_claim_instance.
//
// Opened exactly once. flock is per open file description, not per process, so
// a second open() of this same file would be refused by the lock this process
// already holds — Pob would find itself and conclude it was already running.
static int instance_lock_fd = -1;

// Takes the flock, or reports that someone else has it. Already holding it
// counts as success: this process is the someone else.
static gboolean acquire_instance_lock(const char *instance_dir) {
    if (instance_lock_fd >= 0) return TRUE;

    gchar *lock_path = g_build_filename(instance_dir, ".lock", NULL);
    int fd = open(lock_path, O_CREAT | O_RDWR, 0644);
    g_free(lock_path);
    // A lock file that won't open is a broken ~/.pob, not a second Pob. Start
    // anyway rather than refuse over something unrelated.
    if (fd < 0) return TRUE;

    // Non-blocking: a held lock is an answer, not something to wait for.
    if (flock(fd, LOCK_EX | LOCK_NB) != 0) {
        close(fd);
        return FALSE;
    }
    instance_lock_fd = fd;
    return TRUE;
}

#define INSTANCE_PREFIX "pb-"

// The file under ~/.pob holding the machine's one instance id. Named in
// capitals like VERSION and SYSTEM: a marker the programs write and read, not
// a file to edit.
#define INSTANCE_POINTER "INSTANCE"

// Reserves a fresh <root>/pb-<uid>/, drawing another ID if that one is taken.
// The ID is "pb-<4 hex>", the same scheme the pico-hid firmware uses for its
// "ph-" board id. The headerbar shows it beside the window buttons.
static gchar *reserve_instance_id(const char *root) {
    for (;;) {
        gchar *id = g_strdup_printf(INSTANCE_PREFIX "%04x", g_random_int() & 0xffff);
        gchar *dir = g_build_filename(root, id, NULL);
        int rc = g_mkdir(dir, 0755);
        g_free(dir);
        if (rc == 0 || errno != EEXIST) return id;
        g_free(id);
    }
}

// The machine's instance id — the same one on every run, recorded in
// ~/.pob/INSTANCE the first time it is worked out. This mirrors the Go core's
// ResolveInstanceID because either side can get there first: the shell
// resolves it to show in the headerbar and passes it to pob-core with
// --instance, but the CLI can reach ~/.pob without a shell at all.
//
// The pointer is the only thing that says which instance this machine is:
// with no readable one a fresh id is drawn and a new directory reserved,
// rather than an existing pb-* directory adopted. Deleting the file is
// therefore a way to start clean, and what is already there stays as history.
static gchar *resolve_instance_id(const char *root) {
    gchar *pointer = root_path(INSTANCE_POINTER);
    gchar *contents = NULL;
    if (g_file_get_contents(pointer, &contents, NULL, NULL)) {
        gchar *id = g_strstrip(contents);
        // Anything that isn't an instance id — a truncated or hand-edited
        // file — sends us back to working it out, rather than into a
        // directory named after junk.
        if (g_str_has_prefix(id, INSTANCE_PREFIX) && !strchr(id, '/')) {
            gchar *resolved = g_strdup(id);
            g_free(contents);
            g_free(pointer);
            return resolved;
        }
        g_free(contents);
    }

    gchar *id = reserve_instance_id(root);
    gchar *line = g_strconcat(id, "\n", NULL);
    g_file_set_contents(pointer, line, -1, NULL);
    g_free(line);
    g_free(pointer);
    return id;
}

// Claims this machine's instance for this process and reports whether it was
// free; FALSE means another Pob already holds it. Called at startup, before
// the window is built, since only one Pob drives a desktop.
//
// Claiming and asking are the same operation on purpose. Asking first and
// taking it after would leave a gap for a second Pob to slip through — and,
// because flock belongs to the open file description rather than the process,
// the asking itself would collide with the lock this process had already
// taken.
gboolean settings_claim_instance(void) {
    const char *root = settings_project_root();
    gchar *id = resolve_instance_id(root);
    gchar *dir = g_build_filename(root, id, NULL);
    g_mkdir_with_parents(dir, 0755);
    gboolean claimed = acquire_instance_lock(dir);
    g_free(dir);
    g_free(id);
    return claimed;
}

// This instance's ~/.pob/<instance>/ directory, holding its settings.json,
// instruction.txt, macro.txt and logs/. Nothing is shared between ids.
const char *settings_instance_id(void) {
    static gchar *instance_id = NULL;
    if (instance_id) return instance_id;

    const char *root = settings_project_root();
    instance_id = resolve_instance_id(root);

    gchar *logs = g_build_filename(root, instance_id, "logs", NULL);
    g_mkdir_with_parents(logs, 0755);
    g_free(logs);

    // Normally already held — settings_claim_instance ran at startup. This is
    // the path for anything that reaches the settings without it.
    gchar *instance_dir = g_build_filename(root, instance_id, NULL);
    acquire_instance_lock(instance_dir);
    g_free(instance_dir);

    return instance_id;
}

// Path of a file in this instance's directory (~/.pob/<instance>/<name>).
static gchar *instance_path(const char *name) {
    return g_build_filename(settings_project_root(), settings_instance_id(), name, NULL);
}

static gchar *settings_file_path(void) {
    return instance_path("settings.json");
}

// ── settings.json helpers ───────────────────────────────────────────────────

static JsonObject *load_settings(JsonParser **parser_out) {
    gchar *path = settings_file_path();
    JsonParser *parser = json_parser_new();
    JsonObject *obj = NULL;
    if (json_parser_load_from_file(parser, path, NULL)) {
        JsonNode *node = json_parser_get_root(parser);
        if (node && JSON_NODE_HOLDS_OBJECT(node)) obj = json_node_get_object(node);
    }
    g_free(path);
    if (!obj) {
        g_object_unref(parser);
        *parser_out = NULL;
        return NULL;
    }
    *parser_out = parser; // keeps obj alive; caller unrefs
    return obj;
}

static gchar *load_string_key(const char *key, const char *fallback) {
    JsonParser *parser = NULL;
    JsonObject *obj = load_settings(&parser);
    gchar *value = NULL;
    if (obj && json_object_has_member(obj, key)) {
        const gchar *s = json_object_get_string_member_with_default(obj, key, fallback);
        value = g_strdup(s ? s : fallback);
    } else {
        value = g_strdup(fallback);
    }
    if (parser) g_object_unref(parser);
    return value;
}

gboolean settings_get_window_frame(int *x, int *y, int *w, int *h) {
    JsonParser *parser = NULL;
    JsonObject *obj = load_settings(&parser);
    gboolean ok = FALSE;
    if (obj &&
        json_object_has_member(obj, "window_x") &&
        json_object_has_member(obj, "window_y") &&
        json_object_has_member(obj, "window_width") &&
        json_object_has_member(obj, "window_height")) {
        *x = (int)json_object_get_double_member_with_default(obj, "window_x", 0);
        *y = (int)json_object_get_double_member_with_default(obj, "window_y", 0);
        *w = (int)json_object_get_double_member_with_default(obj, "window_width", 600);
        *h = (int)json_object_get_double_member_with_default(obj, "window_height", 400);
        ok = TRUE;
    }
    if (parser) g_object_unref(parser);
    return ok;
}

void settings_save_window_frame(int x, int y, int w, int h) {
    gchar *path = settings_file_path();

    // Preserve every existing key, only replace the frame values.
    JsonParser *parser = NULL;
    JsonObject *existing = load_settings(&parser);

    JsonBuilder *builder = json_builder_new();
    json_builder_begin_object(builder);
    if (existing) {
        GList *members = json_object_get_members(existing);
        for (GList *l = members; l; l = l->next) {
            const gchar *key = l->data;
            if (g_str_equal(key, "window_x") || g_str_equal(key, "window_y") ||
                g_str_equal(key, "window_width") || g_str_equal(key, "window_height"))
                continue;
            json_builder_set_member_name(builder, key);
            json_builder_add_value(builder, json_node_copy(json_object_get_member(existing, key)));
        }
        g_list_free(members);
    }
    json_builder_set_member_name(builder, "window_x");
    json_builder_add_double_value(builder, x);
    json_builder_set_member_name(builder, "window_y");
    json_builder_add_double_value(builder, y);
    json_builder_set_member_name(builder, "window_width");
    json_builder_add_double_value(builder, w);
    json_builder_set_member_name(builder, "window_height");
    json_builder_add_double_value(builder, h);
    json_builder_end_object(builder);

    JsonGenerator *gen = json_generator_new();
    json_generator_set_pretty(gen, TRUE);
    json_generator_set_indent(gen, 2);
    JsonNode *node = json_builder_get_root(builder);
    json_generator_set_root(gen, node);
    json_generator_to_file(gen, path, NULL);

    json_node_unref(node);
    g_object_unref(gen);
    g_object_unref(builder);
    if (parser) g_object_unref(parser);
    g_free(path);
}

// ── opening files ───────────────────────────────────────────────────────────

static void spawn_detached(gchar **argv) {
    g_spawn_async(NULL, argv, NULL, G_SPAWN_SEARCH_PATH, NULL, NULL, NULL, NULL);
}

static void open_with_editor(const char *path) {
    gchar *editor = load_string_key("editor", "system");

    if (g_str_equal(editor, "vscode")) {
        gchar *argv[] = {"code", (gchar *)path, NULL};
        spawn_detached(argv);
    } else if (g_str_equal(editor, "zed")) {
        gchar *argv[] = {"zed", (gchar *)path, NULL};
        spawn_detached(argv);
    } else if (g_str_equal(editor, "sublime_text")) {
        gchar *argv[] = {"subl", (gchar *)path, NULL};
        spawn_detached(argv);
    } else if (g_str_equal(editor, "vim")) {
        gchar *terminal = load_string_key("terminal", "system");
        if (g_str_equal(terminal, "konsole")) {
            gchar *argv[] = {"konsole", "-e", "vim", (gchar *)path, NULL};
            spawn_detached(argv);
        } else if (g_str_equal(terminal, "xterm")) {
            gchar *argv[] = {"xterm", "-e", "vim", (gchar *)path, NULL};
            spawn_detached(argv);
        } else { // "system" / "gnome-terminal"
            gchar *argv[] = {"gnome-terminal", "--", "vim", (gchar *)path, NULL};
            spawn_detached(argv);
        }
        g_free(terminal);
    } else { // "system"
        gchar *argv[] = {"xdg-open", (gchar *)path, NULL};
        spawn_detached(argv);
    }
    g_free(editor);
}

static void ensure_file(const char *path) {
    if (!g_file_test(path, G_FILE_TEST_EXISTS))
        g_file_set_contents(path, "", 0, NULL);
}

void settings_open_settings_file(void) {
    gchar *path = settings_file_path();
    ensure_file(path);
    open_with_editor(path);
    g_free(path);
}

void settings_open_instruction_file(void) {
    gchar *path = instance_path("instruction.txt");
    ensure_file(path);
    open_with_editor(path);
    g_free(path);
}

void settings_open_macro_file(void) {
    gchar *path = instance_path("macro.txt");
    ensure_file(path);
    open_with_editor(path);
    g_free(path);
}

void settings_open_app_log(void) {
    gchar *path = root_path("app.log");
    ensure_file(path);
    open_with_editor(path);
    g_free(path);
}

void settings_open_logs_folder(void) {
    gchar *path = instance_path("logs");
    g_mkdir_with_parents(path, 0755);
    gchar *argv[] = {"xdg-open", path, NULL};
    spawn_detached(argv);
    g_free(path);
}

// ── file contents / clearing ────────────────────────────────────────────────

gchar *settings_get_macro(void) {
    gchar *path = instance_path("macro.txt");
    gchar *contents = NULL;
    if (!g_file_get_contents(path, &contents, NULL, NULL)) contents = g_strdup("");
    g_free(path);
    return contents;
}

// Appends one action line, keeping the file newline-terminated. Read-modify-
// write like the macOS shell's appendToMacro: macro.txt is small and the core
// is the only other writer.
void settings_append_macro(const char *line) {
    gchar *contents = settings_get_macro();
    gboolean needs_newline = *contents != '\0' && !g_str_has_suffix(contents, "\n");
    gchar *next = g_strconcat(contents, needs_newline ? "\n" : "", line, "\n", NULL);
    gchar *path = instance_path("macro.txt");
    g_file_set_contents(path, next, -1, NULL);
    g_free(path);
    g_free(next);
    g_free(contents);
}

void settings_clear_macro(void) {
    gchar *path = instance_path("macro.txt");
    g_file_set_contents(path, "", 0, NULL);
    g_free(path);
}

void settings_clear_instruction(void) {
    gchar *path = instance_path("instruction.txt");
    g_file_set_contents(path, "", 0, NULL);
    g_free(path);
}

