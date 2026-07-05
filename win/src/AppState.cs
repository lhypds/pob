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
    public static bool IsClickThrough;
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

    // ── dialogs ─────────────────────────────────────────────────────────────

    public static void ShowMaxStepDialog()
    {
        bool shouldContinue = Dialogs.ShowMaxStep(Toolbar);
        CoreBridge.ResolveMaxStep(shouldContinue);
    }
}
