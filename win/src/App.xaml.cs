// Application lifecycle for the Pob Windows shell, mirroring the Linux
// main.c: builds the toolbar + content overlay pair (visually one window),
// restores/persists the window frame in settings.json (debounced 500 ms),
// and starts the mouse worker and the Go core bridge.
//
// The persisted frame is the combined rect — toolbar top to content bottom —
// in DIP coordinates, like the logical-pixel frame the other shells save.
using System.Windows;
using System.Windows.Media;
using System.Windows.Threading;
using Pob.Interop;
using Pob.Services;
using Pob.Views;

namespace Pob;

public partial class App : Application
{
    // What a brand new instance opens at, before it has a frame of its own.
    //
    // 1024×768 rather than something smaller, because the frame is the screen
    // as far as a macro is concerned: every coordinate a macro holds is a
    // position inside it, and a frame that has to be dragged bigger before the
    // first macro is recorded is a frame whose coordinates were all written
    // against a size nobody meant. It is also exactly the microVM's screen
    // (vm/msb/run.sh), so an instance started here is one that fits when the
    // same ~/.pob is copied in.
    //
    // The same size on the other two shells —
    // macos/Sources/Services/PobInstance.swift and linux-x11/src/main.c.
    private const double StartWidth = 1024;
    private const double StartHeight = 768;

    private ToolbarWindow? _toolbar;
    private OverlayWindow? _overlay;

    private bool _syncing;

    private DispatcherTimer? _saveTimer;
    private bool _lastFrameSeeded;
    private (int X, int Y, int W, int H) _lastFrame;

    private bool _isMaximized;
    private Rect _restoreFrame;

    protected override void OnStartup(StartupEventArgs e)
    {
        base.OnStartup(e);

        // One Pob drives a desktop: there is one pointer and one focused
        // window to drive it with, so a second copy would only fight the
        // first for both.
        if (!SettingsService.ClaimInstance())
        {
            MessageBox.Show(
                "Only one Pob can run at a time — it drives the desktop, and there is one pointer to drive it with. Use the window that is already open.",
                "Pob is already running", MessageBoxButton.OK, MessageBoxImage.Warning);
            Shutdown();
            return;
        }

        // --fullscreen: the overlay covers the whole display and the toolbar
        // window never appears, so nothing on screen is Pob's to click and the
        // `pob` command is what drives it. Read before either window is built,
        // since it decides what is built and where.
        AppState.IsFullscreen = e.Args.Contains("--fullscreen");

        // The lock and the click-through the window was left with, read before
        // the toolbar is built so its buttons start on the right icons. An
        // instance set up for a macro comes back locked rather than movable,
        // and over the app it drives rather than catching its clicks.
        //
        // Fullscreen takes the click-through as read: the window is the whole
        // display, so an instance last left catching clicks would come back
        // having taken the desktop away with nothing on screen to give it back.
        AppState.IsLocked = SettingsService.GetWindowLocked();
        AppState.IsClickThrough = AppState.IsFullscreen || SettingsService.GetClickThrough();
        AppState.UpdateWindowLock();

        var toolbar = new ToolbarWindow();
        var overlay = new OverlayWindow();
        _toolbar = toolbar;
        _overlay = overlay;
        AppState.Toolbar = toolbar;
        AppState.Overlay = overlay;

        // Restore the saved frame, or the starting size centered.
        double x, y, w, h;
        if (SettingsService.GetWindowFrame(out int fx, out int fy, out int fw, out int fh))
        {
            x = fx;
            y = fy;
            w = fw;
            h = fh;
        }
        else
        {
            Rect area = SystemParameters.WorkArea;
            // Less whatever of the starting size this screen has not got: a
            // default big enough to hang off the screen would be a window with
            // its titlebar out of reach on the machines that can least afford
            // it.
            w = Math.Min(StartWidth, area.Width);
            h = Math.Min(StartHeight, area.Height);
            x = area.Left + (area.Width - w) / 2;
            y = area.Top + (area.Height - h) / 2;
        }
        ApplyCombinedFrame(new Rect(x, y, w, h));

        toolbar.Show();
        overlay.Owner = toolbar;
        overlay.Show();
        if (AppState.IsFullscreen) EnterFullscreen();
        else toolbar.Activate();

        // WS_EX_TRANSPARENT needs the overlay HWND, so apply the restored
        // click-through state only once both windows exist.
        AppState.UpdateClickThrough();

        // Keep the overlay out of its own screenshots for the rest of the
        // session. The alternative — hiding it around every capture — is a
        // blink the view page's frame rate turns into a strobe.
        AppLogger.Log(ScreenshotService.EnableCaptureExclusion(overlay)
            ? "Screenshot: overlay excluded from capture (WDA_EXCLUDEFROMCAPTURE)"
            : "Screenshot: WDA_EXCLUDEFROMCAPTURE unavailable — hiding the overlay around each capture instead");

        // Glue: the toolbar is the titlebar, the overlay hangs directly below;
        // moving or resizing either keeps the pair together.
        toolbar.LocationChanged += (_, _) =>
        {
            if (!_syncing)
            {
                _syncing = true;
                overlay.Left = toolbar.Left;
                overlay.Top = toolbar.Top + ToolbarWindow.BarHeight;
                _syncing = false;
                // After the glue, so the pair has already arrived where Carry
                // is about to measure from — and before the save, which only
                // widens the gap between the frame and what it is holding.
                CarryService.FrameMoved();
            }
            ScheduleSaveFrame();
        };
        overlay.LocationChanged += (_, _) =>
        {
            if (!_syncing)
            {
                _syncing = true;
                toolbar.Left = overlay.Left;
                toolbar.Top = overlay.Top - ToolbarWindow.BarHeight;
                _syncing = false;
                CarryService.FrameMoved();
            }
            ScheduleSaveFrame();
        };
        overlay.SizeChanged += (_, _) =>
        {
            if (!_syncing)
            {
                _syncing = true;
                toolbar.Width = overlay.ActualWidth;
                _syncing = false;
            }
            ScheduleSaveFrame();
        };

        // Owned windows hide with a minimized owner; make it explicit.
        toolbar.StateChanged += (_, _) =>
        {
            if (toolbar.WindowState == WindowState.Minimized)
                overlay.Hide();
            else
                overlay.Show();
        };

        toolbar.Closed += (_, _) => Shutdown();

        AppLogger.Event("Pob started");
        MouseService.Init();
        CoreBridge.Start();
    }

