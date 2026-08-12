#include "settings_service.h"
#include "app_logger.h"
#include "content_view.h"

#include <errno.h>
#include <fcntl.h>
#include <gio/gio.h>
#include <glib/gstdio.h>
#include <json-glib/json-glib.h>
#include <string.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <sys/wait.h>
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

// This instance's ~/.pob/<instance>/ directory, holding its src/ and
// logs/. The machine's settings.json sits above them, at the
// root, and is the one thing every id shares.
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

// This instance's macros (~/.pob/<instance>/src). A macro of any size is
// written across several files — the entry point calls the pieces — so they are
// kept together in one directory, which is what the Macro PSL button opens.
static gchar *src_path(void) {
    return instance_path("src");
}

// The entry point of this instance's macro. `.macro.psl` says psl fills its
// slots; a `.macro` beside it is replayed without the compiler.
static gchar *macro_path(void) {
    gchar *src = src_path();
    gchar *path = g_build_filename(src, "main.macro.psl", NULL);
    g_free(src);
    return path;
}

// The core makes src/ at startup; this is for the writes that can land before
// it has, and costs a stat on a directory that is already there.
static void ensure_src_dir(void) {
    gchar *src = src_path();
    g_mkdir_with_parents(src, 0755);
    g_free(src);
}

// The machine's settings, shared by every instance: the API key, the model and
// the port are how this machine works whichever instance it is running, so
// moving ~/.pob/INSTANCE does not mean setting them again.
static gchar *settings_file_path(void) {
    return root_path("settings.json");
}

// What this instance is rather than how it is configured: its id, the name
// `pob new` gave it, when it last ran and, while it runs, the pid and control
// port. The Go core owns the file; the window frame is the shell's one entry,
// since where the window was is a property of the machine rather than
// something anybody sets.
static gchar *instance_file_path(void) {
    return instance_path("instance.json");
}

// ── settings.json helpers ───────────────────────────────────────────────────

