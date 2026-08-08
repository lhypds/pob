// UI-side view of the files Pob works with, mirroring the macOS/Linux
// SettingsService. The Go core owns settings.json defaults, instruction.txt,
// macro.txt and the logs tree; this service only resolves the project root,
// opens files in the user's editor, persists the window frame and clears
// user files on request.
//
// What an instance owns lives under ~/.pob/<instance>/; settings.json sits
// above them at ~/.pob and is shared — pointing ~/.pob/INSTANCE at another id
// starts Pob on a clean instruction and macro, on a machine already set up.
using System.Diagnostics;
using System.IO;
using System.Text.Json;
using System.Text.Json.Nodes;

namespace Pob.Services;

public static class SettingsService
{
    // ── project root ────────────────────────────────────────────────────────

    private static string? _root;

    public static string ProjectRoot => _root ??= ComputeRoot();

    private static string ComputeRoot()
    {
        // All Pob components share ~/.pob — the same root the pob CLI uses.
        string root = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".pob");
        Directory.CreateDirectory(root);
        return root;
    }

    private static string RootPath(string name) => Path.Combine(ProjectRoot, name);

    // ── instance directory ──────────────────────────────────────────────────

    private static string? _instanceId;

    /// <summary>
    /// ~/.pob/&lt;InstanceId&gt;, everything this instance owns: its
    /// instruction.txt, macro.txt and logs/. Passed to pob-core via
    /// --instance.
    /// </summary>
    public static string InstanceId => _instanceId ??= AllocateInstance();

    /// <summary>
    /// Exclusive handle on &lt;InstanceId&gt;/.lock, held for the process
    /// lifetime. It marks the directory as belonging to a running Pob, which
    /// is how a second Pob is detected — see ClaimInstance.
    ///
    /// Opened exactly once. The share mode is enforced per handle, not per
    /// process, so opening this same file a second time would be refused by
    /// the handle this process already holds — Pob would find itself and
    /// conclude it was already running.
    /// </summary>
    private static FileStream? _instanceLock;

    private const string InstancePrefix = "pb-";

    /// <summary>
    /// The file under ~/.pob holding the machine's one instance id. Named in
    /// capitals like VERSION and SYSTEM: a marker the programs write and read,
    /// not a file to edit.
    /// </summary>
    private const string InstancePointer = "INSTANCE";

    private static string AllocateInstance()
    {
        string id = ResolveInstanceId(ProjectRoot);
        string dir = Path.Combine(ProjectRoot, id);
        Directory.CreateDirectory(Path.Combine(dir, "logs"));
        AcquireInstanceLock(dir);
        return id;
    }

    /// <summary>~/.pob/&lt;instance&gt;, everything this instance owns.</summary>
    public static string InstanceDir => Path.Combine(ProjectRoot, InstanceId);

    private static string InstancePath(string name) => Path.Combine(InstanceDir, name);

    /// <summary>
    /// The machine's instance id — the same one on every run, recorded in
    /// ~/.pob/INSTANCE the first time it is worked out. This mirrors the Go
    /// core's ResolveInstanceID because either side can get there first: the
    /// shell resolves it to show in the toolbar and passes it to pob-core
    /// with --instance, but the CLI can reach ~/.pob without a shell at all.
    ///
    /// The pointer is the only thing that says which instance this machine
    /// is: with no readable one a fresh id is drawn and a new directory
    /// reserved, rather than an existing pb-* directory adopted. Deleting the
    /// file is therefore a way to start clean, and what is already there stays
    /// as history.
    /// </summary>
    private static string ResolveInstanceId(string root)
    {
        string pointer = RootPath(InstancePointer);
        try
        {
            string id = File.ReadAllText(pointer).Trim();
            // Anything that isn't an instance id — a truncated or hand-edited
            // file — sends us back to working it out, rather than into a
            // directory named after junk.
            if (id.StartsWith(InstancePrefix, StringComparison.Ordinal) &&
                id.IndexOfAny([Path.DirectorySeparatorChar, Path.AltDirectorySeparatorChar]) < 0)
            {
                return id;
            }
        }
        catch (IOException)
        {
        }
        catch (UnauthorizedAccessException)
        {
        }

        string resolved = ReserveInstanceId(root);
        try
        {
            File.WriteAllText(pointer, resolved + Environment.NewLine);
        }
        catch (IOException)
        {
        }
        return resolved;
    }

    /// <summary>
    /// Reserves a fresh pb-&lt;4 hex&gt; directory — the last two bytes of a new
    /// UID as lowercase hex, the same scheme the pico-hid firmware uses for
    /// its ph- board id. The toolbar shows it beside the window buttons, so
    /// the id on screen names the directory to look in.
    /// </summary>
    private static string ReserveInstanceId(string root)
    {
        string id = NewInstanceId();
        while (Directory.Exists(Path.Combine(root, id))) id = NewInstanceId();
        return id;
    }

    private static string NewInstanceId() => InstancePrefix + Guid.NewGuid().ToString("N")[28..];

    /// <summary>
    /// Claims this machine's instance for this process and reports whether it
    /// was free; false means another Pob already holds it. Called at launch,
    /// before any window is built, since only one Pob drives a desktop.
    ///
    /// Claiming and asking are the same operation on purpose. Asking first
    /// and taking it after would leave a gap for a second Pob to slip
    /// through — and, because the share mode belongs to the handle rather
    /// than the process, the asking itself would collide with the handle this
    /// process had already opened.
    /// </summary>
    public static bool ClaimInstance()
    {
        string dir = Path.Combine(ProjectRoot, ResolveInstanceId(ProjectRoot));
        Directory.CreateDirectory(dir);
        return AcquireInstanceLock(dir);
    }

    /// <summary>
    /// The machine's settings, shared by every instance: the API key, the model
    /// and the port are how this machine works whichever instance it is
    /// running, so moving ~/.pob/INSTANCE does not mean setting them again.
    /// </summary>
    private static string SettingsFilePath() => Path.Combine(ProjectRoot, "settings.json");

    /// <summary>
    /// What this instance is rather than how it is configured: its id, the name
    /// `pob new` gave it, when it last ran and, while it runs, the pid and
    /// control port. The Go core owns the file; the window frame is the shell's
    /// one entry, since where the window was is a property of the machine
    /// rather than something anybody sets.
    /// </summary>
    private static string InstanceFilePath() => InstancePath("instance.json");

    // ── settings.json helpers ───────────────────────────────────────────────

    private static JsonObject? LoadSettings() => LoadJson(SettingsFilePath());

    private static JsonObject? LoadInstance() => LoadJson(InstanceFilePath());

    private static JsonObject? LoadJson(string path)
    {
        try
        {
            return JsonNode.Parse(File.ReadAllText(path)) as JsonObject;
        }
        catch
        {
            return null;
        }
    }

    private static string LoadStringKey(string key, string fallback)
    {
        JsonObject? obj = LoadSettings();
        if (obj != null && obj.TryGetPropertyValue(key, out JsonNode? node) && node is JsonValue v &&
            v.TryGetValue(out string? s) && s != null)
            return s;
        return fallback;
    }

    /// <summary>
    /// The frame comes from instance.json, or from settings.json where it used
    /// to be kept. Both are read because either side can get here first on the
    /// run that moves it: pob-core carries the frame over at startup, and this
    /// is called as the window is built. The fallback goes quiet once core has
    /// run, which drops the keys from settings.json.
    /// </summary>
    public static bool GetWindowFrame(out int x, out int y, out int w, out int h)
    {
        if (ReadFrame(LoadInstance(), out x, out y, out w, out h)) return true;
        return ReadFrame(LoadSettings(), out x, out y, out w, out h);
    }

    private static bool ReadFrame(JsonObject? obj, out int x, out int y, out int w, out int h)
    {
        x = y = 0;
        w = 600;
        h = 400;
        if (obj == null) return false;
        if (!TryGetInt(obj, "window_x", out x) || !TryGetInt(obj, "window_y", out y) ||
            !TryGetInt(obj, "window_width", out w) || !TryGetInt(obj, "window_height", out h))
            return false;
        return true;
    }

    private static bool TryGetInt(JsonObject obj, string key, out int value)
    {
        value = 0;
        if (!obj.TryGetPropertyValue(key, out JsonNode? node) || node is not JsonValue v) return false;
        if (v.TryGetValue(out double d))
        {
            value = (int)d;
            return true;
        }
        return false;
    }

    public static void SaveWindowFrame(int x, int y, int w, int h)
    {
        // Preserve every existing key, only replace the frame values.
        JsonObject obj = LoadInstance() ?? new JsonObject();
        obj["window_x"] = (double)x;
        obj["window_y"] = (double)y;
        obj["window_width"] = (double)w;
        obj["window_height"] = (double)h;
        try
        {
            File.WriteAllText(InstanceFilePath(),
                obj.ToJsonString(new JsonSerializerOptions { WriteIndented = true }));
        }
        catch (IOException)
        {
        }
    }

    // ── opening files ───────────────────────────────────────────────────────

    private static void SpawnDetached(string fileName, params string[] args)
    {
        try
        {
            var psi = new ProcessStartInfo(fileName)
            {
                UseShellExecute = false,
                CreateNoWindow = true,
            };
            foreach (string a in args) psi.ArgumentList.Add(a);
            Process.Start(psi);
        }
        catch (Exception e)
        {
            AppLogger.Log($"Failed to launch {fileName}: {e.Message}");
        }
    }

    private static void OpenWithEditor(string path)
    {
        string editor = LoadStringKey("editor", "system");

        switch (editor)
        {
            case "vscode":
                // VS Code's CLI is code.cmd — go through cmd so PATH lookup works.
                SpawnDetached("cmd.exe", "/c", "code", path);
                break;
            case "zed":
                SpawnDetached("cmd.exe", "/c", "zed", path);
                break;
            case "sublime_text":
                SpawnDetached("cmd.exe", "/c", "subl", path);
                break;
            case "vim":
                string terminal = LoadStringKey("terminal", "system");
                if (terminal == "wt" || terminal == "windows_terminal")
                    SpawnDetached("wt.exe", "vim", path);
                else // "system": a plain console window
                    SpawnDetached("cmd.exe", "/c", "start", "vim", path);
                break;
            default: // "system": the file-type association (xdg-open equivalent)
                try
                {
                    Process.Start(new ProcessStartInfo(path) { UseShellExecute = true });
                }
                catch
                {
                    SpawnDetached("notepad.exe", path); // .log & friends without an association
                }
                break;
        }
    }

    private static void EnsureFile(string path)
    {
        if (!File.Exists(path)) File.WriteAllText(path, "");
    }

    public static void OpenSettingsFile()
    {
        string path = SettingsFilePath();
        EnsureFile(path);
        OpenWithEditor(path);
    }

    public static void OpenInstructionFile()
    {
        string path = InstancePath("instruction.txt");
        EnsureFile(path);
        OpenWithEditor(path);
    }

    public static void OpenMacroFile()
    {
        string path = InstancePath("macro.txt");
        EnsureFile(path);
        OpenWithEditor(path);
    }

    public static void OpenAppLog()
    {
        string path = RootPath("app.log");
        EnsureFile(path);
        OpenWithEditor(path);
    }

    public static void OpenLogsFolder()
    {
        string path = InstancePath("logs");
        Directory.CreateDirectory(path);
        SpawnDetached("explorer.exe", path);
    }

    // ── file contents / clearing ────────────────────────────────────────────

    public static string GetMacro()
    {
        try
        {
            return File.ReadAllText(InstancePath("macro.txt"));
        }
        catch
        {
            return "";
        }
    }

    // Appends one action line, keeping the file newline-terminated. Read-
    // modify-write like the macOS shell's appendToMacro: macro.txt is small
    // and the core is the only other writer.
    public static void AppendToMacro(string line)
    {
        string content = GetMacro();
        if (content.Length > 0 && !content.EndsWith("\n")) content += "\n";
        try
        {
            File.WriteAllText(InstancePath("macro.txt"), content + line + "\n");
        }
        catch (IOException)
        {
        }
    }

    public static void ClearMacro() => TryTruncate(InstancePath("macro.txt"));

    public static void ClearInstruction() => TryTruncate(InstancePath("instruction.txt"));

    /// <summary>
    /// Takes the lock handle, or reports that someone else has it. Already
    /// holding it counts as success — this process is the someone else.
    /// </summary>
    private static bool AcquireInstanceLock(string instanceDir)
    {
        if (_instanceLock != null) return true;
        try
        {
            _instanceLock = new FileStream(Path.Combine(instanceDir, ".lock"),
                FileMode.OpenOrCreate, FileAccess.ReadWrite, FileShare.None);
            return true;
        }
        catch (IOException)
        {
            // A sharing violation is another Pob. Anything else here is a
            // broken ~/.pob, but both look the same from out here and
            // refusing to start is the safer reading of the two.
            return false;
        }
        catch (UnauthorizedAccessException)
        {
            // Permissions, not a second Pob. Start anyway rather than refuse
            // over something unrelated.
            return true;
        }
    }

    private static void TryTruncate(string path)
    {
        try
        {
            File.WriteAllText(path, "");
        }
        catch (IOException)
        {
        }
    }
}
