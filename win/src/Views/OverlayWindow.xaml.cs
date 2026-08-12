// The content overlay window: translucent gray, always on top, glued below
// the toolbar window. Handles edge resizing (borderless windows have no
// system resize frame) and the per-window click-through / no-activate flags.
using System.Windows;
using System.Windows.Input;
using System.Windows.Interop;
using System.Windows.Media;
using System.Windows.Threading;
using Pob.Interop;

namespace Pob.Views;

public partial class OverlayWindow : Window
{
    public ContentView ContentView => ContentArea;

    private const double EdgeSize = 6;

    // How often the pointer is sampled while click-through is on — short
    // enough that the border is grabbable the moment the pointer lands on it.
    private static readonly TimeSpan EdgeWatchInterval = TimeSpan.FromMilliseconds(30);

    private enum Zone
    {
        None,
        Left,
        Right,
        Top,
        Bottom,
        TopLeft,
        TopRight,
        BottomLeft,
        BottomRight,
    }

    private Zone _resizeZone = Zone.None;
    private bool _resizing;
    private NativeMethods.POINT _resizeStartCursor;
    private Rect _resizeStartFrame;

    // An edge drag moves the window's top-left as it resizes it, which reaches
    // App's LocationChanged glue looking exactly like a move. CarryService asks
    // so it can tell the two apart.
    public bool IsResizing => _resizing;

    private bool _passThrough;
    private bool _edgeHot;
    private DispatcherTimer? _edgeWatch;

    // Matches the Root border's CornerRadius — see ClipContentToCorners.
    private const double CornerRadius = 3;

    public OverlayWindow()
    {
        InitializeComponent();
        PreviewMouseLeftButtonDown += OnPreviewLeftButtonDown;
        PreviewMouseMove += OnPreviewMove;
        PreviewMouseLeftButtonUp += OnPreviewLeftButtonUp;
        ContentArea.SizeChanged += (_, e) => ClipContentToCorners(e.NewSize);
    }

    // A Border rounds the background it paints, not what a child draws over it
    // — so the content area gets a clip of its own, or the screenshot flash and
    // the crop overlay would square the corners off again. The clip starts one
    // radius above the top edge: rounding is wanted along the bottom only, and
    // up there the toolbar window is glued on, where a notch would show.
    private void ClipContentToCorners(Size size)
    {
        var area = new Rect(0, -CornerRadius, size.Width, size.Height + CornerRadius);
        ContentArea.Clip = new RectangleGeometry(area, CornerRadius, CornerRadius);
    }

    // ── extended-style flags ────────────────────────────────────────────────

    private IntPtr Hwnd => new WindowInteropHelper(this).Handle;

    // WS_EX_TRANSPARENT is a whole-window flag, so switching click-through on
    // would also swallow the edge-resize grips below. Mirror the macOS shell:
    // while it is on, watch the pointer and lift the flag for as long as it
    // sits in the resize border — the edges stay grabbable and everything
    // inside them still passes through.
    public void SetHitTestTransparent(bool pass)
    {
        _passThrough = pass;
        if (!pass)
        {
            _edgeWatch?.Stop();
            _edgeHot = false;
        }
        ApplyHitTest();
        if (pass) StartEdgeWatch();
    }

    private void ApplyHitTest()
    {
        NativeMethods.SetExStyleFlag(Hwnd, NativeMethods.WS_EX_TRANSPARENT,
                                     _passThrough && !_edgeHot && !_resizing);
    }

    private void StartEdgeWatch()
    {
        _edgeWatch ??= new DispatcherTimer(EdgeWatchInterval, DispatcherPriority.Input,
                                           OnEdgeWatchTick, Dispatcher);
        _edgeWatch.Start();
    }

    private void OnEdgeWatchTick(object? sender, EventArgs e)
    {
        if (_resizing) return; // mouse is captured — leave the flag lifted
        Zone zone = PointerZone();
        Cursor? cursor = ZoneCursor(zone);
        if (Cursor != cursor) Cursor = cursor;
        bool hot = zone != Zone.None;
        if (hot == _edgeHot) return;
        _edgeHot = hot;
        ApplyHitTest();
    }

    // The resize zone under the real pointer. Read from the OS rather than
    // from a WPF mouse event: while click-through is on the window is hit-test
    // transparent, so no mouse event ever reaches it.
    private Zone PointerZone()
    {
        if (!ResizeAllowed || !IsVisible) return Zone.None;
        NativeMethods.GetCursorPos(out NativeMethods.POINT p);
        Point local = PointFromScreen(new Point(p.X, p.Y));
        if (local.X < 0 || local.Y < 0 || local.X > ActualWidth || local.Y > ActualHeight)
            return Zone.None;
        return HitTestZone(local);
    }

