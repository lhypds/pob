// Virtual cursor state + SendInput-based mouse/keyboard synthesis, mirroring
// the macOS/Linux MouseService. The virtual cursor lives in screenshot pixel
// coordinates (top-left origin) and never touches the real pointer except
// for the brief instant an action is performed (the click must land at the
// target position; the pointer is restored immediately afterwards).
//
// Blocking actions run on a dedicated worker thread so the WPF dispatcher
// stays responsive; the worker answers the pending JSON-RPC request through
// CoreBridge's thread-safe responders.
using System.Collections.Concurrent;
using System.Windows;
using Pob.Interop;

namespace Pob.Services;

public enum MouseJobType
{
    Click,
    RightClick,
    DoubleClick,
    Drag,
    Scroll,
    Type,
    KeyPress,
    Shutdown, // sentinel
}

public static class MouseService
{
    // ── virtual cursor state ────────────────────────────────────────────────

    private static readonly object PosLock = new();
    private static double _virtualX;
    private static double _virtualY;

    public static void GetVirtualPos(out double x, out double y)
    {
        lock (PosLock)
        {
            x = _virtualX;
            y = _virtualY;
        }
    }

    // ClampToWindow holds a position inside the Pob window. Everything the
    // cursor addresses is inside that window — it is what the screenshots show
    // and what the clicks are aimed through — so a position outside it can
    // only act on something nobody asked for and that no screenshot would ever
    // reveal. Relative moves are what make this necessary: a trackpad drag, or
    // a run of nudges from a model, adds up in one direction and walks off the
    // edge.
    //
    // Until a capture context exists there is nothing to clamp to, and the
    // position is left as it is.
    private static void ClampToWindow(ref double x, ref double y)
    {
        ShotContext ctx = ScreenshotService.GetContext();
        if (!ctx.Valid || ctx.Width <= 0 || ctx.Height <= 0) return;
        // The last pixel is Width - 1: a cursor at Width would be the first
        // pixel outside the window.
        x = Math.Clamp(x, 0, ctx.Width - 1);
        y = Math.Clamp(y, 0, ctx.Height - 1);
    }

    private static void SetVirtualPos(double x, double y)
    {
        // Clamped outside the lock: reading the capture context takes a lock of
        // its own, and taking the two in one order here and the other order
        // anywhere else is how a deadlock gets built.
        ClampToWindow(ref x, ref y);
        lock (PosLock)
        {
            _virtualX = x;
            _virtualY = y;
        }
    }

    public static void ResetCursor()
    {
        SetVirtualPos(20, 20);
        AppState.Overlay?.ContentView.CursorTargetChanged(20, 20);
    }

    public static void MoveBy(double dx, double dy)
    {
        double x, y;
        lock (PosLock)
        {
            x = _virtualX + dx;
            y = _virtualY + dy;
        }
        SetVirtualPos(x, y);
        GetVirtualPos(out x, out y); // the clamped position, not the asked-for one
        AppState.Overlay?.ContentView.CursorTargetChanged(x, y);
    }

    // Marshals a display-position update onto the dispatcher (drag ends on
    // the worker thread but the overlay animation is UI-thread only).
    private static void PostDisplayPos(double x, double y)
    {
        Application.Current?.Dispatcher.BeginInvoke(() =>
            AppState.Overlay?.ContentView.CursorTargetChanged(x, y));
    }

    // ── worker thread plumbing ──────────────────────────────────────────────

    private sealed record MouseJob(MouseJobType Type, string Id, double Dx, double Dy, string Text);

    private static readonly BlockingCollection<MouseJob> Jobs = new();
    private static Thread? _worker;

    public static void Enqueue(MouseJobType type, string id, double dx, double dy, string? text)
    {
        Jobs.Add(new MouseJob(type, id, dx, dy, text ?? ""));
    }

    public static void Init()
    {
        _worker = new Thread(WorkerMain) { IsBackground = true, Name = "pob-mouse-worker" };
        _worker.Start();
    }

    public static void Shutdown()
    {
        if (_worker == null) return;
        Jobs.Add(new MouseJob(MouseJobType.Shutdown, "", 0, 0, ""));
        _worker.Join(2000);
        _worker = null;
    }

