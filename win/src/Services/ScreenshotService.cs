// Captures the desktop area behind the Pob window's content view, mirroring
// the macOS/Linux ScreenshotService. macOS excludes the overlay window via
// CGWindowListCreateImage(.optionOnScreenBelowWindow); Windows has no direct
// equivalent, so the overlay window is made fully transparent for a moment
// while BitBlt grabs the screen.
//
// All published coordinates are screenshot pixels = physical device pixels
// (top-left origin), so ShotContext also records where the content area sat
// on the screen at capture time — mouse actions use it to convert the
// virtual cursor position back to screen coordinates.
using System.IO;
using System.Windows;
using System.Windows.Interop;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using System.Windows.Threading;
using Pob.Interop;
using Pob.Views;

namespace Pob.Services;

public struct ShotContext
{
    public bool Valid;
    public int OriginX; // content-area origin on the screen, device pixels
    public int OriginY;
    public int Width;  // content-area size in device pixels — the box the
    public int Height; // virtual cursor lives in, and the size a capture makes
    public double Scale; // DPI scale factor at capture time
}

public static class ScreenshotService
{
    private static readonly object CtxLock = new();
    private static ShotContext _context = new() { Valid = false, Scale = 1 };

    public static ShotContext GetContext()
    {
        lock (CtxLock) return _context;
    }

    private static void SetContext(int ox, int oy, int w, int h, double scale)
    {
        lock (CtxLock)
        {
            _context = new ShotContext
            {
                Valid = true, OriginX = ox, OriginY = oy, Width = w, Height = h, Scale = scale,
            };
        }
    }

    // ── pending request (core sends one capture at a time) ─────────────────

    private sealed record PendingShot(string Id, bool WithCursor, bool HasCrop,
                                      double CropX, double CropY, double CropW, double CropH,
                                      string Format, int MaxWidth, int Quality);

    private static PendingShot? _pending;

    public static void HandleCapture(string id, bool withCursor, bool hasCrop,
                                     double cropX, double cropY, double cropW, double cropH,
                                     string format, int maxWidth, int quality)
    {
        if (_pending != null)
        { // should not happen — the core awaits each capture
            CoreBridge.RespondError(id, "Capture already in progress");
            return;
        }
        OverlayWindow? overlay = AppState.Overlay;
        if (overlay == null || !overlay.IsLoaded)
        {
            CoreBridge.RespondError(id, "Window not ready");
            return;
        }

        _pending = new PendingShot(id, withCursor, hasCrop, cropX, cropY, cropW, cropH,
                                   format, maxWidth, quality);

        // Hide the overlay so the capture shows the desktop beneath it
        // (macOS: .optionOnScreenBelowWindow), give the compositor a moment,
        // then grab.
        overlay.Opacity = 0.0;
        var timer = new DispatcherTimer { Interval = TimeSpan.FromMilliseconds(80) };
        timer.Tick += (_, _) =>
        {
            timer.Stop();
            DoCapture();
        };
        timer.Start();
    }

    private static void FinishPending() => _pending = null;

    private static void Fail(string message)
    {
        if (_pending != null) CoreBridge.RespondError(_pending.Id, message);
        FinishPending();
    }

