// Shared application state and cross-module entry points for the Pob
// Windows shell. Mirrors the macOS/Linux shells: a translucent always-on-top
// overlay whose perception/operation primitives are driven by the Go core
// (pob-core) over line-delimited JSON-RPC on stdin/stdout.
//
// Windows-specific: click-through (WS_EX_TRANSPARENT) is a per-window flag,
// so the shell is split into a toolbar window (always interactive) and a
// glued content overlay window — visually one window, like the input-shape
// region trick on X11.
using System.IO;
using System.Windows.Media;
using Pob.Services;
using Pob.Views;

namespace Pob;

public static class AppState
{
    // Colors copied from the macOS shell (SwiftUI system palette) so all
    // platforms render identically.
    public static readonly Color GrayOverlay = Color.FromArgb(51, 142, 142, 147); // Color.gray @ 0.2
    public static readonly Color Accent = Color.FromRgb(0x00, 0x7A, 0xFF);        // #007AFF systemBlue
    public static readonly Color RecordRed = Color.FromRgb(0xFF, 0x3B, 0x30);     // #FF3B30 systemRed

    public static ToolbarWindow? Toolbar;
    public static OverlayWindow? Overlay;

    public static bool IsTargeting;
    public static bool IsCropping;
    // Click-through defaults to ON: the overlay sits on top of the app being
    // driven, so passing clicks through is the useful resting state. The
    // toolbar window stays interactive either way.
    public static bool IsClickThrough = true;
    public static bool IsLocked;
    public static bool IsRecording;
    public static bool IsExecuting;

    // ── version ─────────────────────────────────────────────────────────────

    private static string? _version;

    public static string Version => _version ??= ReadVersion();

    private static string ReadVersion()
    {
        // Project root first (dev workflow), then next to the executable
        // (packaged install), then relative to the dev build output
        // (win/bin/<cfg>/<tfm>/Pob.exe -> ../../../../VERSION).
        string exeDir = AppContext.BaseDirectory;
        string[] candidates =
        {
            Path.Combine(SettingsService.ProjectRoot, "VERSION"),
            Path.Combine(exeDir, "VERSION"),
            Path.GetFullPath(Path.Combine(exeDir, "..", "..", "..", "..", "VERSION")),
        };
        foreach (string path in candidates)
        {
            try
            {
                if (!File.Exists(path)) continue;
                string version = File.ReadAllText(path).Trim();
                if (version.Length > 0) return version;
            }
            catch (IOException)
            {
            }
        }
        return "0.0.0";
    }

    // ── mode / state transitions ────────────────────────────────────────────

    public static void UpdateClickThrough()
    {
        // Pass clicks through the content area (toolbar stays interactive)
        // when the user enabled click-through, or while executing — the
        // synthesized clicks must reach the window below the overlay.
        // Targeting and cropping need the content clickable, so they win.
        bool pass = (IsClickThrough || IsExecuting) && !IsTargeting && !IsCropping;
        Overlay?.SetHitTestTransparent(pass);
    }

    public static void UpdateWindowLock()
    {
        // Locked (or executing): OverlayWindow / ToolbarWindow consult
        // IsMoveResizeLocked before starting a drag or an edge resize.
    }

    public static bool IsMoveResizeLocked => IsLocked || IsExecuting;

    public static void SetTargeting(bool targeting)
    {
        IsTargeting = targeting;
        if (targeting) IsCropping = false;
        Toolbar?.SyncModeVisuals();
        UpdateClickThrough();
        Overlay?.ContentView.UpdateCursorStyle();
    }

    public static void SetCropping(bool cropping)
    {
        IsCropping = cropping;
        if (cropping) IsTargeting = false;
        Toolbar?.SyncModeVisuals();
        UpdateClickThrough();
        Overlay?.ContentView.UpdateCursorStyle();
    }

    public static void SetExecuting(bool executing)
    {
        IsExecuting = executing;
        if (executing) Overlay?.ContentView.ResetAnim();
        Toolbar?.SetExecutingVisual(executing);
        // Don't hold keyboard focus while the agent drives other windows.
        Toolbar?.SetNoActivate(executing);
        Overlay?.SetNoActivate(executing);
        UpdateWindowLock();
        UpdateClickThrough();
        Overlay?.ContentView.InvalidateVisual();
    }

    // Something outside the agent loop — an MCP client, a browser on the Pob
    // server — has started driving this instance.
    public static void SetMcpDriving(bool driving)
    {
        // Park the cursor at its home position so it is visible the moment the
        // server comes up, rather than sitting on the top-left corner where it
        // reads as "no cursor at all" — and start the overlay there too, so it
        // does not sit at a stale spot until the client's first move.
        if (!driving) return;
        MouseService.ResetCursor();
        Overlay?.ContentView.ResetAnim();
    }

    // ── server address ──────────────────────────────────────────────────────

    // The dashboard: the server's bare root, which answers with the index page
    // — what is running here, and links on to the control and view pages. The
    // address the core reports names the machine in its path, and a GET there
    // is a picture of the screen rather than somewhere to land, so the path is
    // dropped and the origin is what the instance badge hands out. Null while
    // the server is off.
    public static string? DashboardUrl { get; private set; }

    public static void SetServerUrl(string? url)
    {
        DashboardUrl = url == null ? null
            : Uri.TryCreate(url, UriKind.Absolute, out Uri? parsed)
                ? parsed.GetLeftPart(UriPartial.Authority)
                : url;
        Toolbar?.SyncInstanceBadge();
    }

    // ── dialogs ─────────────────────────────────────────────────────────────

    public static void ShowMaxStepDialog()
    {
        bool shouldContinue = Dialogs.ShowMaxStep(Toolbar);
        CoreBridge.ResolveMaxStep(shouldContinue);
    }
}
