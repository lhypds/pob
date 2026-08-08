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

// Exclusive flock on logs/<instance>/.lock, held for the process lifetime. It
// marks the directory as belonging to a running Pob, which is what
// settings_clear_logs checks — and taking it is also how a second Pob is
// detected, see settings_claim_instance.
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

// TRUE when a live instance still holds the directory's .lock. Entries
// without a lock file (stale instances, stray files) count as not running.
static gboolean instance_is_running(const char *dir_path) {
    gchar *lock_path = g_build_filename(dir_path, ".lock", NULL);
    int fd = open(lock_path, O_RDWR);
    g_free(lock_path);
    if (fd < 0) return FALSE;
    if (flock(fd, LOCK_EX | LOCK_NB) == 0) {
        flock(fd, LOCK_UN);
        close(fd);
        return FALSE;
    }
    close(fd);
    return TRUE;
}

#define INSTANCE_PREFIX "pb-"

// Reserves a fresh logs/pb-<uid>/, drawing another ID if that one is taken.
// The ID is "pb-<4 hex>", the same scheme the pico-hid firmware uses for its
// "ph-" board id. The headerbar shows it beside the window buttons.
static gchar *reserve_instance_id(const char *logs) {
    for (;;) {
        gchar *id = g_strdup_printf(INSTANCE_PREFIX "%04x", g_random_int() & 0xffff);
        gchar *dir = g_build_filename(logs, id, NULL);
        int rc = g_mkdir(dir, 0755);
        g_free(dir);
        if (rc == 0 || errno != EEXIST) return id;
        g_free(id);
    }
}

// The pb-* directory modified last, or NULL when there are none. By
// modification time rather than by name: the directory is touched every time
// a session is written into it, so the newest is the one that was in use.
static gchar *most_recent_instance(const char *logs) {
    GDir *dir = g_dir_open(logs, 0, NULL);
    if (!dir) return NULL;

    gchar *newest = NULL;
    gint64 newest_at = 0;
    const gchar *name;
    while ((name = g_dir_read_name(dir))) {
        if (!g_str_has_prefix(name, INSTANCE_PREFIX)) continue;
        gchar *path = g_build_filename(logs, name, NULL);
        GStatBuf st;
        if (g_stat(path, &st) == 0 && S_ISDIR(st.st_mode) &&
            (!newest || (gint64)st.st_mtime > newest_at)) {
            g_free(newest);
            newest = g_strdup(name);
            newest_at = (gint64)st.st_mtime;
        }
        g_free(path);
    }
    g_dir_close(dir);
    return newest;
}

// The machine's instance id — the same one on every run, recorded in
// ~/.pob/instance the first time it is worked out. This mirrors the Go core's
// ResolveInstanceID because either side can get there first: the shell
// resolves it to show in the headerbar and passes it to pob-core with
// --instance, but the CLI can reach ~/.pob without a shell at all.
//
// A machine upgrading from the versions that took a fresh id per launch has a
// logs/ full of pb-* directories. Rather than add one more, the one used last
// is adopted; the rest stay where they are as history.
static gchar *resolve_instance_id(const char *logs) {
    gchar *pointer = root_path("instance");
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

    gchar *id = most_recent_instance(logs);
    if (!id) id = reserve_instance_id(logs);
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
    gchar *logs = root_path("logs");
    g_mkdir_with_parents(logs, 0755);
    gchar *id = resolve_instance_id(logs);
    gchar *dir = g_build_filename(logs, id, NULL);
    g_mkdir_with_parents(dir, 0755);
    gboolean claimed = acquire_instance_lock(dir);
    g_free(dir);
    g_free(id);
    g_free(logs);
    return claimed;
}

// This instance's logs/<instance>/ directory, seeded with a copy of the root
// settings.json so it reads and edits its own settings. instruction.txt and
// macro.txt stay shared.
const char *settings_instance_id(void) {
    static gchar *instance_id = NULL;
    if (instance_id) return instance_id;

    gchar *logs = root_path("logs");
    g_mkdir_with_parents(logs, 0755);

    instance_id = resolve_instance_id(logs);
    gchar *instance_dir = g_build_filename(logs, instance_id, NULL);
    g_mkdir_with_parents(instance_dir, 0755);
    // Normally already held — settings_claim_instance ran at startup. This is
    // the path for anything that reaches the settings without it.
    acquire_instance_lock(instance_dir);
    g_free(instance_dir);

    // Seed this instance's settings.json from the root template.
    gchar *root_settings = root_path("settings.json");
    gchar *instance_settings = g_build_filename(logs, instance_id, "settings.json", NULL);
    gchar *contents = NULL;
    gsize len = 0;
    if (!g_file_test(instance_settings, G_FILE_TEST_EXISTS) &&
        g_file_get_contents(root_settings, &contents, &len, NULL)) {
        g_file_set_contents(instance_settings, contents, len, NULL);
        g_free(contents);
    }
    g_free(instance_settings);
    g_free(root_settings);
    g_free(logs);
    return instance_id;
}

