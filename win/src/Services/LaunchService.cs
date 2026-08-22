// Opens an application and puts its window in the frame — what a macro's
// launch("notepad") asks for.
//
// The overlay is a hole punched over somebody else's desktop, and every
// position a macro holds is a position inside that hole. Which means every one
// of them was written down while some window sat in a particular place under
// the frame, put there by a person, once. A macro that opens the window itself
// and places it where the frame is does not need that person again — and a
// macro that does not need that person is one a schedule can run.
//
// So this is two jobs, and they are one call because the second needs the
// first. Opening is the shell's; placing is SetWindowPos, the same call
// CarryService moves a window with on every drag. What ties them together is
// the process: the window to place is a window of what was just started, and
// nothing but the side that started it knows which that is.
//
// The macOS and X11 halves of this live in
// macos/Sources/Services/LaunchService.swift and
// linux-x11/src/launch_service.c.
using System.Diagnostics;
using System.IO;
using System.Windows;
using Pob.Interop;

namespace Pob.Services;

public static class LaunchService
{
    // How long a launch waits for the application to put a window on screen.
    //
    // An application already running answers on the first poll, and a cold
    // start of something large — a browser, an office suite, an IDE — is
    // seconds rather than tenths of them on a machine that has other things to
    // do. Twenty is past all of that and still short of a macro looking hung:
    // what usually reaches it is an application that opened no window at all,
    // which is a thing to be told about rather than waited on.
    private static readonly TimeSpan WindowWait = TimeSpan.FromSeconds(20);

    // How often the wait looks. A window that has just appeared is worth
    // finding within a frame or two of appearing, since the statement under
    // this one is about to click into it.
    private static readonly TimeSpan PollInterval = TimeSpan.FromMilliseconds(200);

    // Windows smaller than this on either side are not what was opened — the
    // splash and scratch windows an application puts up while it starts, which
    // appear before the real one and would otherwise be what the frame got.
    private const int MinimumWindowSize = 40;

    // How far the window may end up from what it was asked for and still count
    // as fitted. Rounding between DIPs and physical pixels is worth a pixel on
    // a scaled display, and the invisible resize border is measured rather than
    // guessed; two covers both.
    private const int FitTolerance = 2;

    // ── the request ─────────────────────────────────────────────────────────

    // Opens `target`, fits its window to the content area, and answers the core
    // on the id the request came in on.
    //
    // Called on the UI thread and answers off it. The waiting is up to twenty
    // seconds long and has no business on the thread drawing the overlay; the
    // one thing it comes back for is the frame's geometry, which only the UI
    // thread may read.
    public static void Handle(string id, string target, int gap)
    {
        target = (target ?? string.Empty).Trim();
        if (target.Length == 0)
        {
            CoreBridge.RespondError(id, "launch was given no application to open");
            return;
        }

        Process? started;
        try
        {
            // UseShellExecute, so what may be written in the statement is what
            // may be typed at a Run box: a bare name resolved off PATH and the
            // App Paths registry — "notepad", "firefox" — as well as a full
            // path to an .exe.
            started = Process.Start(new ProcessStartInfo
            {
                FileName = target,
                UseShellExecute = true,
            });
        }
        catch (Exception e)
        {
            CoreBridge.RespondError(id, $"{target} would not open: {e.Message}");
            return;
        }

        // A null process is not a failure. An application whose executable is a
        // stub — a launcher that hands the request to a copy of itself already
        // running and exits — leaves the shell with nothing to hand back, and
        // the window still appears. What is looked for then is a window of a
        // process with the same name, which is the same window either way. The
        // same stub is why the name is read now: it is unreadable the moment
        // the process it belongs to has gone.
        string name = ProcessName(started) ?? Path.GetFileNameWithoutExtension(target);
        if (name.Length == 0) name = target;
        int pid = ProcessId(started);

        Task.Run(() =>
        {
            (bool fitted, string note) = WaitAndFit(started, name, gap);
            CoreBridge.RespondLaunch(id, name, pid, fitted, note);
        });
    }

    // ── waiting ─────────────────────────────────────────────────────────────