    private static void WorkerMain()
    {
        foreach (MouseJob job in Jobs.GetConsumingEnumerable())
        {
            if (job.Type == MouseJobType.Shutdown) break;

            switch (job.Type)
            {
                case MouseJobType.Click: DoClick(right: false); break;
                case MouseJobType.RightClick: DoClick(right: true); break;
                case MouseJobType.DoubleClick: DoDoubleClick(); break;
                case MouseJobType.Drag: DoDrag(job.Dx, job.Dy); break;
                case MouseJobType.Scroll: DoScroll(job.Dx, job.Dy); break;
                case MouseJobType.Type: DoType(job.Text); break;
                case MouseJobType.KeyPress: DoKeyPress(job.Text); break;
            }

            // Mouse actions answer with the (possibly updated) cursor position;
            // keyboard actions answer with an empty result — same as macOS/Linux.
            if (job.Type == MouseJobType.Type || job.Type == MouseJobType.KeyPress)
                CoreBridge.RespondEmpty(job.Id);
            else
                CoreBridge.RespondPosition(job.Id);
        }
    }

    // ── mouse primitives (worker thread) ────────────────────────────────────

    // Converts the virtual cursor (screenshot pixels) to screen coordinates
    // using the most recent capture context. Returns false when no screenshot
    // has been taken yet — the action is skipped, matching the other shells.
    private static bool ToScreen(double px, double py, out int sx, out int sy)
    {
        ShotContext ctx = ScreenshotService.GetContext();
        sx = ctx.OriginX + (int)px;
        sy = ctx.OriginY + (int)py;
        return ctx.Valid;
    }

    private static void SavePointer(out int x, out int y)
    {
        NativeMethods.GetCursorPos(out NativeMethods.POINT pt);
        x = pt.X;
        y = pt.Y;
    }

    private static void RestorePointer(int x, int y) => NativeMethods.SetCursorPos(x, y);

    private static void ButtonEvent(bool right, bool press)
    {
        uint flag = right
            ? (press ? NativeMethods.MOUSEEVENTF_RIGHTDOWN : NativeMethods.MOUSEEVENTF_RIGHTUP)
            : (press ? NativeMethods.MOUSEEVENTF_LEFTDOWN : NativeMethods.MOUSEEVENTF_LEFTUP);
        NativeMethods.Send(NativeMethods.MouseInput(flag));
    }

    private static void DoClick(bool right)
    {
        GetVirtualPos(out double px, out double py);
        if (!ToScreen(px, py, out int rx, out int ry)) return;

        SavePointer(out int sx, out int sy);
        NativeMethods.SetCursorPos(rx, ry);
        ButtonEvent(right, press: true);
        Thread.Sleep(50); // match macOS: 50 ms between down and up
        ButtonEvent(right, press: false);
        RestorePointer(sx, sy);
    }

    private static void DoDoubleClick()
    {
        GetVirtualPos(out double px, out double py);
        if (!ToScreen(px, py, out int rx, out int ry)) return;

        SavePointer(out int sx, out int sy);
        NativeMethods.SetCursorPos(rx, ry);
        for (int i = 0; i < 2; i++)
        {
            ButtonEvent(right: false, press: true);
            Thread.Sleep(30);
            ButtonEvent(right: false, press: false);
            if (i == 0) Thread.Sleep(50);
        }
        RestorePointer(sx, sy);
    }

    private static void DoDrag(double dx, double dy)
    {
        GetVirtualPos(out double px, out double py);
        double endX = px + dx, endY = py + dy;

        if (ToScreen(px, py, out int rx, out int ry) && ToScreen(endX, endY, out int ex, out int ey))
        {
            SavePointer(out int sx, out int sy);
            NativeMethods.SetCursorPos(rx, ry);
            ButtonEvent(right: false, press: true);
            Thread.Sleep(50);
            const int steps = 20; // match macOS: 20 interpolated moves, ~16 ms apart
            for (int i = 1; i <= steps; i++)
            {
                double t = (double)i / steps;
                NativeMethods.SetCursorPos(rx + (int)((ex - rx) * t), ry + (int)((ey - ry) * t));
                // Keep the overlay arrow tracking the real pointer so the two
                // don't show as separate cursors during the drag.
                PostDisplayPos(px + (endX - px) * t, py + (endY - py) * t);
                Thread.Sleep(16);
            }
            ButtonEvent(right: false, press: false);
            RestorePointer(sx, sy);
        }

        SetVirtualPos(endX, endY);
        // Read it back rather than reusing endX/endY: a drag ending past the
        // window edge is held at the edge, and the drawn cursor has to agree
        // with where the cursor actually is.
        GetVirtualPos(out endX, out endY);
        PostDisplayPos(endX, endY);
    }