    public void SetNoActivate(bool noActivate)
    {
        NativeMethods.SetExStyleFlag(Hwnd, NativeMethods.WS_EX_NOACTIVATE, noActivate);
    }

    // ── edge resize ─────────────────────────────────────────────────────────

    private Zone HitTestZone(Point p)
    {
        double w = ActualWidth, h = ActualHeight;
        bool left = p.X <= EdgeSize;
        bool right = p.X >= w - EdgeSize;
        bool top = p.Y <= EdgeSize;
        bool bottom = p.Y >= h - EdgeSize;
        if (top && left) return Zone.TopLeft;
        if (top && right) return Zone.TopRight;
        if (bottom && left) return Zone.BottomLeft;
        if (bottom && right) return Zone.BottomRight;
        if (left) return Zone.Left;
        if (right) return Zone.Right;
        if (top) return Zone.Top;
        if (bottom) return Zone.Bottom;
        return Zone.None;
    }

    private static Cursor? ZoneCursor(Zone zone) => zone switch
    {
        Zone.Left or Zone.Right => Cursors.SizeWE,
        Zone.Top or Zone.Bottom => Cursors.SizeNS,
        Zone.TopLeft or Zone.BottomRight => Cursors.SizeNWSE,
        Zone.TopRight or Zone.BottomLeft => Cursors.SizeNESW,
        _ => null,
    };

    // Targeting and cropping need the content clickable up to the edges, and
    // a locked (or executing) window must not resize — mirror the GTK shell.
    private bool ResizeAllowed =>
        !AppState.IsResizeLocked && !AppState.IsTargeting && !AppState.IsCropping;

    private void OnPreviewLeftButtonDown(object sender, MouseButtonEventArgs e)
    {
        if (!ResizeAllowed) return;
        Zone zone = HitTestZone(e.GetPosition(this));
        if (zone == Zone.None) return;

        _resizeZone = zone;
        _resizing = true;
        NativeMethods.GetCursorPos(out _resizeStartCursor);
        _resizeStartFrame = new Rect(Left, Top, ActualWidth, ActualHeight);
        Root.CaptureMouse();
        e.Handled = true;
    }

    private void OnPreviewMove(object sender, MouseEventArgs e)
    {
        if (_resizing)
        {
            ApplyResize();
            e.Handled = true;
            return;
        }
        Cursor = ResizeAllowed ? ZoneCursor(HitTestZone(e.GetPosition(this))) : null;
    }

    private void OnPreviewLeftButtonUp(object sender, MouseButtonEventArgs e)
    {
        if (!_resizing) return;
        _resizing = false;
        _resizeZone = Zone.None;
        Root.ReleaseMouseCapture();
        e.Handled = true;
    }

    private void ApplyResize()
    {
        NativeMethods.GetCursorPos(out NativeMethods.POINT cur);
        double scale = VisualTreeHelper.GetDpi(this).DpiScaleX;
        double dx = (cur.X - _resizeStartCursor.X) / scale;
        double dy = (cur.Y - _resizeStartCursor.Y) / scale;

        double left = _resizeStartFrame.Left;
        double top = _resizeStartFrame.Top;
        double width = _resizeStartFrame.Width;
        double height = _resizeStartFrame.Height;

        bool resizeLeft = _resizeZone is Zone.Left or Zone.TopLeft or Zone.BottomLeft;
        bool resizeRight = _resizeZone is Zone.Right or Zone.TopRight or Zone.BottomRight;
        bool resizeTop = _resizeZone is Zone.Top or Zone.TopLeft or Zone.TopRight;
        bool resizeBottom = _resizeZone is Zone.Bottom or Zone.BottomLeft or Zone.BottomRight;

        if (resizeLeft)
        {
            double newWidth = Math.Max(MinWidth, width - dx);
            left += width - newWidth;
            width = newWidth;
        }
        else if (resizeRight)
        {
            width = Math.Max(MinWidth, width + dx);
        }

        if (resizeTop)
        {
            double newHeight = Math.Max(MinHeight, height - dy);
            top += height - newHeight;
            height = newHeight;
        }
        else if (resizeBottom)
        {
            height = Math.Max(MinHeight, height + dy);
        }

        Left = left;
        Top = top;
        Width = width;
        Height = height;
    }

    protected override void OnPreviewKeyDown(KeyEventArgs e)
    {
        base.OnPreviewKeyDown(e);
        // Quit Pob: Ctrl+Q (the stand-in for the macOS app menu item).
        if (e.Key == Key.Q && Keyboard.Modifiers == ModifierKeys.Control)
        {
            Application.Current.Shutdown();
            e.Handled = true;
        }
    }
}