    private static void DoCapture()
    {
        PendingShot? pending = _pending;
        OverlayWindow? overlay = AppState.Overlay;
        if (pending == null) return;
        if (overlay == null)
        {
            Fail("Window not ready");
            return;
        }

        ContentView content = overlay.ContentView;

        // Content-area geometry in physical screen pixels.
        Point originDip;
        try
        {
            originDip = content.PointToScreen(new Point(0, 0)); // device pixels
        }
        catch (InvalidOperationException)
        {
            overlay.Opacity = 1.0;
            Fail("Screenshot capture failed");
            return;
        }
        double scale = VisualTreeHelper.GetDpi(content).DpiScaleX;

        int devX = (int)Math.Round(originDip.X);
        int devY = (int)Math.Round(originDip.Y);
        int devW = (int)Math.Round(content.ActualWidth * scale);
        int devH = (int)Math.Round(content.ActualHeight * scale);

        // Clamp to the virtual screen (all monitors).
        int vsX = NativeMethods.GetSystemMetrics(NativeMethods.SM_XVIRTUALSCREEN);
        int vsY = NativeMethods.GetSystemMetrics(NativeMethods.SM_YVIRTUALSCREEN);
        int vsW = NativeMethods.GetSystemMetrics(NativeMethods.SM_CXVIRTUALSCREEN);
        int vsH = NativeMethods.GetSystemMetrics(NativeMethods.SM_CYVIRTUALSCREEN);
        if (devX < vsX) { devW -= vsX - devX; devX = vsX; }
        if (devY < vsY) { devH -= vsY - devY; devY = vsY; }
        if (devX + devW > vsX + vsW) devW = vsX + vsW - devX;
        if (devY + devH > vsY + vsH) devH = vsY + vsH - devY;

        if (devW <= 0 || devH <= 0)
        {
            overlay.Opacity = 1.0;
            Fail("Screenshot capture failed");
            return;
        }

        BitmapSource? shot = CaptureScreen(devX, devY, devW, devH);
        overlay.Opacity = 1.0;

        if (shot == null)
        {
            Fail("Screenshot capture failed");
            return;
        }

        SetContext(devX, devY, devW, devH, scale);

        // Crop, then shrink, then the cursor, then the encoder. The order
        // matters more than any single step: shrinking first means the cursor
        // is drawn onto — and the encoder runs over — a quarter of the pixels
        // at half width, and both of those cost strictly by the pixel.
        BitmapSource result = shot;
        double cropX = 0, cropY = 0;

        if (pending.HasCrop && pending.CropW > 0 && pending.CropH > 0)
        {
            int cx = Math.Max(0, (int)pending.CropX);
            int cy = Math.Max(0, (int)pending.CropY);
            int cw = Math.Min((int)pending.CropW, devW - cx);
            int ch = Math.Min((int)pending.CropH, devH - cy);
            if (cw > 0 && ch > 0)
            {
                result = new CroppedBitmap(result, new Int32Rect(cx, cy, cw, ch));
                cropX = cx;
                cropY = cy;
            }
        }

        int sourceW = result.PixelWidth;
        int sourceH = result.PixelHeight;
        int outW = sourceW, outH = sourceH;
        // Only ever shrinks: asking for more pixels than were captured would
        // invent them, so a width larger than the window simply gets the window.
        if (pending.MaxWidth > 0 && pending.MaxWidth < sourceW)
        {
            outW = pending.MaxWidth;
            outH = Math.Max(1, (int)Math.Round((double)sourceH * pending.MaxWidth / sourceW));
        }
        double factor = (double)outW / sourceW;

        if (pending.WithCursor || outW != sourceW)
        {
            double px = 0, py = 0;
            if (pending.WithCursor)
            {
                MouseService.GetVirtualPos(out px, out py);
                // Into the shrunk picture's own coordinates: past the crop's
                // origin, then scaled by however much the picture shrank.
                px = (px - cropX) * factor;
                py = (py - cropY) * factor;
            }
            result = Compose(result, outW, outH, pending.WithCursor, px, py, scale * factor);
        }

        // Frozen, so the encode can leave this thread. It is the expensive
        // half and this is the UI thread, which is also where every mouse
        // event is posted from — at a watchable frame rate, encoding here
        // would put a picture in front of each one.
        result.Freeze();
        PendingShot shotRequest = pending;
        FinishPending();

        Task.Run(() => EncodeAndDeliver(shotRequest, result, outW, outH, sourceW, sourceH));
    }

    private static void EncodeAndDeliver(PendingShot pending, BitmapSource image,
                                         int width, int height, int sourceWidth, int sourceHeight)
    {
        byte[]? bytes = Encode(image, pending.Format, pending.Quality);
        if (bytes == null)
        {
            CoreBridge.RespondError(pending.Id, "Screenshot encoding failed");
            return;
        }
        var meta = new Dictionary<string, object?>
        {
            ["width"] = width,
            ["height"] = height,
            ["sourceWidth"] = sourceWidth,
            ["sourceHeight"] = sourceHeight,
        };
        // The frame channel if it is up, the JSON-RPC line as base64 if not —
        // which is what this shell did before the channel existed, and what an
        // older core still expects.
        if (FrameChannel.Send(pending.Id, meta, bytes)) return;
        CoreBridge.RespondImage(pending.Id, Convert.ToBase64String(bytes), meta);
    }

