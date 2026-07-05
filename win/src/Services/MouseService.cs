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

    private static void SetVirtualPos(double x, double y)
    {
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
            _virtualX += dx;
            _virtualY += dy;
            x = _virtualX;
            y = _virtualY;
        }
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
                Thread.Sleep(16);
            }
            ButtonEvent(right: false, press: false);
            RestorePointer(sx, sy);
        }

        SetVirtualPos(endX, endY);
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
    private const ushort VK_CONTROL = 0x11;

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

    // Special-key names accepted by the core's keyPress tool; extended = the
    // key sits on the navigation cluster and needs KEYEVENTF_EXTENDEDKEY.
    private static readonly Dictionary<string, (ushort Vk, bool Extended)> PlainKeys = new()
    {
        ["return"] = (0x0D, false), ["enter"] = (0x0D, false),
        ["tab"] = (0x09, false), ["space"] = (0x20, false),
        ["delete"] = (0x08, false), ["backspace"] = (0x08, false),
        ["escape"] = (0x1B, false), ["esc"] = (0x1B, false),
        ["left"] = (0x25, true), ["up"] = (0x26, true),
        ["right"] = (0x27, true), ["down"] = (0x28, true),
        ["home"] = (0x24, true), ["end"] = (0x23, true),
        ["pageup"] = (0x21, true), ["pagedown"] = (0x22, true),
        ["f1"] = (0x70, false), ["f2"] = (0x71, false), ["f3"] = (0x72, false),
        ["f4"] = (0x73, false), ["f5"] = (0x74, false), ["f6"] = (0x75, false),
        ["f7"] = (0x76, false), ["f8"] = (0x77, false), ["f9"] = (0x78, false),
        ["f10"] = (0x79, false), ["f11"] = (0x7A, false), ["f12"] = (0x7B, false),
    };

    // "cmd+<letter>" maps to Ctrl+<letter> — the Windows equivalent of the
    // macOS Command shortcuts (same convention as the Linux shell).
    private static bool ResolveKey(string name, out ushort vk, out bool extended, out bool ctrl)
    {
        vk = 0;
        extended = false;
        ctrl = false;
        if (PlainKeys.TryGetValue(name, out (ushort Vk, bool Extended) entry))
        {
            vk = entry.Vk;
            extended = entry.Extended;
            return true;
        }
        if (name.StartsWith("cmd+") && name.Length == 5 && name[4] >= 'a' && name[4] <= 'z')
        {
            vk = (ushort)('A' + (name[4] - 'a'));
            ctrl = true;
            return true;
        }
        return false;
    }

    private static void DoKeyPress(string key)
    {
        string lower = key.ToLowerInvariant();
        if (!ResolveKey(lower, out ushort vk, out bool extended, out bool ctrl))
        {
            AppLogger.Log($"Unknown key: {key}");
            return;
        }

        uint flags = extended ? NativeMethods.KEYEVENTF_EXTENDEDKEY : 0;
        if (ctrl) NativeMethods.Send(NativeMethods.KeyInput(VK_CONTROL, 0, 0));
        NativeMethods.Send(NativeMethods.KeyInput(vk, 0, flags));
        Thread.Sleep(30); // match macOS: 30 ms hold
        NativeMethods.Send(NativeMethods.KeyInput(vk, 0, flags | NativeMethods.KEYEVENTF_KEYUP));
        if (ctrl) NativeMethods.Send(NativeMethods.KeyInput(VK_CONTROL, 0, NativeMethods.KEYEVENTF_KEYUP));
    }
}
