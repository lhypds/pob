// Spawns and talks to the Go core (pob-core.exe) over stdin/stdout using
// line-delimited JSON-RPC, mirroring the macOS/Linux CoreBridge. The Go side
// owns the agent loop, LLM calls, logs and the MCP server; this bridge
// answers its perception/operation requests (screenshot, mouse, keyboard,
// UI dialogs) and forwards user commands (run / stop / recording) back.
using System.Diagnostics;
using System.IO;
using System.Text;
using System.Text.Json;
using System.Windows;

namespace Pob.Services;

public static class CoreBridge
{
    private static Process? _process;
    private static StreamWriter? _stdin;
    private static readonly object WriteLock = new();


    // ── writing ─────────────────────────────────────────────────────────────

    private static void Send(Dictionary<string, object?> message)
    {
        string json = JsonSerializer.Serialize(message);
        lock (WriteLock)
        {
            if (_stdin == null) return;
            try
            {
                _stdin.Write(json);
                _stdin.Write('\n');
                _stdin.Flush();
            }
            catch (Exception)
            {
                // Broken pipe — the core has exited; the Exited handler cleans up.
            }
        }
    }

    private static void Notify(string method, Dictionary<string, object?> parameters)
    {
        Send(new Dictionary<string, object?>
        {
            ["jsonrpc"] = "2.0",
            ["method"] = method,
            ["params"] = parameters,
        });
    }

    // ── responders (thread-safe) ────────────────────────────────────────────

    public static void RespondPosition(string id)
    {
        MouseService.GetVirtualPos(out double x, out double y);
        Send(new Dictionary<string, object?>
        {
            ["jsonrpc"] = "2.0",
            ["id"] = id,
            ["result"] = new Dictionary<string, object?> { ["x"] = x, ["y"] = y },
        });
    }

    public static void RespondEmpty(string id)
    {
        Send(new Dictionary<string, object?>
        {
            ["jsonrpc"] = "2.0",
            ["id"] = id,
            ["result"] = new Dictionary<string, object?>(),
        });
    }

    // The fallback for a captured frame, used when the frame channel is not
    // up: the picture as base64 on this line, with the sizes beside it.
    public static void RespondImage(string id, string imageBase64, Dictionary<string, object?> meta)
    {
        var result = new Dictionary<string, object?>(meta) { ["image"] = imageBase64 };
        Send(new Dictionary<string, object?>
        {
            ["jsonrpc"] = "2.0",
            ["id"] = id,
            ["result"] = result,
        });
    }

    public static void RespondError(string id, string message)
    {
        Send(new Dictionary<string, object?>
        {
            ["jsonrpc"] = "2.0",
            ["id"] = id,
            ["error"] = new Dictionary<string, object?> { ["code"] = -32603, ["message"] = message },
        });
    }

    // ── commands (shell -> Go) ──────────────────────────────────────────────

    public static void RunMacro()
    {
        Notify("run.macro", new Dictionary<string, object?>());
    }

    public static void StopExecution()
    {
        Notify("run.stop", new Dictionary<string, object?>());
    }

    public static void RecordingChanged(bool recording)
    {
        Notify("recording.changed", new Dictionary<string, object?> { ["recording"] = recording });
    }

    // Toolbar screenshot button: the Go core flashes, captures and saves the
    // shot, and records a takeScreenshot() macro line while recording.
    public static void TakeScreenshot()
    {
        Notify("screenshot.take", new Dictionary<string, object?>());
    }

    // ── dispatch (UI thread) ────────────────────────────────────────────────

    private static double MemberDouble(JsonElement? obj, string name, double fallback)
    {
        if (obj is JsonElement o && o.ValueKind == JsonValueKind.Object &&
            o.TryGetProperty(name, out JsonElement v) && v.ValueKind == JsonValueKind.Number)
            return v.GetDouble();
        return fallback;
    }

    private static bool MemberBool(JsonElement? obj, string name, bool fallback)
    {
        if (obj is JsonElement o && o.ValueKind == JsonValueKind.Object &&
            o.TryGetProperty(name, out JsonElement v) &&
            (v.ValueKind == JsonValueKind.True || v.ValueKind == JsonValueKind.False))
            return v.GetBoolean();
        return fallback;
    }

