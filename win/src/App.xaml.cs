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

        var toolbar = new ToolbarWindow();
        var overlay = new OverlayWindow();
        _toolbar = toolbar;
        _overlay = overlay;
        AppState.Toolbar = toolbar;
        AppState.Overlay = overlay;

        // Restore the saved frame, or default to 600×400 centered.
        double x, y, w = 600, h = 400;
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
            x = area.Left + (area.Width - w) / 2;
            y = area.Top + (area.Height - h) / 2;
        }
        ApplyCombinedFrame(new Rect(x, y, w, h));

        toolbar.Show();
        overlay.Owner = toolbar;
        overlay.Show();
        toolbar.Activate();

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

        AppLogger.Log("Pob started");
        MouseService.Init();
        CoreBridge.Start();
    }

    protected override void OnExit(ExitEventArgs e)
    {
        _saveTimer?.Stop();
        SaveFrameNow();
        CoreBridge.Stop();
        MouseService.Shutdown();
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
