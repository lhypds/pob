// The overlay content view, mirroring the macOS/Linux ContentView: a
// translucent gray area with targeting mode (click to copy coordinates),
// crop mode (drag to copy a region), the animated virtual-cursor overlay
// shown while the agent executes, and the white screenshot flash.
using System.Diagnostics;
using System.Globalization;
using System.Windows;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Threading;

namespace Pob.Views;

public class ContentView : FrameworkElement
{
    // Targeting: last pointer position in DIP (logical) coordinates.
    private bool _hasMousePos;
    private Point _mousePos;

    // Cropping drag, DIP coordinates.
    private bool _cropDragging;
    private bool _hasCropRect;
    private Point _cropStart;
    private Point _cropCur;

    // Virtual cursor animation (screenshot/device pixels).
    private double _animX = 20, _animY = 20;
    private double _animFromX = 20, _animFromY = 20;
    private double _animToX = 20, _animToY = 20;
    private long _animStartMs;
    private bool _animating;

    // Screenshot flash.
    private double _flashOpacity;
    private long _flashStartMs;
    private bool _flashing;

    private bool _ticking;
    private static readonly Stopwatch Clock = Stopwatch.StartNew();

    private const double AnimDurationMs = 100;  // matches .easeOut(duration: 0.1)
    private const double FlashDurationMs = 400; // matches .easeOut(duration: 0.4)

    // Transient toast message.
    private string? _toastText;
    private DispatcherTimer? _toastTimer;

    private static readonly Brush Black75 = Freeze(new SolidColorBrush(Color.FromArgb(191, 0, 0, 0)));
    private static readonly Brush BlueFill = Freeze(new SolidColorBrush(
        Color.FromArgb((byte)(255 * 0.08), AppState.Accent.R, AppState.Accent.G, AppState.Accent.B)));
    private static readonly Brush BlueStroke = Freeze(new SolidColorBrush(AppState.Accent));
    private static readonly Pen BluePen = FreezePen(new Pen(BlueStroke, 1));
    private static readonly Typeface LabelFont =
        new(new FontFamily("Consolas"), FontStyles.Normal, FontWeights.Bold, FontStretches.Normal);

    private static Brush Freeze(Brush b)
    {
        b.Freeze();
        return b;
    }

    private static Pen FreezePen(Pen p)
    {
        p.Freeze();
        return p;
    }

    public ContentView()
    {
        MinWidth = 0;
        MinHeight = 0;
    }

    private double Scale => VisualTreeHelper.GetDpi(this).DpiScaleX;

    // ── animation plumbing ──────────────────────────────────────────────────

    private void EnsureTick()
    {
        if (_ticking) return;
        _ticking = true;
        CompositionTarget.Rendering += OnTick;
    }

    private void OnTick(object? sender, EventArgs e)
    {
        long now = Clock.ElapsedMilliseconds;
        bool active = false;

        if (_animating)
        {
            double t = (now - _animStartMs) / AnimDurationMs;
            if (t >= 1.0)
            {
                _animX = _animToX;
                _animY = _animToY;
                _animating = false;
            }
            else
            {
                double eased = 1.0 - Math.Pow(1.0 - t, 3); // ease-out cubic
                _animX = _animFromX + (_animToX - _animFromX) * eased;
                _animY = _animFromY + (_animToY - _animFromY) * eased;
                active = true;
            }
        }

        if (_flashing)
        {
            double t = (now - _flashStartMs) / FlashDurationMs;
            if (t >= 1.0)
            {
                _flashOpacity = 0;
                _flashing = false;
            }
            else
            {
                double eased = 1.0 - Math.Pow(1.0 - t, 3);
                _flashOpacity = 0.5 * (1.0 - eased);
                active = true;
            }
        }

        InvalidateVisual();
        if (!active)
        {
            CompositionTarget.Rendering -= OnTick;
            _ticking = false;
        }
    }

    // New virtual-cursor display target in screenshot (device) pixels; the
    // overlay animates toward it with a 0.1 s ease-out, like macOS.
    public void CursorTargetChanged(double x, double y)
    {
        _animFromX = _animX;
        _animFromY = _animY;
        _animToX = x;
        _animToY = y;
        _animStartMs = Clock.ElapsedMilliseconds;
        _animating = true;
        EnsureTick();
    }