    // Waits for a window of the application and fits the first one it gets.
    // Off the UI thread.
    private static (bool, string) WaitAndFit(Process? started, string name, int gap)
    {
        DateTime deadline = DateTime.UtcNow + WindowWait;
        while (true)
        {
            IntPtr hwnd = WindowOf(started, name);
            if (hwnd != IntPtr.Zero)
            {
                if (!TryContentRect(gap, out NativeMethods.RECT rect))
                    return (false, "Pob's own window is not on screen to fit it to");
                return Fit(hwnd, rect);
            }
            if (DateTime.UtcNow >= deadline)
                return (false, $"{name} put no window on screen within {(int)WindowWait.TotalSeconds} seconds");
            Thread.Sleep(PollInterval);
        }
    }

    // The frame's content area with the launch gap taken off it — the rect the
    // window is actually put in.
    //
    // Read now rather than when the launch started: a cold start is seconds
    // long, and the frame is a window somebody can pick up and move in that
    // time. What the window is fitted to is where the frame is when it is
    // fitted.
    //
    // The gap needs no scaling here: ContentRect answers in physical pixels,
    // which on Windows is also what a screenshot pixel is, so the margin is the
    // number it was written as.
    //
    // A gap is daylight around a window and not a reason to have no window: a
    // frame too small to hold one with the whole margin on gets the margin it
    // can afford, which keeps at least half of the frame in each direction for
    // the window itself.
    private static bool TryContentRect(int gap, out NativeMethods.RECT rect)
    {
        NativeMethods.RECT found = default;
        bool ok = false;
        Application.Current?.Dispatcher.Invoke(() =>
        {
            if (!ScreenshotService.ContentRect(out int x, out int y, out int w, out int h, out _)) return;
            int margin = Math.Max(0, Math.Min(gap, Math.Min(w, h) / 4));
            found = new NativeMethods.RECT
            {
                Left = x + margin,
                Top = y + margin,
                Right = x + w - margin,
                Bottom = y + h - margin,
            };
            ok = true;
        });
        rect = found;
        return ok;
    }

    // ── finding the window ──────────────────────────────────────────────────

    // The application's first window that is worth putting in the frame, or
    // zero while it has none.
    //
    // Whose window counts is the process that was started, and every other
    // process running the same executable. The second half is what catches a
    // launcher that re-execs: the window belongs to a sibling of the process
    // the shell handed back, and by name they are the same application.
    private static IntPtr WindowOf(Process? started, string name)
    {
        var pids = new HashSet<uint>();
        if (started != null && !HasExited(started)) pids.Add((uint)ProcessId(started));
        foreach (Process peer in SafeProcessesByName(name))
        {
            using (peer) pids.Add((uint)peer.Id);
        }
        if (pids.Count == 0) return IntPtr.Zero;

        IntPtr found = IntPtr.Zero;
        // Held in a local through the call: a delegate marshalled straight into
        // a P/Invoke argument is collectable while the callee still holds the
        // thunk, and this one is called back once per top-level window.
        NativeMethods.EnumWindowsProc callback = (hwnd, _) =>
        {
            if (!NativeMethods.IsWindowVisible(hwnd)) return true;
            // Untitled top-level windows are the desktop's plumbing — the
            // invisible message-only and shell windows every process is full
            // of — and a tool window is a palette rather than the application.
            if (NativeMethods.GetWindowTextLength(hwnd) == 0) return true;
            long exStyle = NativeMethods.GetWindowLongPtr(hwnd, NativeMethods.GWL_EXSTYLE).ToInt64();
            if ((exStyle & NativeMethods.WS_EX_TOOLWINDOW) != 0) return true;
            if (IsCloaked(hwnd)) return true;

            NativeMethods.GetWindowThreadProcessId(hwnd, out uint pid);
            if (!pids.Contains(pid)) return true;

            // A minimized window has no size worth reading, and it is about to
            // be restored below — so it passes the test the others take.
            if (!NativeMethods.IsIconic(hwnd))
            {
                if (!NativeMethods.GetWindowRect(hwnd, out NativeMethods.RECT bounds)) return true;
                if (bounds.Right - bounds.Left < MinimumWindowSize ||
                    bounds.Bottom - bounds.Top < MinimumWindowSize) return true;
            }

            found = hwnd;
            return false;
        };
        NativeMethods.EnumWindows(callback, IntPtr.Zero);
        GC.KeepAlive(callback);
        return found;
    }

    // Everything about a Process is unreadable once it has gone, and a launcher
    // stub is gone before the window it asked for is up — so each of these
    // answers "nothing known" rather than throwing under the launch.
    private static bool HasExited(Process process)
    {
        try { return process.HasExited; }
        catch (InvalidOperationException) { return true; }
    }