    private static void DoScroll(double dx, double dy)
    {
        GetVirtualPos(out double px, out double py);
        if (!ToScreen(px, py, out int rx, out int ry)) return;

        SavePointer(out int sx, out int sy);
        NativeMethods.SetCursorPos(rx, ry);

        // Windows scrolls in wheel notches (120 units); ~40 px per notch
        // approximates the macOS pixel-unit scroll amounts.
        int vClicks = (int)(Math.Abs(dy) / 40.0);
        int hClicks = (int)(Math.Abs(dx) / 40.0);
        if (dy != 0 && vClicks < 1) vClicks = 1;
        if (dx != 0 && hClicks < 1) hClicks = 1;

        // dy > 0 = scroll down = negative wheel delta; dx > 0 = right = positive.
        uint vDelta = dy > 0 ? unchecked((uint)-120) : 120;
        uint hDelta = dx > 0 ? 120 : unchecked((uint)-120);

        for (int i = 0; i < vClicks; i++)
        {
            NativeMethods.Send(NativeMethods.MouseInput(NativeMethods.MOUSEEVENTF_WHEEL, vDelta));
            Thread.Sleep(10);
        }
        for (int i = 0; i < hClicks; i++)
        {
            NativeMethods.Send(NativeMethods.MouseInput(NativeMethods.MOUSEEVENTF_HWHEEL, hDelta));
            Thread.Sleep(10);
        }
        RestorePointer(sx, sy);
    }

    // ── keyboard synthesis ──────────────────────────────────────────────────

    private const ushort VK_RETURN = 0x0D;
    private const ushort VK_SHIFT = 0x10;
    private const ushort VK_CONTROL = 0x11;
    private const ushort VK_MENU = 0x12; // Alt
    private const ushort VK_LWIN = 0x5B;

    private static void TapVk(ushort vk, bool extended)
    {
        uint flags = extended ? NativeMethods.KEYEVENTF_EXTENDEDKEY : 0;
        NativeMethods.Send(NativeMethods.KeyInput(vk, 0, flags));
        NativeMethods.Send(NativeMethods.KeyInput(vk, 0, flags | NativeMethods.KEYEVENTF_KEYUP));
    }

    // KEYEVENTF_UNICODE types any character regardless of the keyboard layout
    // (CJK included) — surrogate pairs are sent as consecutive UTF-16 units.
    private static void DoType(string text)
    {
        foreach (char ch in text)
        {
            if (ch == '\r') continue;
            if (ch == '\n')
            {
                TapVk(VK_RETURN, extended: false);
                Thread.Sleep(12);
                continue;
            }
            NativeMethods.Send(NativeMethods.KeyInput(0, ch, NativeMethods.KEYEVENTF_UNICODE));
            NativeMethods.Send(NativeMethods.KeyInput(0, ch,
                NativeMethods.KEYEVENTF_UNICODE | NativeMethods.KEYEVENTF_KEYUP));
            if (!char.IsHighSurrogate(ch)) Thread.Sleep(12);
        }
    }

    // Key names accepted by the core's keyPress tool, as virtual-key codes;
    // extended = the key sits on the navigation cluster or the right-hand side
    // of the board and needs KEYEVENTF_EXTENDEDKEY to be told apart from its
    // keypad twin.
    //
    // A name is a *position* on the board rather than a character — "slash" is
    // wherever the layout puts it — so the active layout decides what actually
    // gets typed. That is what lets a keyboard elsewhere forward keys verbatim.
    private static readonly Dictionary<string, (ushort Vk, bool Extended)> PlainKeys = new()
    {
        ["return"] = (0x0D, false), ["enter"] = (0x0D, false),
        ["tab"] = (0x09, false), ["space"] = (0x20, false),
        ["delete"] = (0x08, false), ["backspace"] = (0x08, false),
        ["forwarddelete"] = (0x2E, true), ["insert"] = (0x2D, true),
        ["escape"] = (0x1B, false), ["esc"] = (0x1B, false),
        ["left"] = (0x25, true), ["up"] = (0x26, true),
        ["right"] = (0x27, true), ["down"] = (0x28, true),
        ["home"] = (0x24, true), ["end"] = (0x23, true),
        ["pageup"] = (0x21, true), ["pagedown"] = (0x22, true),
        ["capslock"] = (0x14, false), ["printscreen"] = (0x2C, true),
        ["scrolllock"] = (0x91, false), ["pause"] = (0x13, false),
        ["menu"] = (0x5D, true),

        ["minus"] = (0xBD, false), ["equals"] = (0xBB, false),
        ["leftbracket"] = (0xDB, false), ["rightbracket"] = (0xDD, false),
        ["backslash"] = (0xDC, false), ["semicolon"] = (0xBA, false),
        ["quote"] = (0xDE, false), ["grave"] = (0xC0, false),
        ["comma"] = (0xBC, false), ["period"] = (0xBE, false),
        ["slash"] = (0xBF, false),
    };