    // Snaps the animated cursor back to (20, 20) — called when execution starts.
    public void ResetAnim()
    {
        _animX = _animFromX = _animToX = 20;
        _animY = _animFromY = _animToY = 20;
        _animating = false;
        InvalidateVisual();
    }

    // Triggers the white screenshot flash (opacity 0.5 fading out over 0.4 s).
    public void Flash()
    {
        _flashOpacity = 0.5;
        _flashStartMs = Clock.ElapsedMilliseconds;
        _flashing = true;
        EnsureTick();
    }

    // Shows a transient message (top center, black pill, white text) that
    // disappears after ~2 s — action feedback like "Logs cleared".
    public void ShowMessage(string text)
    {
        _toastText = text;
        _toastTimer?.Stop();
        _toastTimer = new DispatcherTimer { Interval = TimeSpan.FromSeconds(2) };
        _toastTimer.Tick += (_, _) =>
        {
            _toastTimer?.Stop();
            _toastText = null;
            InvalidateVisual();
        };
        _toastTimer.Start();
        InvalidateVisual();
    }

    // Applies the crosshair pointer while cropping; call after mode changes.
    public void UpdateCursorStyle()
    {
        Cursor = AppState.IsCropping ? Cursors.Cross : null;
        _hasMousePos = false;
        _hasCropRect = false;
        _cropDragging = false;
        InvalidateVisual();
    }

    // ── drawing ─────────────────────────────────────────────────────────────

    // Black 75% pill with white 11 px monospaced text, centered at (cx, cy) —
    // the same style as the macOS coordinate labels.
    private void DrawLabel(DrawingContext dc, double cx, double cy, string text)
    {
        var ft = new FormattedText(text, CultureInfo.InvariantCulture, FlowDirection.LeftToRight,
                                   LabelFont, 11, Brushes.White,
                                   VisualTreeHelper.GetDpi(this).PixelsPerDip);
        const double padH = 6, padV = 3;
        double w = ft.Width + padH * 2;
        double h = 11 + padV * 2;
        dc.DrawRoundedRectangle(Black75, null, new Rect(cx - w / 2, cy - h / 2, w, h), 4, 4);
        dc.DrawText(ft, new Point(cx - ft.Width / 2, cy - ft.Height / 2));
    }

    protected override void OnRender(DrawingContext dc)
    {
        double w = ActualWidth, h = ActualHeight;
        double scale = Scale;

        // Transparent hit-test surface (the translucent gray background is
        // painted by the OverlayWindow itself).
        dc.DrawRectangle(Brushes.Transparent, null, new Rect(0, 0, w, h));

        // Transient action feedback.
        if (_toastText != null)
            DrawLabel(dc, w / 2, 20, _toastText);

        // Crop selection rectangle + size label.
        if (AppState.IsCropping && _hasCropRect)
        {
            double minX = Math.Min(_cropStart.X, _cropCur.X);
            double minY = Math.Min(_cropStart.Y, _cropCur.Y);
            double cw = Math.Abs(_cropCur.X - _cropStart.X);
            double ch = Math.Abs(_cropCur.Y - _cropStart.Y);

            dc.DrawRectangle(BlueFill, null, new Rect(minX, minY, cw, ch));
            dc.DrawRectangle(null, BluePen, new Rect(minX + 0.5, minY + 0.5, cw, ch));

            string text = $"({(int)(minX * scale)}, {(int)(minY * scale)}) " +
                          $"{(int)(cw * scale)}×{(int)(ch * scale)}";

            // Same clamping as the macOS view: prefer below the box, then above.
            const double labelW = 180, labelH = 22, margin = 6;
            double cx = Math.Clamp(minX + cw / 2, labelW / 2 + margin, Math.Max(labelW / 2 + margin, w - labelW / 2 - margin));
            double belowY = minY + ch + 2 + labelH / 2;
            double aboveY = minY - 2 - labelH / 2;
            double minAllowed = margin + labelH / 2;
            double maxAllowed = h - margin - labelH / 2;
            double cy;
            if (belowY <= maxAllowed) cy = belowY;
            else if (aboveY >= minAllowed) cy = aboveY;
            else cy = Math.Clamp(belowY, minAllowed, maxAllowed);

            DrawLabel(dc, cx, cy, text);
        }

        // Targeting coordinate label following the pointer.
        if (AppState.IsTargeting && _hasMousePos)
        {
            string text = $"({(int)(_mousePos.X * scale)}, {(int)(_mousePos.Y * scale)})";
            const double estW = 100, margin = 6;
            double rawX = _mousePos.X + 55;
            double cx = Math.Max(estW / 2 + margin, Math.Min(rawX, w - estW / 2 - margin));
            double cy = Math.Max(14, _mousePos.Y - 14);
            DrawLabel(dc, cx, cy, text);
        }

        // Virtual cursor overlay while the agent executes.
        if (AppState.IsExecuting)
        {
            double vx = _animX / scale, vy = _animY / scale;
            dc.PushTransform(new TranslateTransform(vx, vy)); // hotspot = (0, 0)
            CursorArrow.Draw(dc);
            dc.Pop();
        }

        // Screenshot flash.
        if (_flashOpacity > 0)
        {
            var flash = new SolidColorBrush(Color.FromArgb((byte)(255 * _flashOpacity), 255, 255, 255));
            dc.DrawRectangle(flash, null, new Rect(0, 0, w, h));
        }
    }