    // ── BitBlt capture ──────────────────────────────────────────────────────

    private static BitmapSource? CaptureScreen(int x, int y, int w, int h)
    {
        IntPtr screenDc = NativeMethods.GetDC(IntPtr.Zero);
        if (screenDc == IntPtr.Zero) return null;
        IntPtr memDc = IntPtr.Zero, bitmap = IntPtr.Zero, old = IntPtr.Zero;
        try
        {
            memDc = NativeMethods.CreateCompatibleDC(screenDc);
            bitmap = NativeMethods.CreateCompatibleBitmap(screenDc, w, h);
            if (memDc == IntPtr.Zero || bitmap == IntPtr.Zero) return null;
            old = NativeMethods.SelectObject(memDc, bitmap);

            // CAPTUREBLT includes other layered (translucent) windows.
            if (!NativeMethods.BitBlt(memDc, 0, 0, w, h, screenDc, x, y,
                                      NativeMethods.SRCCOPY | NativeMethods.CAPTUREBLT))
                return null;

            BitmapSource source = Imaging.CreateBitmapSourceFromHBitmap(
                bitmap, IntPtr.Zero, Int32Rect.Empty, BitmapSizeOptions.FromEmptyOptions());
            source.Freeze();
            return source;
        }
        catch (Exception)
        {
            return null;
        }
        finally
        {
            if (old != IntPtr.Zero) NativeMethods.SelectObject(memDc, old);
            if (bitmap != IntPtr.Zero) NativeMethods.DeleteObject(bitmap);
            if (memDc != IntPtr.Zero) NativeMethods.DeleteDC(memDc);
            NativeMethods.ReleaseDC(IntPtr.Zero, screenDc);
        }
    }

    // Shrinks the shot to (pxW, pxH) and, if asked, draws the arrow cursor
    // into it with its hotspot at (px, py) — already in the shrunk picture's
    // own coordinates. macOS renders the cursor 88 px tall on 2× displays;
    // 44 × scale keeps the same apparent size on every density, and scale has
    // the shrink folded into it so the cursor shrinks with the picture.
    //
    // Both jobs in one render: the shrink is free here, since the shot has to
    // be drawn into the target either way.
    private static BitmapSource Compose(BitmapSource shot, int pxW, int pxH,
                                        bool withCursor, double px, double py, double scale)
    {
        var visual = new DrawingVisual();
        using (DrawingContext dc = visual.RenderOpen())
        {
            // The bitmap may carry a non-96 DPI; draw it 1:1 into a 96-DPI target.
            dc.DrawImage(shot, new Rect(0, 0, pxW, pxH));
            if (withCursor)
            {
                double s = 44.0 * scale / CursorArrow.Height;
                dc.PushTransform(new TranslateTransform(px, py)); // hotspot = (0, 0), the arrow tip
                dc.PushTransform(new ScaleTransform(s, s));
                CursorArrow.Draw(dc);
                dc.Pop();
                dc.Pop();
            }
        }

        var target = new RenderTargetBitmap(pxW, pxH, 96, 96, PixelFormats.Pbgra32);
        target.Render(visual);
        target.Freeze();
        return target;
    }

    // PNG is the default because it is the agent's format: it reads the text
    // in a frame, and a lossy encoder is no way to hand it one. A browser
    // watching the machine wants the opposite trade, and asks for JPEG —
    // which is the heavier half of what makes a watchable frame rate possible
    // at all, being an order of magnitude cheaper to encode and to send.
    private static byte[]? Encode(BitmapSource source, string format, int quality)
    {
        try
        {
            BitmapEncoder encoder;
            if (format == "jpeg")
            {
                int q = quality > 0 ? Math.Min(100, Math.Max(1, quality)) : 70;
                encoder = new JpegBitmapEncoder { QualityLevel = q };
            }
            else
            {
                encoder = new PngBitmapEncoder();
            }
            encoder.Frames.Add(BitmapFrame.Create(source));
            using var stream = new MemoryStream();
            encoder.Save(stream);
            return stream.ToArray();
        }
        catch (Exception)
        {
            return null;
        }
    }
}
