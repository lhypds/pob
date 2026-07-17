#include "settings_service.h"

#include <errno.h>
#include <fcntl.h>
#include <gio/gio.h>
#include <glib/gstdio.h>
#include <json-glib/json-glib.h>
#include <string.h>
#include <sys/file.h>
#include <unistd.h>

// ── project root ────────────────────────────────────────────────────────────

const char *settings_project_root(void) {
    static gchar *root = NULL;
    if (root) return root;

    gchar *cwd = g_get_current_dir();

    // Dev workflow: launched from the project root, which has settings.json.
    gchar *settings = g_build_filename(cwd, "settings.json", NULL);
    gboolean has_settings = g_file_test(settings, G_FILE_TEST_EXISTS);
    g_free(settings);
    if (has_settings) {
        root = cwd;
        return root;
    }

    // Also accept a directory that looks like the project source tree.
    gchar *core = g_build_filename(cwd, "core", NULL);
    gboolean has_core = g_file_test(core, G_FILE_TEST_IS_DIR);
    g_free(core);
    if (has_core) {
        root = cwd;
        return root;
    }
    g_free(cwd);

    // Production: launched from a desktop entry — project files live in
    // ~/.pob, the same default the pob CLI falls back to.
    root = g_build_filename(g_get_home_dir(), ".pob", NULL);
    g_mkdir_with_parents(root, 0755);
    return root;
}

static gchar *root_path(const char *name) {
    return g_build_filename(settings_project_root(), name, NULL);
}

// ── instance directory ──────────────────────────────────────────────────────

// Exclusive flock on logs/<instance>/.lock, held for the process lifetime so
// settings_clear_logs (from this or another process) can tell the directory
// belongs to a running instance.
static int instance_lock_fd = -1;

static void acquire_instance_lock(void) {
    gchar *lock_path = g_build_filename(settings_project_root(), "logs",
                                        settings_instance_id(), ".lock", NULL);
    instance_lock_fd = open(lock_path, O_CREAT | O_RDWR, 0644);
    if (instance_lock_fd >= 0) flock(instance_lock_fd, LOCK_EX);
    g_free(lock_path);
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

// Reserves logs/<unixtime>/ exclusively for this process (bumping if another
// instance grabbed the same second, mirroring the Go core's newInstanceID)
// and seeds it with a copy of the root settings.json so the instance reads
// and edits its own settings. instruction.txt and macro.txt stay shared.
const char *settings_instance_id(void) {
    static gchar *instance_id = NULL;
    if (instance_id) return instance_id;

    gchar *logs = root_path("logs");
    g_mkdir_with_parents(logs, 0755);

    gint64 id = g_get_real_time() / G_USEC_PER_SEC;
    for (;;) {
        gchar *dir = g_strdup_printf("%s/%" G_GINT64_FORMAT, logs, id);
        int rc = g_mkdir(dir, 0755);
        g_free(dir);
        if (rc == 0 || errno != EEXIST) break;
        id++;
    }
    instance_id = g_strdup_printf("%" G_GINT64_FORMAT, id);
    acquire_instance_lock();

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
    g_free(instance_dir);
    g_free(path);
    acquire_instance_lock();

    if (settings_data) {
        g_file_set_contents(settings_path, settings_data, settings_len, NULL);
        g_free(settings_data);
    }
    g_free(settings_path);

    gchar *applog = root_path("app.log");
    g_file_set_contents(applog, "", 0, NULL);
    g_free(applog);
}