    private static string? ProcessName(Process? process)
    {
        if (process == null) return null;
        try { return process.ProcessName; }
        catch (Exception) { return null; }
    }

    private static int ProcessId(Process? process)
    {
        if (process == null) return 0;
        try { return process.Id; }
        catch (Exception) { return 0; }
    }

    private static Process[] SafeProcessesByName(string name)
    {
        try { return Process.GetProcessesByName(name); }
        catch (InvalidOperationException) { return Array.Empty<Process>(); }
    }

    private static bool IsCloaked(IntPtr hwnd)
    {
        return NativeMethods.DwmGetWindowAttribute(
                   hwnd, NativeMethods.DWMWA_CLOAKED, out int cloaked, sizeof(int)) == 0
               && cloaked != 0;
    }

    // ── fitting ─────────────────────────────────────────────────────────────

    // Puts the window where the frame is and makes it the size the frame is,
    // and says how close it got.
    private static (bool, string) Fit(IntPtr hwnd, NativeMethods.RECT rect)
    {
        // Maximized and minimized are both states the shell puts a window in
        // rather than sizes it happens to be, and neither will take a place
        // from anybody else. Restoring first is what makes the window ordinary
        // enough to be placed.
        if (NativeMethods.IsZoomed(hwnd) || NativeMethods.IsIconic(hwnd))
        {
            NativeMethods.ShowWindow(hwnd, NativeMethods.SW_RESTORE);
        }
        NativeMethods.SetForegroundWindow(hwnd);

        // SetWindowPos places a window in the space GetWindowRect reports, and
        // that space is bigger than the window looks: some seven pixels of
        // invisible resize border on every side of a normal one. Fitted to the
        // frame means what a person sees fills the frame, so the border is
        // measured and paid for rather than left to hang over the edges.
        (int padLeft, int padTop, int padRight, int padBottom) = InvisibleBorder(hwnd);
        int x = rect.Left - padLeft;
        int y = rect.Top - padTop;
        int w = rect.Right - rect.Left + padLeft + padRight;
        int h = rect.Bottom - rect.Top + padTop + padBottom;

        NativeMethods.SetWindowPos(hwnd, IntPtr.Zero, x, y, w, h,
                                   NativeMethods.SWP_NOZORDER | NativeMethods.SWP_NOACTIVATE);

        if (!VisibleBounds(hwnd, out NativeMethods.RECT now))
            return (false, "its window would not say where it ended up");

        bool placed = Math.Abs(now.Left - rect.Left) <= FitTolerance &&
                      Math.Abs(now.Top - rect.Top) <= FitTolerance;
        if (!placed) return (false, "its window would not move to the frame");

        bool sized = Math.Abs((now.Right - now.Left) - (rect.Right - rect.Left)) <= FitTolerance &&
                     Math.Abs((now.Bottom - now.Top) - (rect.Bottom - rect.Top)) <= FitTolerance;
        if (!sized)
            return (true, $"its window would not resize past {now.Right - now.Left}×{now.Bottom - now.Top}");
        return (true, "");
    }

    // How far the window rect stands outside the window as the compositor draws
    // it, on each side. Zero on the pre-DWM path, where the two are the same
    // thing anyway.
    private static (int, int, int, int) InvisibleBorder(IntPtr hwnd)
    {
        int size = System.Runtime.InteropServices.Marshal.SizeOf<NativeMethods.RECT>();
        if (!NativeMethods.GetWindowRect(hwnd, out NativeMethods.RECT frame)) return (0, 0, 0, 0);
        if (NativeMethods.DwmGetWindowAttribute(
                hwnd, NativeMethods.DWMWA_EXTENDED_FRAME_BOUNDS, out NativeMethods.RECT visible, size) != 0)
            return (0, 0, 0, 0);
        return (visible.Left - frame.Left, visible.Top - frame.Top,
                frame.Right - visible.Right, frame.Bottom - visible.Bottom);
    }

    // The window's bounds as the compositor draws them, falling back to the
    // window rect on the pre-DWM path.
    private static bool VisibleBounds(IntPtr hwnd, out NativeMethods.RECT bounds)
    {
        int size = System.Runtime.InteropServices.Marshal.SizeOf<NativeMethods.RECT>();
        if (NativeMethods.DwmGetWindowAttribute(
                hwnd, NativeMethods.DWMWA_EXTENDED_FRAME_BOUNDS, out bounds, size) == 0)
            return true;
        return NativeMethods.GetWindowRect(hwnd, out bounds);
    }
}