// Path of this instance's settings.json (logs/<instance>/settings.json).
static gchar *settings_file_path(void) {
    return g_build_filename(settings_project_root(), "logs", settings_instance_id(),
                            "settings.json", NULL);
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
    gchar *path = root_path("instruction.txt");
    ensure_file(path);
    open_with_editor(path);
    g_free(path);
}

void settings_open_macro_file(void) {
    gchar *path = root_path("macro.txt");
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
    gchar *path = root_path("logs");
    g_mkdir_with_parents(path, 0755);
    gchar *argv[] = {"xdg-open", path, NULL};
    spawn_detached(argv);
    g_free(path);
}

// ── file contents / clearing ────────────────────────────────────────────────

gchar *settings_get_macro(void) {
    gchar *path = root_path("macro.txt");
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
    gchar *path = root_path("macro.txt");
    g_file_set_contents(path, next, -1, NULL);
    g_free(path);
    g_free(next);
    g_free(contents);
}

void settings_clear_macro(void) {
    gchar *path = root_path("macro.txt");
    g_file_set_contents(path, "", 0, NULL);
    g_free(path);
}

void settings_clear_instruction(void) {
    gchar *path = root_path("instruction.txt");
    g_file_set_contents(path, "", 0, NULL);
    g_free(path);
}

static void remove_tree(GFile *file) {
    GFileEnumerator *e = g_file_enumerate_children(
        file, G_FILE_ATTRIBUTE_STANDARD_NAME "," G_FILE_ATTRIBUTE_STANDARD_TYPE,
        G_FILE_QUERY_INFO_NOFOLLOW_SYMLINKS, NULL, NULL);
    if (e) {
        GFileInfo *info;
        while ((info = g_file_enumerator_next_file(e, NULL, NULL))) {
            GFile *child = g_file_get_child(file, g_file_info_get_name(info));
            if (g_file_info_get_file_type(info) == G_FILE_TYPE_DIRECTORY)
                remove_tree(child);
            else
                g_file_delete(child, NULL, NULL);
            g_object_unref(child);
            g_object_unref(info);
        }
        g_object_unref(e);
    }
    g_file_delete(file, NULL, NULL);
}

void settings_clear_logs(void) {
    gchar *path = root_path("logs");

    // Delete only directories of instances that are no longer running —
    // every live instance holds a flock on its logs/<instance>/.lock, so a
    // held lock means "in use, skip".
    GDir *dir = g_dir_open(path, 0, NULL);
    if (dir) {
        const gchar *name;
        while ((name = g_dir_read_name(dir))) {
            if (g_strcmp0(name, settings_instance_id()) == 0) continue;
            gchar *child_path = g_build_filename(path, name, NULL);
            if (!instance_is_running(child_path)) {
                GFile *child = g_file_new_for_path(child_path);
                remove_tree(child);
                g_object_unref(child);
            }
            g_free(child_path);
        }
        g_dir_close(dir);
    }

    // Wipe this instance's own logs, carrying over its live settings.json.
    // The .lock goes down with the directory, so re-acquire it after.
    gchar *settings_path = settings_file_path();
    gchar *settings_data = NULL;
    gsize settings_len = 0;
    g_file_get_contents(settings_path, &settings_data, &settings_len, NULL);

    if (instance_lock_fd >= 0) {
        close(instance_lock_fd);
        instance_lock_fd = -1;
    }
    gchar *instance_dir = g_build_filename(path, settings_instance_id(), NULL);
    GFile *own = g_file_new_for_path(instance_dir);
    remove_tree(own);
    g_object_unref(own);
    g_mkdir_with_parents(instance_dir, 0755);
    acquire_instance_lock(instance_dir);
    g_free(instance_dir);
    g_free(path);

    if (settings_data) {
        g_file_set_contents(settings_path, settings_data, settings_len, NULL);
        g_free(settings_data);
    }
    g_free(settings_path);

    gchar *applog = root_path("app.log");
    g_file_set_contents(applog, "", 0, NULL);
    g_free(applog);
}
