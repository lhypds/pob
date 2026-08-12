// Carries the windows Pob frames along when the frame itself is dragged.
//
// The overlay is a hole punched over somebody else's desktop: the content area
// is what a screenshot shows and what every click is aimed through, so how the
// frame sits over what is under it is the whole arrangement. Moving the frame
// normally leaves all of that where it was and the arrangement with it — the
// picture slides off the apps it was framing. Carrying moves everything under
// the frame by the frame's own delta instead, and the scene stays as it was
// set up.
//
// This is what the lock turns on, and half of what the lock means: a locked
// frame keeps its size and keeps what it frames, which together are what let a
// macro's coordinates survive the window being nudged (see
// AppState.UpdateWindowLock).
//
// What counts as under the frame is what the frame shows: a window is carried
// when it overlaps the content area at all, the same test that decides whether
// it turns up in a screenshot. A window Pob only shows a corner of is a window
// Pob is framing, which is the ordinary case of a frame parked over part of
// something bigger than itself.
//
// The X11 and macOS halves of this live in linux-x11/src/carry_service.c and
// macos/Sources/Services/CarryService.swift.
using System.Windows.Input;
using System.Windows.Interop;
using System.Windows.Media;
using System.Windows.Threading;
using Pob.Interop;
using Pob.Views;

namespace Pob.Services;

public static class CarryService
{
    // How often a held latch is checked for the end of the drag that took it.
    private static readonly TimeSpan LatchPollInterval = TimeSpan.FromMilliseconds(100);

    // How many of those polls a held button with a frame standing still gives
    // up its latch after anyway. Only a stuck button reading gets this far — it
    // is the backstop that keeps one bad reading from carrying into the next
    // drag.
    private const int LatchIdlePolls = 10;

    // Windows smaller than this on either side are not carried — the 1x1
    // markers some apps park on screen, and the rest of the desktop's
    // degenerate furniture. Anything a person could actually see inside the
    // frame clears it comfortably.
    private const int MinimumWindowSize = 40;

    // A ceiling on how many windows one drag will carry. Each one costs a
    // SetWindowPos per move, and the frame is redrawn as fast as the pointer
    // moves while it is dragged — past some number of windows the drag itself
    // would start to stutter. A frame would have to be parked over most of a
    // full desktop to reach this.
    private const int MaximumCarried = 16;

    public static bool IsEnabled { get; private set; }

    // ── the latch ───────────────────────────────────────────────────────────
    //
    // The windows carried through one drag, and where each of them and the
    // frame stood when that drag began. Every move puts each window at
    // `its origin + (frame now − frame then)` rather than nudging it by each
    // step's delta: a drag is hundreds of moves, and each one rounded back to
    // whole pixels would walk the windows away from the frame a fraction at a
    // time.
    //
    // `_carried` is empty when the search found nothing to take along. That
    // case is latched too — the alternative is enumerating every window on the
    // desktop again on every move of a drag over bare desktop.
    private readonly record struct Carried(IntPtr Window, int X, int Y);

    private static bool _held;
    private static readonly List<Carried> _carried = new(MaximumCarried);
    private static double _frameLeft, _frameTop; // DIPs, as WPF reports
    private static DispatcherTimer? _dragWatch;
    private static bool _movedSincePoll;
    private static int _idlePolls;

    // Where the frame stood before the move now being reported. A latch is
    // anchored there rather than at the frame's current position: by the time
    // the first move of a drag arrives the frame has already left, and
    // anchoring to where it landed would bake that first step in as a permanent
    // offset between the frame and what it carries.
    private static bool _seeded;
    private static double _previousLeft, _previousTop;

    // ── entry points ────────────────────────────────────────────────────────

    public static void SetEnabled(bool enabled)
    {
        if (IsEnabled == enabled) return;
        IsEnabled = enabled;
        // Turning it off mid-drag has to let go of what it was holding, or the
        // rest of that drag would still be carrying it.
        if (!enabled) Release();
    }

    // Seeds the anchor from the window as it now stands. Called once the frame
    // has been restored, so the first drag is measured from where the window
    // actually starts rather than from wherever WPF first put it.
    public static void Seed()
    {
        ToolbarWindow? toolbar = AppState.Toolbar;
        if (toolbar == null) return;
        _previousLeft = toolbar.Left;
        _previousTop = toolbar.Top;
        _seeded = true;
        Release();
    }