    private static string MemberString(JsonElement? obj, string name, string fallback)
    {
        if (obj is JsonElement o && o.ValueKind == JsonValueKind.Object &&
            o.TryGetProperty(name, out JsonElement v) && v.ValueKind == JsonValueKind.String)
            return v.GetString() ?? fallback;
        return fallback;
    }

    private static void Dispatch(JsonElement msg)
    {
        if (msg.ValueKind != JsonValueKind.Object) return;
        string? method = null;
        if (msg.TryGetProperty("method", out JsonElement m) && m.ValueKind == JsonValueKind.String)
            method = m.GetString();
        if (method == null) return;

        JsonElement? parameters = null;
        if (msg.TryGetProperty("params", out JsonElement p) && p.ValueKind == JsonValueKind.Object)
            parameters = p;

        string? id = null;
        if (msg.TryGetProperty("id", out JsonElement i) && i.ValueKind == JsonValueKind.String)
            id = i.GetString();

        switch (method)
        {
            case "session.state":
                AppState.SetExecuting(MemberBool(parameters, "executing", false));
                break;

            case "mcp.state":
                AppState.SetMcpDriving(MemberBool(parameters, "active", false));
                break;

            case "server.state":
            {
                bool running = MemberBool(parameters, "running", false);
                string url = MemberString(parameters, "url", "");
                AppState.SetServerUrl(running && url.Length > 0 ? url : null);
                break;
            }

            case "screenshot.capture":
            {
                if (id == null) return;
                bool withCursor = MemberBool(parameters, "withCursor", true);
                bool hasCrop = false;
                double cx = 0, cy = 0, cw = 0, ch = 0;
                if (parameters is JsonElement po && po.TryGetProperty("crop", out JsonElement cn) &&
                    cn.ValueKind == JsonValueKind.Object)
                {
                    cx = MemberDouble(cn, "x", 0);
                    cy = MemberDouble(cn, "y", 0);
                    cw = MemberDouble(cn, "width", 0);
                    ch = MemberDouble(cn, "height", 0);
                    hasCrop = true;
                }
                string format = MemberString(parameters, "format", "png");
                int maxWidth = (int)MemberDouble(parameters, "maxWidth", 0);
                int quality = (int)MemberDouble(parameters, "quality", 0);
                // Absent means the caller wants Pob out of the picture — the
                // agent's capture, and what every capture was before the view
                // page asked otherwise.
                bool keepWindow = MemberBool(parameters, "keepWindow", false);
                ScreenshotService.HandleCapture(id, withCursor, hasCrop, cx, cy, cw, ch,
                                                format, maxWidth, quality, keepWindow);
                break;
            }

            case "frames.channel":
            {
                int port = (int)MemberDouble(parameters, "port", 0);
                string token = MemberString(parameters, "token", "");
                if (port > 0 && token.Length > 0) FrameChannel.Connect(port, token);
                break;
            }

            case "cursor.reset":
                if (id == null) return;
                MouseService.ResetCursor();
                RespondPosition(id);
                break;

            case "cursor.move":
                if (id == null) return;
                MouseService.MoveBy(MemberDouble(parameters, "dx", 0), MemberDouble(parameters, "dy", 0));
                RespondPosition(id);
                break;

            case "mouse.click":
                if (id != null) MouseService.Enqueue(MouseJobType.Click, id, 0, 0, null);
                break;

            case "mouse.rightClick":
                if (id != null) MouseService.Enqueue(MouseJobType.RightClick, id, 0, 0, null);
                break;

            case "mouse.doubleClick":
                if (id != null) MouseService.Enqueue(MouseJobType.DoubleClick, id, 0, 0, null);
                break;

            case "mouse.drag":
                if (id != null)
                    MouseService.Enqueue(MouseJobType.Drag, id,
                        MemberDouble(parameters, "dx", 0), MemberDouble(parameters, "dy", 0), null);
                break;

            case "mouse.scroll":
                if (id != null)
                    MouseService.Enqueue(MouseJobType.Scroll, id,
                        MemberDouble(parameters, "dx", 0), MemberDouble(parameters, "dy", 0), null);
                break;

            case "keyboard.type":
                if (id != null)
                    MouseService.Enqueue(MouseJobType.Type, id, 0, 0,
                        MemberString(parameters, "text", ""));
                break;

            case "keyboard.keyPress":
                if (id != null)
                    MouseService.Enqueue(MouseJobType.KeyPress, id, 0, 0,
                        MemberString(parameters, "key", ""));
                break;

            case "ui.flash":
                AppState.Overlay?.ContentView.Flash();
                if (id != null) RespondEmpty(id);
                break;

            // The three the `pob` CLI sets. Each does what the toolbar button
            // does; the reply says only that this shell knows the method, which
            // is the answer the terminal is waiting on — an older shell falls
            // through to the error below.
            case "ui.lock":
                AppState.SetLocked(MemberBool(parameters, "locked", false));
                if (id != null) RespondEmpty(id);
                break;

            case "ui.clickThrough":
                AppState.SetClickThrough(MemberBool(parameters, "enabled", false));
                if (id != null) RespondEmpty(id);
                break;

            case "ui.record":
                AppState.SetRecording(MemberBool(parameters, "recording", false));
                if (id != null) RespondEmpty(id);
                break;

            case "ui.alert":
                // Answered before the dialog goes up: it is modal, and the core
                // is not waiting on it — it has already given up on whatever it
                // was asked to do.
                if (id != null) RespondEmpty(id);
                AppState.ShowAlertDialog(
                    MemberString(parameters, "title", "Pob"),
                    MemberString(parameters, "message", ""));
                break;

            default:
                if (id != null) RespondError(id, $"Unknown method: {method}");
                break;
        }
    }

