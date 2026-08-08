// The classic arrow pointer drawn as vector geometry — the same hand-drawn
// shape the Linux shell uses (hotspot at the tip, top-left). Shared by the
// screenshot compositor and the executing-mode overlay cursor.
using System.Windows;
using System.Windows.Media;

namespace Pob.Views;

public static class CursorArrow
{
    public const double Width = 24;
    public const double Height = 36;

    /// How much of that the overlay draws. The geometry is sized for the
    /// screenshot compositor, which scales it to a fixed pixel height so the
    /// agent can see it; drawn straight into the window it comes out at device
    /// -independent units and towers over the real Windows pointer beside it.
    /// Half is about the size of that pointer. macOS and Linux sidestep this by
    /// drawing the system cursor image at its natural size, which Windows has
    /// no equivalently cheap way to reach from WPF.
    public const double OverlayScale = 0.5;

    public static readonly Geometry Geometry = BuildGeometry();
    private static readonly Pen Outline = BuildOutline(1.5);

    // Half scale would take the outline down to 0.75 px with it, which is the
    // one part of the arrow that cannot afford to go faint — it is what keeps
    // the black shape off a dark background. Thickened here so it lands back at
    // a crisp 1 px once the overlay's scale is applied.
    private static readonly Pen OverlayOutline = BuildOutline(1.0 / OverlayScale);

    private static Geometry BuildGeometry()
    {
        var geometry = new StreamGeometry();
        using (StreamGeometryContext ctx = geometry.Open())
        {
            ctx.BeginFigure(new Point(0, 0), isFilled: true, isClosed: true);
            ctx.LineTo(new Point(0, 26), true, false);
            ctx.LineTo(new Point(6, 20), true, false);
            ctx.LineTo(new Point(10, 30), true, false);
            ctx.LineTo(new Point(14, 28), true, false);
            ctx.LineTo(new Point(10, 19), true, false);
            ctx.LineTo(new Point(18, 18), true, false);
        }
        geometry.Freeze();
        return geometry;
    }

    private static Pen BuildOutline(double thickness)
    {
        var pen = new Pen(Brushes.White, thickness);
        pen.Freeze();
        return pen;
    }

    /// Draws at whatever scale the caller has pushed — the screenshot
    /// compositor, which scales up and wants the outline to grow with it.
    public static void Draw(DrawingContext dc)
    {
        dc.DrawGeometry(Brushes.Black, Outline, Geometry);
    }

    /// Draws for the on-screen overlay, which pushes OverlayScale first.
    public static void DrawOverlay(DrawingContext dc)
    {
        dc.DrawGeometry(Brushes.Black, OverlayOutline, Geometry);
    }
}