    // The frame moved. Called for every step of a drag, so everything here is
    // either cheap or latched.
    public static void FrameMoved()
    {
        ToolbarWindow? toolbar = AppState.Toolbar;
        OverlayWindow? overlay = AppState.Overlay;
        if (toolbar == null || overlay == null) return;

        double left = toolbar.Left;
        double top = toolbar.Top;
        double anchorLeft = _previousLeft, anchorTop = _previousTop;
        bool moved = _seeded && (left != _previousLeft || top != _previousTop);

        // Kept current whether or not anything is carried: switching Carry on
        // mid-session must not measure its first drag from wherever the frame
        // happened to be when the windows were built.
        _previousLeft = left;
        _previousTop = top;
        _seeded = true;

        // Dragging the top or left edge moves the frame as it resizes, and a
        // resize is not a move: it changes what the frame covers rather than
        // where it sits, and the window below is meant to stay put under it.
        //
        // Carry follows drags, hence the held button. A window Windows places
        // on its own — restoring a frame at launch, snapping one back on screen
        // when a monitor goes away, undoing a maximize — moves with nobody
        // holding it, and dragging some app along with that is nobody's intent.
        if (!IsEnabled || !moved || overlay.IsResizing ||
            Mouse.LeftButton != MouseButtonState.Pressed) return;

        double scale = VisualTreeHelper.GetDpi(toolbar).DpiScaleX;

        if (!_held) AcquireLatch(anchorLeft, anchorTop, left, top, scale);
        StartDragWatch();

        // WPF places windows in DIPs and Win32 in physical pixels, so on a
        // scaled display the frame's delta is worth more than its face value by
        // the time it reaches the windows below.
        int dx = (int)Math.Round((left - _frameLeft) * scale);
        int dy = (int)Math.Round((top - _frameTop) * scale);
        foreach (Carried window in _carried)
        {
            NativeMethods.SetWindowPos(window.Window, IntPtr.Zero, window.X + dx, window.Y + dy, 0, 0,
                                       NativeMethods.SWP_NOSIZE | NativeMethods.SWP_NOZORDER |
                                       NativeMethods.SWP_NOACTIVATE);
        }
    }

    // ── the latch ───────────────────────────────────────────────────────────

    private static void Release()
    {
        _dragWatch?.Stop();
        _held = false;
        _carried.Clear();
        _movedSincePoll = false;
        _idlePolls = 0;
    }

    // Holds the latch for exactly as long as the drag that took it.
    //
    // Letting it lapse on a lull instead would quietly change what is being
    // carried halfway through a drag: a frame that pauses and then moves on
    // re-runs the search and picks up whatever has since come under it, so a
    // slow drag across a busy desktop would gather windows as it went. The set
    // is decided once, when the frame is picked up.
    private static void StartDragWatch()
    {
        _movedSincePoll = true;
        if (_dragWatch == null)
        {
            _dragWatch = new DispatcherTimer { Interval = LatchPollInterval };
            _dragWatch.Tick += (_, _) =>
            {
                if (Mouse.LeftButton != MouseButtonState.Pressed)
                {
                    Release();
                    return;
                }
                _idlePolls = _movedSincePoll ? 0 : _idlePolls + 1;
                _movedSincePoll = false;
                if (_idlePolls >= LatchIdlePolls) Release();
            };
        }
        if (!_dragWatch.IsEnabled) _dragWatch.Start();
    }

    // Finds the windows under the frame as the frame stood at the anchor. A
    // latch is always taken — an empty one when there is nothing under the
    // frame to carry — so the search runs once per drag either way.
    private static void AcquireLatch(double anchorLeft, double anchorTop,
                                     double left, double top, double scale)
    {
        _held = true;
        _carried.Clear();
        _frameLeft = anchorLeft;
        _frameTop = anchorTop;

        OverlayWindow? overlay = AppState.Overlay;
        if (overlay == null) return;
        IntPtr overlayHwnd = new WindowInteropHelper(overlay).Handle;
        if (!NativeMethods.GetWindowRect(overlayHwnd, out NativeMethods.RECT content)) return;

        // The search wants the content area where the drag started, not where
        // this first step has already put it: a fast grab can cover half a
        // screen before the first move arrives, by which time the frame may be
        // over something else entirely.
        int backX = (int)Math.Round((anchorLeft - left) * scale);
        int backY = (int)Math.Round((anchorTop - top) * scale);
        content.Left += backX;
        content.Right += backX;
        content.Top += backY;
        content.Bottom += backY;

        CollectWindowsUnder(content);
    }