// Takes ownership of path.
static JsonObject *load_json_file(gchar *path, JsonParser **parser_out) {
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

static JsonObject *load_settings(JsonParser **parser_out) {
    return load_json_file(settings_file_path(), parser_out);
}

static JsonObject *load_instance(JsonParser **parser_out) {
    return load_json_file(instance_file_path(), parser_out);
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

static gboolean read_frame(JsonObject *obj, int *x, int *y, int *w, int *h) {
    if (!obj ||
        !json_object_has_member(obj, "window_x") ||
        !json_object_has_member(obj, "window_y") ||
        !json_object_has_member(obj, "window_width") ||
        !json_object_has_member(obj, "window_height"))
        return FALSE;
    *x = (int)json_object_get_double_member_with_default(obj, "window_x", 0);
    *y = (int)json_object_get_double_member_with_default(obj, "window_y", 0);
    *w = (int)json_object_get_double_member_with_default(obj, "window_width", 600);
    *h = (int)json_object_get_double_member_with_default(obj, "window_height", 400);
    return TRUE;
}

// The frame comes from instance.json, or from settings.json where it used to
// be kept. Both are read because either side can get here first on the run
// that moves it: pob-core carries the frame over at startup, and this is
// called as the window is built. The fallback goes quiet once core has run,
// which drops the keys from settings.json.
gboolean settings_get_window_frame(int *x, int *y, int *w, int *h) {
    JsonParser *parser = NULL;
    gboolean ok = read_frame(load_instance(&parser), x, y, w, h);
    if (parser) g_object_unref(parser);
    if (ok) return TRUE;

    parser = NULL;
    ok = read_frame(load_settings(&parser), x, y, w, h);
    if (parser) g_object_unref(parser);
    return ok;
}

// Opens a rewrite of instance.json holding every key it already has except
// `skip` — a NULL-terminated list of the ones the caller is about to write.
// The rest of the file is the core's: the id, the name, the times it keeps, and
// the port it advertises while it runs, none of which the shell may drop.
// The object is left open for those members; finish_instance_write closes it.
static JsonBuilder *begin_instance_write(const char *const *skip, JsonParser **parser_out) {
    JsonParser *parser = NULL;
    JsonObject *existing = load_instance(&parser);

    JsonBuilder *builder = json_builder_new();
    json_builder_begin_object(builder);
    if (existing) {
        GList *members = json_object_get_members(existing);
        for (GList *l = members; l; l = l->next) {
            const gchar *key = l->data;
            gboolean replaced = FALSE;
            for (const char *const *s = skip; *s && !replaced; s++)
                replaced = g_str_equal(key, *s);
            if (replaced) continue;
            json_builder_set_member_name(builder, key);
            json_builder_add_value(builder, json_node_copy(json_object_get_member(existing, key)));
        }
        g_list_free(members);
    }
    *parser_out = parser; // keeps `existing` alive until the write is done
    return builder;
}

static void finish_instance_write(JsonBuilder *builder, JsonParser *parser) {
    json_builder_end_object(builder);

    gchar *path = instance_file_path();
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

void settings_save_window_frame(int x, int y, int w, int h) {
    static const char *const frame_keys[] = {"window_x", "window_y", "window_width",
                                             "window_height", NULL};
    JsonParser *parser = NULL;
    JsonBuilder *builder = begin_instance_write(frame_keys, &parser);
    json_builder_set_member_name(builder, "window_x");
    json_builder_add_double_value(builder, x);
    json_builder_set_member_name(builder, "window_y");
    json_builder_add_double_value(builder, y);
    json_builder_set_member_name(builder, "window_width");
    json_builder_add_double_value(builder, w);
    json_builder_set_member_name(builder, "window_height");
    json_builder_add_double_value(builder, h);
    finish_instance_write(builder, parser);
}

gboolean settings_get_window_locked(void) {
    JsonParser *parser = NULL;
    JsonObject *obj = load_instance(&parser);
    gboolean locked = obj && json_object_get_boolean_member_with_default(obj, "is_locked", FALSE);
    if (parser) g_object_unref(parser);
    return locked;
}

void settings_save_window_locked(gboolean locked) {
    static const char *const lock_keys[] = {"is_locked", NULL};
    JsonParser *parser = NULL;
    JsonBuilder *builder = begin_instance_write(lock_keys, &parser);
    json_builder_set_member_name(builder, "is_locked");
    json_builder_add_boolean_value(builder, locked);
    finish_instance_write(builder, parser);
}

// ── opening files ───────────────────────────────────────────────────────────

// One way of opening a file: a program, and the arguments that go in front of
// the path.
//
// `dispatcher` marks the programs that do not open the file themselves but
// hand it to whatever the desktop has registered for it — xdg-open and gio.
// They exit straight away, non-zero when nothing was registered, and that exit
// status is the only sign that no window appeared. A real editor is the
// opposite: it stays running with the file on screen, and the status it exits
// with whenever the user closes it says nothing about whether the file opened.
typedef struct {
    const char *program;
    const char *args[4]; // NULL-terminated, placed before the path
    gboolean dispatcher;
} open_command;

// The system's own way of opening a text file, in the order it is tried: the
// desktop's file association first, then the editors the common desktops ship.
// A machine can have no xdg-open at all, or an xdg-open with nothing
// registered for .txt and .json — a bare X session, a container, a trimmed
// down install — and the toolbar button still has to put the file on screen.
// macOS never needs this list: `open -t` always has TextEdit behind it.
static const open_command SYSTEM_EDITORS[] = {
    {"xdg-open", {NULL}, TRUE},
    {"gio", {"open", NULL}, TRUE},
    {"gnome-text-editor", {NULL}, FALSE},
    {"gedit", {NULL}, FALSE},
    {"kate", {NULL}, FALSE},
    {"kwrite", {NULL}, FALSE},
    {"mousepad", {NULL}, FALSE},
    {"xed", {NULL}, FALSE},
    {"pluma", {NULL}, FALSE},
    {"leafpad", {NULL}, FALSE},
};

// The same idea for a directory: the file managers the common desktops ship,
// behind the association.
static const open_command FILE_MANAGERS[] = {
    {"xdg-open", {NULL}, TRUE},
    {"gio", {"open", NULL}, TRUE},
    {"nautilus", {NULL}, FALSE},
    {"dolphin", {NULL}, FALSE},
    {"thunar", {NULL}, FALSE},
    {"nemo", {NULL}, FALSE},
    {"caja", {NULL}, FALSE},
    {"pcmanfm", {NULL}, FALSE},
};

// settings.json's editor names and the command each one opens a file with.
// Any other value — "system", or a name nothing matches — starts at
// SYSTEM_EDITORS. vim is not here: it needs a terminal window to live in,
// which is a setting of its own.
static const struct {
    const char *name;
    open_command command;
} EDITORS[] = {
    {"vscode", {"code", {NULL}, FALSE}},
    {"zed", {"zed", {NULL}, FALSE}},
    {"sublime_text", {"subl", {NULL}, FALSE}},
};

// The terminals vim can be opened in, in the order they are tried when the
// configured one is absent. gnome-terminal leads because "system" means it.
static const open_command VIM_TERMINALS[] = {
    {"gnome-terminal", {"--", "vim", NULL}, FALSE},
    {"konsole", {"-e", "vim", NULL}, FALSE},
    {"xterm", {"-e", "vim", NULL}, FALSE},
    {"x-terminal-emulator", {"-e", "vim", NULL}, FALSE},
};

// A walk down a chain of open_commands. It outlives the call that started it:
// a dispatcher's failure only shows up when the child exits, so the rest of
// the chain has to be waited for on the main loop rather than run in a loop.
typedef struct {
    open_command *chain; // heap copy — the head is built from settings.json
    guint count;
    guint index;
    gchar *path;
    const char *what; // "text editor" / "file manager", for the failure message
} open_attempt;

static void run_open_attempt(open_attempt *attempt);

static void open_attempt_free(open_attempt *attempt) {
    g_free(attempt->chain);
    g_free(attempt->path);
    g_free(attempt);
}

static void on_open_exit(GPid pid, gint status, gpointer data) {
    open_attempt *attempt = data;
    g_spawn_close_pid(pid);

    if (WIFEXITED(status) && WEXITSTATUS(status) == 0) {
        open_attempt_free(attempt);
        return;
    }

    // xdg-open exits 3 when nothing is registered for the file and 4 when the
    // program it picked failed to start. Either way no window appeared, so
    // carry on down the chain from where run_open_attempt left off.
    app_logger_log("Settings: %s did not open %s (status %d) — trying the next one",
                   attempt->chain[attempt->index - 1].program, attempt->path, status);
    run_open_attempt(attempt);
}

// Runs the chain from its current position, skipping programs that are not
// installed, until one of them starts. Consumes the attempt.
static void run_open_attempt(open_attempt *attempt) {
    for (; attempt->index < attempt->count; attempt->index++) {
        const open_command *cmd = &attempt->chain[attempt->index];

        // Asking the PATH first is what makes the fall-through work for the
        // editors: g_spawn_async reports a missing program too, but only
        // after the fork, and this keeps both answers in one place.
        gchar *program = g_find_program_in_path(cmd->program);
        if (!program) continue;

        gchar *argv[8];
        int n = 0;
        argv[n++] = program;
        for (int i = 0; cmd->args[i]; i++) argv[n++] = (gchar *)cmd->args[i];
        argv[n++] = attempt->path;
        argv[n] = NULL;

        GSpawnFlags flags = G_SPAWN_SEARCH_PATH;
        // Only a dispatcher's exit status is worth waiting for; anything else
        // GLib may reap itself.
        if (cmd->dispatcher) flags |= G_SPAWN_DO_NOT_REAP_CHILD;

        GPid pid = 0;
        GError *error = NULL;
        gboolean spawned = g_spawn_async(NULL, argv, NULL, flags, NULL, NULL,
                                         cmd->dispatcher ? &pid : NULL, &error);
        g_free(program);

        if (!spawned) {
            app_logger_log("Settings: cannot run %s: %s", cmd->program,
                           error ? error->message : "unknown error");
            if (error) g_error_free(error);
            continue;
        }

        if (!cmd->dispatcher) {
            open_attempt_free(attempt);
            return;
        }
        attempt->index++; // where on_open_exit resumes if nothing opened
        g_child_watch_add(pid, on_open_exit, attempt);
        return;
    }

    // Nothing on this machine could open the file. A toolbar button that does
    // nothing at all just looks broken, so say so on screen and in the log.
    app_logger_log("Settings: no %s on this machine — cannot open %s",
                   attempt->what, attempt->path);
    gchar *msg = g_strdup_printf("Cannot open it — no %s on this machine", attempt->what);
    content_view_show_message(msg);
    g_free(msg);
    open_attempt_free(attempt);
}

// Takes a copy of the chain, so callers can build the head of one on the
// stack, and walks it.
static void start_open(const open_command *chain, guint count, const char *path,
                       const char *what) {
    open_attempt *attempt = g_new0(open_attempt, 1);
    attempt->chain = g_new0(open_command, count);
    for (guint i = 0; i < count; i++) attempt->chain[i] = chain[i];
    attempt->count = count;
    attempt->path = g_strdup(path);
    attempt->what = what;
    run_open_attempt(attempt);
}

// The editor named in settings.json, with the system's own openers behind it:
// an editor that is not installed — the setting was carried over from another
// machine, or the app was never installed here in the first place — should
// leave the file opening in whatever this machine does have, not leave the
// button dead.
static void open_with_editor(const char *path) {
    gchar *editor = load_string_key("editor", "system");

    open_command chain[G_N_ELEMENTS(VIM_TERMINALS) + G_N_ELEMENTS(SYSTEM_EDITORS)];
    guint n = 0;

    // vim is looked up here rather than in the chain: what the chain runs is
    // the terminal, and a terminal that opens on a "vim: not found" is worse
    // than the system editor.
    gchar *vim = g_find_program_in_path("vim");
    if (g_str_equal(editor, "vim") && vim) {
        gchar *terminal = load_string_key("terminal", "system");
        // The configured terminal first, then the rest of the list, so a KDE
        // box with konsole and no gnome-terminal still gets vim.
        for (int preferred = 1; preferred >= 0; preferred--)
            for (guint i = 0; i < G_N_ELEMENTS(VIM_TERMINALS); i++)
                if (g_str_equal(terminal, VIM_TERMINALS[i].program) == (preferred == 1))
                    chain[n++] = VIM_TERMINALS[i];
        g_free(terminal);
    } else {
        for (guint i = 0; i < G_N_ELEMENTS(EDITORS); i++)
            if (g_str_equal(editor, EDITORS[i].name)) {
                chain[n++] = EDITORS[i].command;
                break;
            }
    }
    g_free(vim);
    g_free(editor);

    for (guint i = 0; i < G_N_ELEMENTS(SYSTEM_EDITORS); i++) chain[n++] = SYSTEM_EDITORS[i];

    start_open(chain, n, path, "text editor");
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

// Opens the whole src/ directory rather than the entry point alone: what
// someone reaches for the Macro PSL button to do is edit the macro, and a macro
// is the set of files, not the one that happens to be called first.
void settings_open_src_folder(void) {
    gchar *path = src_path();
    g_mkdir_with_parents(path, 0755);
    start_open(FILE_MANAGERS, G_N_ELEMENTS(FILE_MANAGERS), path, "file manager");
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
    start_open(FILE_MANAGERS, G_N_ELEMENTS(FILE_MANAGERS), path, "file manager");
    g_free(path);
}

// ── file contents / clearing ────────────────────────────────────────────────

gchar *settings_get_macro(void) {
    gchar *path = macro_path();
    gchar *contents = NULL;
    if (!g_file_get_contents(path, &contents, NULL, NULL)) contents = g_strdup("");
    g_free(path);
    return contents;
}

// Appends one action line, keeping the file newline-terminated. Read-modify-
// write like the macOS shell's appendToMacro: the macro is small and the core
// is the only other writer.
void settings_append_macro(const char *line) {
    gchar *contents = settings_get_macro();
    gboolean needs_newline = *contents != '\0' && !g_str_has_suffix(contents, "\n");
    gchar *next = g_strconcat(contents, needs_newline ? "\n" : "", line, "\n", NULL);
    ensure_src_dir();
    gchar *path = macro_path();
    g_file_set_contents(path, next, -1, NULL);
    g_free(path);
    g_free(next);
    g_free(contents);
}

void settings_clear_macro(void) {
    ensure_src_dir();
    gchar *path = macro_path();
    g_file_set_contents(path, "", 0, NULL);
    g_free(path);
}

