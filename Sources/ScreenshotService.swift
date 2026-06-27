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
        // NSScreen.screens[0] is always the primary display (origin 0,0 in NSScreen coords).
        // NSScreen.main is the screen with the focused window — it changes at runtime and
        // must NOT be used here, or the Y-flip breaks on multi-monitor setups.
        guard let primaryScreen = NSScreen.screens.first else { return .zero }
        let nsX = contentRectInScreen.origin.x + px / scale
        let nsY = contentRectInScreen.maxY - py / scale  // NSScreen Y from bottom
        let cgY = primaryScreen.frame.height - nsY        // Flip to CG (Y from top)
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

    /// Returns a copy of the image with the macOS arrow cursor drawn at the given
    /// screenshot pixel position (origin: top-left). The cursor hotspot (tip) is placed at pixelPos.
    func imageWithCursor(_ image: NSImage, at pixelPos: CGPoint) -> NSImage {
        guard let tiffData = image.tiffRepresentation,
              let sourceRep = NSBitmapImageRep(data: tiffData),
              let sourceCGImage = sourceRep.cgImage else { return image }

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

        ctx.draw(sourceCGImage, in: CGRect(x: 0, y: 0, width: CGFloat(pixelW), height: CGFloat(pixelH)))

        // Draw the macOS system arrow cursor.
        // NSCursor.arrow hotspot is (0,0) = the tip at the top-left of the cursor image.
        let cursorNSImage = NSCursor.arrow.image
        let hotSpot = NSCursor.arrow.hotSpot  // in cursor image point coords

        // Scale cursor to a fixed target height in screenshot pixels.
        let targetH: CGFloat = 88
        let aspect = cursorNSImage.size.height > 0 ? cursorNSImage.size.width / cursorNSImage.size.height : 1
        let cursorW = targetH * aspect
        let cursorH = targetH

        // Scale hotspot from cursor image points to our target pixel size.
        let hotPxX = cursorNSImage.size.width  > 0 ? hotSpot.x * (cursorW / cursorNSImage.size.width)  : 0
        let hotPxY = cursorNSImage.size.height > 0 ? hotSpot.y * (cursorH / cursorNSImage.size.height) : 0

        // In CGContext (Y from bottom), place cursor so its hotspot lands on pixelPos.
        // For image pixel (hx, hy) drawn in rect (rx, ry, cW, cH):
        //   CG position of that pixel = (rx + hx, ry + cH - hy)
        // We want that = (pixelPos.x, pixelH - pixelPos.y), so:
        let rx = pixelPos.x - hotPxX
        let ry = CGFloat(pixelH) - pixelPos.y - cursorH + hotPxY

        if let cursorTiff = cursorNSImage.tiffRepresentation,
           let cursorRep = NSBitmapImageRep(data: cursorTiff),
           let cursorCG = cursorRep.cgImage {
            ctx.draw(cursorCG, in: CGRect(x: rx, y: ry, width: cursorW, height: cursorH))
        }

        if let resultImg = ctx.makeImage() {
            return NSImage(cgImage: resultImg, size: image.size)
        }
        return image
    }

    /// Returns a 4× magnified crop of the image centered on pixelPos with a red crosshair at the hotspot.
    /// Pixel coordinates use top-left origin (same as screenshot pixel convention).
    func zoomedView(_ image: NSImage, around pixelPos: CGPoint, radius: CGFloat = 150, zoomFactor: CGFloat = 4) -> NSImage? {
        guard let tiff = image.tiffRepresentation,
              let rep = NSBitmapImageRep(data: tiff),
              let sourceCG = rep.cgImage else { return nil }

        let imgW = CGFloat(rep.pixelsWide)
        let imgH = CGFloat(rep.pixelsHigh)

        // Crop bounds in top-left origin space, clamped to image edges.
        let topEdge    = max(0, pixelPos.y - radius)
        let bottomEdge = min(imgH, pixelPos.y + radius)
        let leftEdge   = max(0, pixelPos.x - radius)
        let rightEdge  = min(imgW, pixelPos.x + radius)

        let cropW = rightEdge - leftEdge
        let cropH = bottomEdge - topEdge

        guard cropW > 0 && cropH > 0 else { return nil }

        // CGImage.cropping uses Y-from-bottom (CG convention).
        let cropRect = CGRect(x: leftEdge, y: imgH - bottomEdge, width: cropW, height: cropH)
        guard let croppedCG = sourceCG.cropping(to: cropRect) else { return nil }

        let outW = Int(cropW * zoomFactor)
        let outH = Int(cropH * zoomFactor)

        guard let outCtx = CGContext(
            data: nil, width: outW, height: outH,
            bitsPerComponent: 8, bytesPerRow: outW * 4,
            space: CGColorSpaceCreateDeviceRGB(),
            bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
        ) else { return nil }

        // Draw cropped image (no flip needed — same convention as imageWithCursor).
        outCtx.draw(croppedCG, in: CGRect(x: 0, y: 0, width: CGFloat(outW), height: CGFloat(outH)))

        // Hotspot position in the output context (Y from bottom = CG convention).
        let hotX  = (pixelPos.x - leftEdge)  * zoomFactor
        let hotY  = (bottomEdge - pixelPos.y) * zoomFactor   // CG: 0 = bottom of crop

        // Red crosshair lines.
        outCtx.setStrokeColor(red: 1, green: 0, blue: 0, alpha: 0.9)
        outCtx.setLineWidth(3)
        let arm: CGFloat = 30
        outCtx.move(to: CGPoint(x: hotX - arm, y: hotY)); outCtx.addLine(to: CGPoint(x: hotX + arm, y: hotY))
        outCtx.move(to: CGPoint(x: hotX, y: hotY - arm)); outCtx.addLine(to: CGPoint(x: hotX, y: hotY + arm))
        outCtx.strokePath()

        // Bright dot at exact click point.
        let zDot: CGFloat = 8
        outCtx.setFillColor(red: 1, green: 1, blue: 0, alpha: 0.9)
        outCtx.setStrokeColor(red: 1, green: 0, blue: 0, alpha: 1)
        outCtx.setLineWidth(2)
        outCtx.addEllipse(in: CGRect(x: hotX - zDot, y: hotY - zDot, width: zDot * 2, height: zDot * 2))
        outCtx.drawPath(using: .fillStroke)

        if let resultCG = outCtx.makeImage() {
            return NSImage(cgImage: resultCG, size: NSSize(width: outW, height: outH))
        }
        return nil
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