    // ── finding the window below ────────────────────────────────────────────

    // Every ordinary window overlapping the frame, into `_carried`. EnumWindows
    // walks top-level windows in Z order, front to back, so they arrive in the
    // order they are stacked.
    //
    // A window that may not be moved is passed over rather than ending the
    // walk: it is one window in the frame staying behind, not a reason to leave
    // the rest of them behind with it.
    private static void CollectWindowsUnder(NativeMethods.RECT rect)
    {
        IntPtr toolbarHwnd = AppState.Toolbar == null
            ? IntPtr.Zero : new WindowInteropHelper(AppState.Toolbar).Handle;
        IntPtr overlayHwnd = AppState.Overlay == null
            ? IntPtr.Zero : new WindowInteropHelper(AppState.Overlay).Handle;

        // Held in a local through the call: a delegate marshalled straight into
        // a P/Invoke argument is collectable while the callee still holds the
        // thunk, and this one is called back once per top-level window.
        NativeMethods.EnumWindowsProc callback = (hwnd, _) =>
        {
            if (hwnd == toolbarHwnd || hwnd == overlayHwnd) return true;
            if (!NativeMethods.IsWindowVisible(hwnd) || NativeMethods.IsIconic(hwnd)) return true;
            // Untitled top-level windows are the desktop's plumbing — the
            // invisible message-only and shell windows every session is full of.
            if (NativeMethods.GetWindowTextLength(hwnd) == 0) return true;

            long exStyle = NativeMethods.GetWindowLongPtr(hwnd, NativeMethods.GWL_EXSTYLE).ToInt64();
            if ((exStyle & NativeMethods.WS_EX_TOOLWINDOW) != 0) return true;
            if (IsCloaked(hwnd)) return true;

            if (!VisibleBounds(hwnd, out NativeMethods.RECT bounds)) return true;
            if (bounds.Right - bounds.Left < MinimumWindowSize ||
                bounds.Bottom - bounds.Top < MinimumWindowSize) return true;
            if (!Intersects(bounds, rect)) return true;

            // A maximized window is placed by the shell rather than by whoever
            // asks: moving it leaves it maximized somewhere it does not belong.
            if (NativeMethods.IsZoomed(hwnd)) return true;

            // SetWindowPos places a window in the space GetWindowRect reports —
            // invisible resize border and all. The visible bounds above are
            // only ever used to decide *which* windows these are.
            if (!NativeMethods.GetWindowRect(hwnd, out NativeMethods.RECT frame)) return true;
            _carried.Add(new Carried(hwnd, frame.Left, frame.Top));
            return _carried.Count < MaximumCarried;
        };
        NativeMethods.EnumWindows(callback, IntPtr.Zero);
        GC.KeepAlive(callback);
    }

    // The window's bounds as the compositor draws them, falling back to the
    // window rect on the pre-DWM path where the two are the same thing anyway.
    private static bool VisibleBounds(IntPtr hwnd, out NativeMethods.RECT bounds)
    {
        int size = System.Runtime.InteropServices.Marshal.SizeOf<NativeMethods.RECT>();
        if (NativeMethods.DwmGetWindowAttribute(
                hwnd, NativeMethods.DWMWA_EXTENDED_FRAME_BOUNDS, out bounds, size) == 0)
            return true;
        return NativeMethods.GetWindowRect(hwnd, out bounds);
    }

    private static bool IsCloaked(IntPtr hwnd)
    {
        return NativeMethods.DwmGetWindowAttribute(
                   hwnd, NativeMethods.DWMWA_CLOAKED, out int cloaked, sizeof(int)) == 0
               && cloaked != 0;
    }

    private static bool Intersects(NativeMethods.RECT a, NativeMethods.RECT b) =>
        a.Left < b.Right && b.Left < a.Right && a.Top < b.Bottom && b.Top < a.Bottom;
}
