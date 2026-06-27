import Cocoa
import Foundation

// Holds coordinate mapping info so screenshot pixels can be converted to CG event positions.
struct ScreenshotContext {
    // NSScreen coordinates (origin: bottom-left of screen, Y increases upward)
    let contentRectInScreen: CGRect
    let scale: CGFloat

    // Converts a screenshot pixel position (origin: top-left, Y increases downward)
    // to a CGEvent mouse position (origin: top-left of primary display, Y increases downward).
    func toCGEventPoint(pixelX px: CGFloat, pixelY py: CGFloat) -> CGPoint {
        guard let mainScreen = NSScreen.main else { return .zero }
        let nsX = contentRectInScreen.origin.x + px / scale
        let nsY = contentRectInScreen.maxY - py / scale  // NSScreen Y from bottom
        let cgY = mainScreen.frame.height - nsY           // Flip to CG (Y from top)
        return CGPoint(x: nsX, y: cgY)
    }
}

class ScreenshotService {
    static let shared = ScreenshotService()

    private init() {}

    /// Capture the window content area and also return coordinate context.
    func captureWindowContentAreaWithContext(window: NSWindow) -> (NSImage, ScreenshotContext)? {
        guard let screen = window.screen ?? NSScreen.main else { return nil }

        let contentRect = window.contentLayoutRect
        let screenRect = window.convertToScreen(contentRect)

        let scale = screen.backingScaleFactor
        let sf = screen.frame

        let pixelRect = CGRect(
            x: (screenRect.origin.x - sf.origin.x) * scale,
            y: (sf.maxY - screenRect.maxY) * scale,
            width: screenRect.width * scale,
            height: screenRect.height * scale
        )

        guard let displayImage = CGDisplayCreateImage(screen.displayID),
              let cropped = displayImage.cropping(to: pixelRect) else {
            return nil
        }

        let image = NSImage(cgImage: cropped, size: screenRect.size)
        let context = ScreenshotContext(contentRectInScreen: screenRect, scale: scale)
        return (image, context)
    }

    /// Capture screenshot of the transparent content area of the given window.
    func captureWindowContentArea(window: NSWindow) -> NSImage? {
        captureWindowContentAreaWithContext(window: window)?.0
    }

    /// Capture screenshot of the main display
    func captureScreenshot() -> NSImage? {
        guard let screen = NSScreen.main else { return nil }
        guard let cgImage = CGDisplayCreateImage(screen.displayID) else { return nil }
        return NSImage(cgImage: cgImage, size: screen.frame.size)
    }

    /// Returns a copy of the image with a red cursor marker drawn at the given
    /// screenshot pixel position (origin: top-left).
    func imageWithCursor(_ image: NSImage, at pixelPos: CGPoint) -> NSImage {
        guard let tiffData = image.tiffRepresentation,
              let sourceRep = NSBitmapImageRep(data: tiffData) else { return image }

        let pixelW = sourceRep.pixelsWide
        let pixelH = sourceRep.pixelsHigh

        guard let ctx = CGContext(
            data: nil,
            width: pixelW,
            height: pixelH,
            bitsPerComponent: 8,
            bytesPerRow: pixelW * 4,
            space: CGColorSpaceCreateDeviceRGB(),
            bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
        ) else { return image }

        // Draw source image. CGContext Y is from bottom, so flip while drawing.
        ctx.saveGState()
        ctx.translateBy(x: 0, y: CGFloat(pixelH))
        ctx.scaleBy(x: 1, y: -1)
        if let cgImg = sourceRep.cgImage {
            ctx.draw(cgImg, in: CGRect(x: 0, y: 0, width: CGFloat(pixelW), height: CGFloat(pixelH)))
        }
        ctx.restoreGState()

        // Cursor position: pixelPos.y is from top; CG context Y is from bottom.
        let cx = pixelPos.x
        let cy = CGFloat(pixelH) - pixelPos.y

        let radius: CGFloat = 12

        // Filled circle
        ctx.setFillColor(red: 1, green: 0.1, blue: 0.1, alpha: 0.35)
        ctx.setStrokeColor(red: 1, green: 0.1, blue: 0.1, alpha: 1)
        ctx.setLineWidth(3)
        ctx.addEllipse(in: CGRect(x: cx - radius, y: cy - radius, width: radius * 2, height: radius * 2))
        ctx.drawPath(using: .fillStroke)

        // Crosshair
        ctx.setLineWidth(2)
        ctx.move(to: CGPoint(x: cx - radius * 1.6, y: cy))
        ctx.addLine(to: CGPoint(x: cx + radius * 1.6, y: cy))
        ctx.strokePath()
        ctx.move(to: CGPoint(x: cx, y: cy - radius * 1.6))
        ctx.addLine(to: CGPoint(x: cx, y: cy + radius * 1.6))
        ctx.strokePath()

        if let resultImg = ctx.makeImage() {
            return NSImage(cgImage: resultImg, size: image.size)
        }
        return image
    }

    /// Capture screenshot and save to file
    func captureAndSave(to path: String) -> Bool {
        guard let image = captureScreenshot() else { return false }

        guard let tiffData = image.tiffRepresentation,
              let bitmapImage = NSBitmapImageRep(data: tiffData),
              let pngData = bitmapImage.representation(using: .png, properties: [:]) else { return false }

        do {
            try pngData.write(to: URL(fileURLWithPath: path))
            return true
        } catch {
            return false
        }
    }
}

extension NSScreen {
    var displayID: CGDirectDisplayID {
        guard let n = self.deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? NSNumber else {
            return CGDirectDisplayID(0)
        }
        return CGDirectDisplayID(n.uint32Value)
    }
}