    // Modifiers a chord may hold. "cmd" keeps meaning Ctrl here — the Windows
    // equivalent of the macOS Command shortcuts, and what a macro or an MCP
    // call has always meant by one. "gui" is the other thing you might mean:
    // the physical key beside the space bar, which on this machine is Windows.
    private static readonly Dictionary<string, ushort> Modifiers = new()
    {
        ["cmd"] = VK_CONTROL, ["command"] = VK_CONTROL,
        ["ctrl"] = VK_CONTROL, ["control"] = VK_CONTROL,
        ["alt"] = VK_MENU, ["option"] = VK_MENU, ["opt"] = VK_MENU,
        ["shift"] = VK_SHIFT,
        ["gui"] = VK_LWIN, ["win"] = VK_LWIN, ["super"] = VK_LWIN, ["meta"] = VK_LWIN,
    };

    private static bool IsExtendedModifier(ushort vk) => vk == VK_LWIN;

    /// Resolves "escape", "cmd+v" or "ctrl+alt+shift+f5" into the key to press
    /// and the modifiers to hold while pressing it. Everything before the last
    /// "+" is a modifier; the last part is the key.
    private static bool ResolveKey(string key, out ushort vk, out bool extended, out List<ushort> modifiers)
    {
        vk = 0;
        extended = false;
        modifiers = new List<ushort>();

        string[] parts = key.Split('+', StringSplitOptions.RemoveEmptyEntries);
        if (parts.Length == 0) return false;

        string name = parts[^1];
        if (PlainKeys.TryGetValue(name, out (ushort Vk, bool Extended) entry))
        {
            vk = entry.Vk;
            extended = entry.Extended;
        }
        else if (name.Length == 1 && name[0] >= 'a' && name[0] <= 'z')
        {
            vk = (ushort)('A' + (name[0] - 'a'));
        }
        else if (name.Length == 1 && name[0] >= '0' && name[0] <= '9')
        {
            vk = name[0];
        }
        else if (name.Length >= 2 && name[0] == 'f'
                 && int.TryParse(name.AsSpan(1), out int n) && n >= 1 && n <= 24)
        {
            vk = (ushort)(0x70 + n - 1); // VK_F1 … VK_F24 run consecutively
        }
        else
        {
            return false;
        }

        for (int i = 0; i < parts.Length - 1; i++)
        {
            if (!Modifiers.TryGetValue(parts[i], out ushort mod)) return false;
            if (!modifiers.Contains(mod)) modifiers.Add(mod);
        }
        return true;
    }

    private static void DoKeyPress(string key)
    {
        string lower = key.ToLowerInvariant();
        if (!ResolveKey(lower, out ushort vk, out bool extended, out List<ushort> modifiers))
        {
            AppLogger.Log($"Unknown key: {key}");
            return;
        }

        uint flags = extended ? NativeMethods.KEYEVENTF_EXTENDEDKEY : 0;
        foreach (ushort mod in modifiers)
        {
            NativeMethods.Send(NativeMethods.KeyInput(mod, 0,
                IsExtendedModifier(mod) ? NativeMethods.KEYEVENTF_EXTENDEDKEY : 0));
        }
        NativeMethods.Send(NativeMethods.KeyInput(vk, 0, flags));
        Thread.Sleep(30); // match macOS: 30 ms hold
        NativeMethods.Send(NativeMethods.KeyInput(vk, 0, flags | NativeMethods.KEYEVENTF_KEYUP));
        // Released in reverse, so a modifier never outlives one held under it.
        for (int i = modifiers.Count - 1; i >= 0; i--)
        {
            uint modFlags = NativeMethods.KEYEVENTF_KEYUP;
            if (IsExtendedModifier(modifiers[i])) modFlags |= NativeMethods.KEYEVENTF_EXTENDEDKEY;
            NativeMethods.Send(NativeMethods.KeyInput(modifiers[i], 0, modFlags));
        }
    }
}