    // ── lifecycle ───────────────────────────────────────────────────────────

    // Packaged install: pob-core.exe sits next to Pob.exe.
    // Dev workflow: walk up from Pob.exe towards the repository root and use
    // <root>\core\bin\pob-core.exe built by win/setup.ps1.
    private static string? LocateCoreBinary()
    {
        string bundled = Path.Combine(AppContext.BaseDirectory, "pob-core.exe");
        if (File.Exists(bundled)) return bundled;

        string? dir = AppContext.BaseDirectory;
        for (int i = 0; i < 6 && dir != null; i++)
        {
            string candidate = Path.Combine(dir, "core", "bin", "pob-core.exe");
            if (File.Exists(candidate)) return candidate;
            dir = Path.GetDirectoryName(dir);
        }
        return null;
    }

    public static void Start()
    {
        string root = SettingsService.ProjectRoot;
        string? binary = LocateCoreBinary();
        if (binary == null)
        {
            AppLogger.Error("CoreBridge: pob-core binary not found — run win\\setup.ps1");
            return;
        }

        var psi = new ProcessStartInfo(binary)
        {
            WorkingDirectory = root,
            RedirectStandardInput = true,
            RedirectStandardOutput = true,
            UseShellExecute = false,
            CreateNoWindow = true,
            StandardInputEncoding = new UTF8Encoding(false),
            StandardOutputEncoding = new UTF8Encoding(false),
        };
        psi.ArgumentList.Add("--root");
        psi.ArgumentList.Add(root);
        psi.ArgumentList.Add("--instance");
        psi.ArgumentList.Add(SettingsService.InstanceId);

        var process = new Process { StartInfo = psi, EnableRaisingEvents = true };
        process.Exited += (_, _) =>
        {
            AppLogger.Event("CoreBridge: pob-core exited");
            Application.Current?.Dispatcher.BeginInvoke(() => AppState.SetExecuting(false));
        };

        try
        {
            process.Start();
        }
        catch (Exception e)
        {
            AppLogger.Error($"CoreBridge: failed to start pob-core: {e.Message}");
            return;
        }

        _process = process;
        _stdin = process.StandardInput;

        var reader = new Thread(() => ReadLoop(process)) { IsBackground = true, Name = "pob-core-reader" };
        reader.Start();

        AppLogger.Event($"CoreBridge: pob-core started ({binary})");
    }

    private static void ReadLoop(Process process)
    {
        try
        {
            string? line;
            while ((line = process.StandardOutput.ReadLine()) != null)
            {
                if (line.Length == 0) continue;
                JsonDocument doc;
                try
                {
                    doc = JsonDocument.Parse(line);
                }
                catch (JsonException)
                {
                    continue;
                }
                Application.Current?.Dispatcher.BeginInvoke(() =>
                {
                    using (doc) Dispatch(doc.RootElement);
                });
            }
        }
        catch (Exception)
        {
            // Stream closed during shutdown.
        }
    }

    public static void Stop()
    {
        lock (WriteLock)
        {
            try
            {
                _stdin?.Close(); // core exits on stdin EOF
            }
            catch (Exception)
            {
            }
            _stdin = null;
        }
        _process = null;
    }
}