    protected override void OnExit(ExitEventArgs e)
    {
        _saveTimer?.Stop();
        SaveFrameNow();
        CoreBridge.Stop();
        MouseService.Shutdown();
        // The other half of "Pob started": app.log is the record of the app
        // coming up and going down, so the way out is written too.
        AppLogger.Event("Pob stopped");
        base.OnExit(e);
    }

    // ── combined frame (toolbar + content) ──────────────────────────────────

    private Rect CombinedFrame()
    {
        if (_toolbar == null || _overlay == null) return Rect.Empty;
        return new Rect(_toolbar.Left, _toolbar.Top, _toolbar.Width,
                        ToolbarWindow.BarHeight + _overlay.Height);
    }

    private void ApplyCombinedFrame(Rect frame)
    {
        if (_toolbar == null || _overlay == null) return;
        _syncing = true;
        _toolbar.Left = frame.Left;
        _toolbar.Top = frame.Top;
        _toolbar.Width = Math.Max(frame.Width, _overlay.MinWidth);
        _overlay.Left = frame.Left;
        _overlay.Top = frame.Top + ToolbarWindow.BarHeight;
        _overlay.Width = Math.Max(frame.Width, _overlay.MinWidth);
        _overlay.Height = Math.Max(frame.Height - ToolbarWindow.BarHeight, _overlay.MinHeight);
        _syncing = false;
        ScheduleSaveFrame();
        // Nothing here is a drag — this is the restored frame at startup, or a
        // maximize — and _syncing kept the glue below from telling Carry about
        // it. Re-anchor, so the next real drag is measured from where the frame
        // ended up rather than from wherever it was before this jump.
        CarryService.Seed();
    }

    // ── fullscreen ──────────────────────────────────────────────────────────

    // The overlay alone, over the whole display: the toolbar window is taken
    // off screen rather than never shown, because an owned window has to be
    // given an owner that has been shown at least once — and with it goes every
    // button Pob has, which is the point of the mode. What drives it from here
    // is the `pob` command.
    //
    // The display is the one the window would have come up on, whole:
    // rcMonitor rather than rcWork, so the taskbar's strip is covered too. The
    // overlay is already Topmost, which is what keeps it there.
    private void EnterFullscreen()
    {
        if (_toolbar == null || _overlay == null) return;

        Rect bounds = CurrentMonitorBounds();
        // _syncing, so the glue below leaves the (about to be hidden) toolbar
        // where it is instead of dragging it across the screen with this.
        _syncing = true;
        _overlay.Left = bounds.Left;
        _overlay.Top = bounds.Top;
        _overlay.Width = bounds.Width;
        _overlay.Height = bounds.Height;
        _syncing = false;
        _overlay.EnterFullscreen();

        _toolbar.Hide();
        // Hiding a window does not hide the windows it owns, but the overlay is
        // the whole of Pob now — cheap insurance against a WPF that disagrees.
        if (!_overlay.IsVisible) _overlay.Show();
    }

