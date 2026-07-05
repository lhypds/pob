// The content overlay window: translucent gray, always on top, glued below
// the toolbar window. Handles edge resizing (borderless windows have no
// system resize frame) and the per-window click-through / no-activate flags.
using System.Windows;
using System.Windows.Input;
using System.Windows.Interop;
using System.Windows.Media;
using Pob.Interop;

namespace Pob.Views;

public partial class OverlayWindow : Window
{
    public ContentView ContentView => ContentArea;

    private const double EdgeSize = 6;

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

    public OverlayWindow()
    {
        InitializeComponent();
        PreviewMouseLeftButtonDown += OnPreviewLeftButtonDown;
        PreviewMouseMove += OnPreviewMove;
        PreviewMouseLeftButtonUp += OnPreviewLeftButtonUp;
    }

    // ── extended-style flags ────────────────────────────────────────────────

    private IntPtr Hwnd => new WindowInteropHelper(this).Handle;

    public void SetHitTestTransparent(bool pass)
    {
        NativeMethods.SetExStyleFlag(Hwnd, NativeMethods.WS_EX_TRANSPARENT, pass);
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
        !AppState.IsMoveResizeLocked && !AppState.IsTargeting && !AppState.IsCropping;

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
