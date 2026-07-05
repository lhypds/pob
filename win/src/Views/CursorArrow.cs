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

    public static readonly Geometry Geometry = BuildGeometry();
    private static readonly Pen Outline = BuildOutline();

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

    private static Pen BuildOutline()
    {
        var pen = new Pen(Brushes.White, 1.5);
        pen.Freeze();
        return pen;
    }

    public static void Draw(DrawingContext dc)
    {
        dc.DrawGeometry(Brushes.Black, Outline, Geometry);
    }
}
