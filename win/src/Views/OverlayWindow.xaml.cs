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
    // The pointer is over something this window must keep for itself: the
    // resize border, or the dot a hidden menu left in the corner.
    private bool _pointerHot;
    private DispatcherTimer? _edgeWatch;

    // The dot a hidden menu leaves behind is pressed here rather than in the
    // ContentView, because what a press on it means is only settled when the
    // button comes up: a press that goes nowhere brings the menu back, and one
    // that travels moves the window — the titlebar that used to be dragged is
    // part of what went away.
    private bool _dotPressed;
    private bool _dotDragged;
    private NativeMethods.POINT _dotStartCursor;
    private Point _dotStartFrame;

    // How far the pointer travels before a press counts as a drag rather than a
    // click. A dot this small is pressed with a hand that is never perfectly
    // still, and a menu that refused to come back because the pointer moved a
    // pixel would be a menu with no way back at all.
    private const double DotDragThreshold = 3;

    // Matches the Root border's CornerRadius — see ClipContentToCorners. Not a
    // constant, because fullscreen squares the shape off: the rounding is there
    // to finish the window the toolbar starts, and a display has no corners of
    // its own to round.
    private double _cornerRadius = 3;

    public OverlayWindow()
    {
        InitializeComponent();
        PreviewMouseLeftButtonDown += OnPreviewLeftButtonDown;
        PreviewMouseMove += OnPreviewMove;
        PreviewMouseLeftButtonUp += OnPreviewLeftButtonUp;
        ContentArea.SizeChanged += (_, e) => ClipContentToCorners(e.NewSize);
    }

    // The overlay as the whole display: no toolbar above it to finish the shape
    // of, and nothing to grab it by. App.EnterFullscreen has already placed it.
    public void EnterFullscreen()
    {
        _cornerRadius = 0;
        Root.CornerRadius = new System.Windows.CornerRadius(0);
        ClipContentToCorners(new Size(ContentArea.ActualWidth, ContentArea.ActualHeight));
    }

    // A Border rounds the background it paints, not what a child draws over it
    // — so the content area gets a clip of its own, or the screenshot flash and
    // the crop overlay would square the corners off again. The clip starts one
    // radius above the top edge: rounding is wanted along the bottom only, and
    // up there the toolbar window is glued on, where a notch would show.
    private void ClipContentToCorners(Size size)
    {
        var area = new Rect(0, -_cornerRadius, size.Width, size.Height + _cornerRadius);
        ContentArea.Clip = new RectangleGeometry(area, _cornerRadius, _cornerRadius);
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
            _pointerHot = false;
        }
        ApplyHitTest();
        // Nothing for the watch to find in fullscreen: there is no resize
        // border to lift the flag for, so it would only be arriving at the same
        // answer thirty times a second.
        if (pass && !AppState.IsFullscreen) StartEdgeWatch();
    }

    private void ApplyHitTest()
    {
        NativeMethods.SetExStyleFlag(Hwnd, NativeMethods.WS_EX_TRANSPARENT,
                                     _passThrough && !_pointerHot && !_resizing && !_dotPressed);
    }

    private void StartEdgeWatch()
    {
        _edgeWatch ??= new DispatcherTimer(EdgeWatchInterval, DispatcherPriority.Input,
                                           OnEdgeWatchTick, Dispatcher);
        _edgeWatch.Start();
    }

    private void OnEdgeWatchTick(object? sender, EventArgs e)
    {
        // Mouse is captured — leave the flag lifted for the length of the drag.
        if (_resizing || _dotPressed) return;
        Zone zone = PointerZone();
        bool onDot = PointerOnMenuDot();
        Cursor? cursor = onDot ? Cursors.Hand : ZoneCursor(zone);
        if (Cursor != cursor) Cursor = cursor;
        bool hot = zone != Zone.None || onDot;
        if (hot == _pointerHot) return;
        _pointerHot = hot;
        ApplyHitTest();
    }

    // Whether the pointer is over the dot a hidden menu left in the corner —
    // the only button Pob has while the toolbar window is off the screen, and
    // so the one place inside the content area that must keep its own clicks.
    //
    // Never while the agent is driving, though: the dot sits inside the content
    // area, where a macro's clicks are aimed, and a live dot would swallow one
    // meant for the application below. It comes back the moment the run ends.
    private bool PointerOnMenuDot()
    {
        if (!AppState.IsMenuHidden || AppState.IsExecuting || !IsVisible) return false;
        NativeMethods.GetCursorPos(out NativeMethods.POINT p);
        Point local = PointFromScreen(new Point(p.X, p.Y));
        return AppState.MenuDotHitRect(ActualWidth).Contains(local);
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
    // A fullscreen window is held to the display, so it never resizes at all.
    private bool ResizeAllowed =>
        !AppState.IsFullscreen && !AppState.IsResizeLocked &&
        !AppState.IsTargeting && !AppState.IsCropping;

    private void OnPreviewLeftButtonDown(object sender, MouseButtonEventArgs e)
    {
        // The dot goes first: with the menu hidden it is the only thing Pob has
        // left on the screen, so nothing else on the content area may take its
        // press — a mode left running from before the menu went away included.
        if (AppState.IsMenuHidden && AppState.MenuDotHitRect(ActualWidth).Contains(e.GetPosition(this)))
        {
            _dotPressed = true;
            _dotDragged = false;
            // Screen coordinates on both sides, so the arithmetic is not
            // measured against a window that is travelling with the pointer.
            NativeMethods.GetCursorPos(out _dotStartCursor);
            _dotStartFrame = new Point(Left, Top);
            Root.CaptureMouse();
            ApplyHitTest();
            e.Handled = true;
            return;
        }

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
        if (_dotPressed)
        {
            DragByDot();
            e.Handled = true;
            return;
        }
        Point pos = e.GetPosition(this);
        if (AppState.IsMenuHidden && AppState.MenuDotHitRect(ActualWidth).Contains(pos))
            Cursor = Cursors.Hand;
        else
            Cursor = ResizeAllowed ? ZoneCursor(HitTestZone(pos)) : null;
    }

    private void OnPreviewLeftButtonUp(object sender, MouseButtonEventArgs e)
    {
        // What the press on the dot turned out to mean: a window moved, or a
        // menu asked for.
        if (_dotPressed)
        {
            bool wasDrag = _dotDragged;
            _dotPressed = false;
            _dotDragged = false;
            Root.ReleaseMouseCapture();
            ApplyHitTest();
            e.Handled = true;
            if (!wasDrag) AppState.SetMenuHidden(false);
            return;
        }

        if (!_resizing) return;
        _resizing = false;
        _resizeZone = Zone.None;
        Root.ReleaseMouseCapture();
        e.Handled = true;
    }

    // Moves the window by the pointer's own delta from where the dot was
    // pressed, rather than by each step's, so a drag does not walk the frame
    // away from the pointer a fraction at a time.
    //
    // Only this window is moved: App's LocationChanged glue brings the (hidden)
    // toolbar window along and tells Carry about the move, so a locked frame
    // takes the windows below with it exactly as a titlebar drag would.
    private void DragByDot()
    {
        NativeMethods.GetCursorPos(out NativeMethods.POINT cur);
        double scale = VisualTreeHelper.GetDpi(this).DpiScaleX;
        double dx = (cur.X - _dotStartCursor.X) / scale;
        double dy = (cur.Y - _dotStartCursor.Y) / scale;
        if (!_dotDragged &&
            Math.Abs(dx) < DotDragThreshold && Math.Abs(dy) < DotDragThreshold) return;
        _dotDragged = true;
        Left = _dotStartFrame.X + dx;
        Top = _dotStartFrame.Y + dy;
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