    // Full bounds of the monitor the window sits on, in DIP coordinates —
    // CurrentWorkArea's rect without the taskbar taken out of it.
    private Rect CurrentMonitorBounds()
    {
        if (_toolbar == null) return new Rect(0, 0, SystemParameters.PrimaryScreenWidth,
                                              SystemParameters.PrimaryScreenHeight);
        var helper = new System.Windows.Interop.WindowInteropHelper(_toolbar);
        IntPtr monitor = NativeMethods.MonitorFromWindow(helper.Handle,
                                                         NativeMethods.MONITOR_DEFAULTTONEAREST);
        var info = new NativeMethods.MONITORINFO
        {
            cbSize = System.Runtime.InteropServices.Marshal.SizeOf<NativeMethods.MONITORINFO>(),
        };
        if (monitor == IntPtr.Zero || !NativeMethods.GetMonitorInfo(monitor, ref info))
            return new Rect(0, 0, SystemParameters.PrimaryScreenWidth,
                            SystemParameters.PrimaryScreenHeight);

        double scale = VisualTreeHelper.GetDpi(_toolbar).DpiScaleX;
        return new Rect(info.rcMonitor.Left / scale, info.rcMonitor.Top / scale,
                        (info.rcMonitor.Right - info.rcMonitor.Left) / scale,
                        (info.rcMonitor.Bottom - info.rcMonitor.Top) / scale);
    }

    public void ToggleMaximize()
    {
        if (_toolbar == null || _overlay == null) return;

        if (!_isMaximized)
        {
            _restoreFrame = CombinedFrame();
            ApplyCombinedFrame(CurrentWorkArea());
            _isMaximized = true;
        }
        else
        {
            ApplyCombinedFrame(_restoreFrame);
            _isMaximized = false;
        }
        _toolbar.SetMaximizedVisual(_isMaximized);
    }

    // Work area of the monitor the toolbar sits on, in DIP coordinates.
    private Rect CurrentWorkArea()
    {
        if (_toolbar == null) return SystemParameters.WorkArea;
        var helper = new System.Windows.Interop.WindowInteropHelper(_toolbar);
        IntPtr monitor = NativeMethods.MonitorFromWindow(helper.Handle,
                                                         NativeMethods.MONITOR_DEFAULTTONEAREST);
        var info = new NativeMethods.MONITORINFO
        {
            cbSize = System.Runtime.InteropServices.Marshal.SizeOf<NativeMethods.MONITORINFO>(),
        };
        if (monitor == IntPtr.Zero || !NativeMethods.GetMonitorInfo(monitor, ref info))
            return SystemParameters.WorkArea;

        double scale = VisualTreeHelper.GetDpi(_toolbar).DpiScaleX;
        return new Rect(info.rcWork.Left / scale, info.rcWork.Top / scale,
                        (info.rcWork.Right - info.rcWork.Left) / scale,
                        (info.rcWork.Bottom - info.rcWork.Top) / scale);
    }

    // ── window frame persistence ────────────────────────────────────────────

    private void ScheduleSaveFrame()
    {
        if (_saveTimer == null)
        {
            _saveTimer = new DispatcherTimer { Interval = TimeSpan.FromMilliseconds(500) };
            _saveTimer.Tick += (_, _) =>
            {
                _saveTimer!.Stop();
                SaveFrameNow();
            };
        }
        _saveTimer.Stop();
        _saveTimer.Start();
    }

    private void SaveFrameNow()
    {
        if (_toolbar == null || _overlay == null) return;
        if (_toolbar.WindowState == WindowState.Minimized) return;
        // The frame is not saved in fullscreen: it is the display rather than
        // anything the user placed, and writing it down would bring the next
        // ordinary launch up as a window the size of the screen.
        if (AppState.IsFullscreen) return;

        Rect frame = CombinedFrame();
        var current = ((int)frame.X, (int)frame.Y, (int)frame.Width, (int)frame.Height);

        // Don't rewrite settings.json unless the frame actually moved or resized.
        if (!_lastFrameSeeded)
        {
            _lastFrameSeeded = true;
            if (SettingsService.GetWindowFrame(out int sx, out int sy, out int sw, out int sh))
                _lastFrame = (sx, sy, sw, sh);
            else
                _lastFrame = (int.MinValue, int.MinValue, int.MinValue, int.MinValue);
        }
        if (current == _lastFrame) return;
        _lastFrame = current;
        SettingsService.SaveWindowFrame(current.Item1, current.Item2, current.Item3, current.Item4);
    }
}