    // ── input ───────────────────────────────────────────────────────────────

    private void CopyToClipboard(string text)
    {
        try
        {
            Clipboard.SetText(text);
        }
        catch (Exception)
        {
            // Clipboard can be transiently locked by another process.
        }
    }

    protected override void OnMouseLeftButtonDown(MouseButtonEventArgs e)
    {
        base.OnMouseLeftButtonDown(e);
        Point pos = e.GetPosition(this);

        if (AppState.IsTargeting)
        {
            double scale = Scale;
            string text = $"({(int)(pos.X * scale)}, {(int)(pos.Y * scale)})";
            CopyToClipboard(text);
            ShowMessage($"Copied {text}");
            _hasMousePos = false;
            AppState.SetTargeting(false);
            e.Handled = true;
            return;
        }

        if (AppState.IsCropping)
        {
            _cropDragging = true;
            _hasCropRect = false;
            _cropStart = _cropCur = pos;
            CaptureMouse();
            e.Handled = true;
            return;
        }

        // Plain click: bring the overlay window forward (macOS onTapGesture).
        AppState.Toolbar?.Activate();
    }

    protected override void OnMouseMove(MouseEventArgs e)
    {
        base.OnMouseMove(e);
        if (AppState.IsTargeting)
        {
            _hasMousePos = true;
            _mousePos = e.GetPosition(this);
            InvalidateVisual();
        }
        else if (AppState.IsCropping && _cropDragging)
        {
            _hasCropRect = true;
            _cropCur = e.GetPosition(this);
            InvalidateVisual();
        }
    }

    protected override void OnMouseLeftButtonUp(MouseButtonEventArgs e)
    {
        base.OnMouseLeftButtonUp(e);
        if (!AppState.IsCropping || !_cropDragging) return;
        _cropDragging = false;
        ReleaseMouseCapture();

        Point pos = e.GetPosition(this);
        double minX = Math.Min(_cropStart.X, pos.X);
        double minY = Math.Min(_cropStart.Y, pos.Y);
        double w = Math.Abs(pos.X - _cropStart.X);
        double h = Math.Abs(pos.Y - _cropStart.Y);
        _hasCropRect = false;

        if (w > 2 && h > 2)
        {
            double scale = Scale;
            string text = $"({(int)(minX * scale)}, {(int)(minY * scale)}, " +
                          $"{(int)(w * scale)}, {(int)(h * scale)})";
            CopyToClipboard(text);
            ShowMessage($"Copied {text}");
            AppState.SetCropping(false);
        }
        else
        {
            InvalidateVisual();
        }
        e.Handled = true;
    }

    protected override void OnMouseLeave(MouseEventArgs e)
    {
        base.OnMouseLeave(e);
        if (AppState.IsTargeting)
        {
            _hasMousePos = false;
            InvalidateVisual();
        }
    }
}
