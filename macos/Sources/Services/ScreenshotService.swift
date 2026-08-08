import Cocoa
import Foundation
import ImageIO
import UniformTypeIdentifiers

/// Holds coordinate mapping info so screenshot pixels can be converted to CG event positions.
struct ScreenshotContext {
    // NSScreen coordinates (origin: bottom-left of screen, Y increases upward)
    let contentRectInScreen: CGRect
    let scale: CGFloat

    /// The content area in screenshot pixels — which is both the size of the
    /// image a capture produces and the box the virtual cursor lives in.
    var pixelSize: CGSize {
        CGSize(width: contentRectInScreen.width * scale, height: contentRectInScreen.height * scale)
    }

    /// Converts a screenshot pixel position (origin: top-left, Y increases downward)
    /// to a CGEvent mouse position (origin: top-left of primary display, Y increases downward).
    func toCGEventPoint(pixelX px: CGFloat, pixelY py: CGFloat) -> CGPoint {
        // NSScreen.screens[0] is always the primary display (origin 0,0 in NSScreen coords).
        // NSScreen.main is the screen with the focused window — it changes at runtime and
        // must NOT be used here, or the Y-flip breaks on multi-monitor setups.
        guard let primaryScreen = NSScreen.screens.first else { return .zero }
        let nsX = contentRectInScreen.origin.x + px / scale
        let nsY = contentRectInScreen.maxY - py / scale // NSScreen Y from bottom
        let cgY = primaryScreen.frame.height - nsY // Flip to CG (Y from top)
        return CGPoint(x: nsX, y: cgY)
    }
}

class ScreenshotService {
    static let shared = ScreenshotService()

    private init() {}

    /// The pixel→screen mapping for a window, derived from its geometry alone.
    /// Capturing an image is not required to know where a screenshot pixel
    /// lands, so mouse actions can resolve coordinates before the first capture.
    func context(for window: NSWindow) -> ScreenshotContext? {
        guard let screen = window.screen ?? NSScreen.main else { return nil }
        let screenRect = window.convertToScreen(window.contentLayoutRect)
        return ScreenshotContext(contentRectInScreen: screenRect, scale: screen.backingScaleFactor)
    }

    /// The captured frame as it goes on the wire, and the sizes needed to make
    /// sense of it.
    struct EncodedShot {
        let data: Data
        /// The picture's own size, after any shrinking.
        let width: Int
        let height: Int
        /// Its size in screenshot pixels — the space Pob's coordinates are in,
        /// and what a client has to scale a click on the picture back into.
        let sourceWidth: Int
        let sourceHeight: Int
    }

    /// Everything between the captured pixels and the bytes on the wire: crop,
    /// shrink, draw the cursor, encode.
    ///
    /// The order matters more than any single step. Shrinking first means the
    /// cursor is drawn onto — and the encoder runs over — a quarter of the
    /// pixels at half width, and both of those cost strictly by the pixel. It
    /// is also why nothing here goes near NSImage: `tiffRepresentation` is the
    /// whole frame written out uncompressed and parsed straight back in, twice
    /// the size of the screen in memory, to reach an encoder that took a
    /// CGImage all along.
    ///
    /// Shrinking only ever shrinks. Asking for more pixels than were captured
    /// would invent them, and a client asking for a width larger than the
    /// window simply gets the window.
    func encode(_ image: CGImage,
                cursorAt cursor: CGPoint?,
                crop: CGRect?,
                maxWidth: Int,
                format: String,
                quality: Int) -> EncodedShot?
    {
        var source = image
        if let crop, let cropped = source.cropping(to: cgCropRect(crop, in: source)) {
            source = cropped
        }
        let sourceW = source.width
        let sourceH = source.height
        guard sourceW > 0, sourceH > 0 else { return nil }

        var outW = sourceW
        var outH = sourceH
        if maxWidth > 0, maxWidth < sourceW {
            outW = maxWidth
            outH = max(1, Int((Double(sourceH) * Double(maxWidth) / Double(sourceW)).rounded()))
        }
        let factor = Double(outW) / Double(sourceW)

        // Nothing to redraw for: hand the captured image straight to the
        // encoder. This is the agent's path — full size, no cursor — and the
        // copy it skips is the size of the screen.
        var out = source
        if cursor != nil || outW != sourceW {
            guard let ctx = CGContext(
                data: nil,
                width: outW,
                height: outH,
                bitsPerComponent: 8,
                bytesPerRow: 0, // let CoreGraphics pick an aligned stride
                space: CGColorSpaceCreateDeviceRGB(),
                bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue
            ) else { return nil }
            // Cheaper than .high and, at these ratios, not tellable apart on a
            // frame that is about to be JPEG'd anyway.
            ctx.interpolationQuality = .medium
            ctx.draw(source, in: CGRect(x: 0, y: 0, width: outW, height: outH))

            if let cursor {
                // Into the shrunk picture's own coordinates: past the crop's
                // origin, then scaled by however much the picture shrank.
                let px = (cursor.x - (crop?.origin.x ?? 0)) * CGFloat(factor)
                let py = (cursor.y - (crop?.origin.y ?? 0)) * CGFloat(factor)
                drawCursor(into: ctx, at: CGPoint(x: px, y: py), pixelHeight: outH, factor: CGFloat(factor))
            }
            guard let drawn = ctx.makeImage() else { return nil }
            out = drawn
        }

        guard let data = Self.encodeImage(out, format: format, quality: quality) else { return nil }
        return EncodedShot(data: data, width: outW, height: outH,
                           sourceWidth: sourceW, sourceHeight: sourceH)
    }

    /// CGImage.cropping works in Y-from-bottom; screenshot pixels are
    /// Y-from-top.
    private func cgCropRect(_ rect: CGRect, in image: CGImage) -> CGRect {
        CGRect(x: rect.origin.x,
               y: CGFloat(image.height) - rect.origin.y - rect.height,
               width: rect.width,
               height: rect.height)
    }

    /// Draws the arrow cursor with its hotspot at (point) in a context that is
    /// `pixelHeight` tall. factor is how much the picture was shrunk, so the
    /// cursor shrinks with it and stays the same size relative to the screen.
    private func drawCursor(into ctx: CGContext, at point: CGPoint, pixelHeight: Int, factor: CGFloat) {
        let cursorImage = NSCursor.arrow.image
        let hotSpot = NSCursor.arrow.hotSpot
        guard cursorImage.size.height > 0,
              let tiff = cursorImage.tiffRepresentation,
              let rep = NSBitmapImageRep(data: tiff),
              let cursorCG = rep.cgImage else { return }

        // 44 px on a 2× capture, which is about what the machine draws its own
        // pointer at — half of what this was, which towered over everything
        // else in the frame. Scaling it with the picture keeps it that size
        // whatever width was asked for.
        let cursorH = 44 * factor
        let cursorW = cursorH * (cursorImage.size.width / cursorImage.size.height)
        let hotPxX = hotSpot.x * (cursorW / cursorImage.size.width)
        let hotPxY = hotSpot.y * (cursorH / cursorImage.size.height)

        // In CGContext (Y from bottom), place the cursor so its hotspot lands
        // on point, which is given Y-from-top.
        let rx = point.x - hotPxX
        let ry = CGFloat(pixelHeight) - point.y - cursorH + hotPxY
        ctx.draw(cursorCG, in: CGRect(x: rx, y: ry, width: cursorW, height: cursorH))
    }

    /// CGImage straight to encoded bytes. No NSImage, no TIFF in between.
    private static func encodeImage(_ image: CGImage, format: String, quality: Int) -> Data? {
        let jpeg = format == "jpeg"
        let type = jpeg ? UTType.jpeg.identifier : UTType.png.identifier
        let buffer = NSMutableData()
        guard let dest = CGImageDestinationCreateWithData(buffer, type as CFString, 1, nil) else { return nil }
        var props: [CFString: Any] = [:]
        if jpeg {
            let q = quality > 0 ? quality : 70
            props[kCGImageDestinationLossyCompressionQuality] = Double(min(100, max(1, q))) / 100.0
        }
        CGImageDestinationAddImage(dest, image, props as CFDictionary)
        guard CGImageDestinationFinalize(dest) else { return nil }
        return buffer as Data
    }

    /// Capture the window content area as a CGImage, with the coordinate
    /// context that says where those pixels sat on the screen.
    /// Uses CGWindowListCreateImage with .optionOnScreenBelowWindow so the pob overlay
    /// window itself (including its background) is excluded from the capture.
    func captureWindowContentCGImage(window: NSWindow) -> (CGImage, ScreenshotContext)? {
        guard let screen = window.screen ?? NSScreen.main else { return nil }
        guard let primaryScreen = NSScreen.screens.first else { return nil }

        let contentRect = window.contentLayoutRect
        let screenRect = window.convertToScreen(contentRect)
        let scale = screen.backingScaleFactor

        // Convert NSScreen rect (Y from bottom-left) to CG screen rect (Y from top-left of primary display).
        let cgRect = CGRect(
            x: screenRect.origin.x,
            y: primaryScreen.frame.height - screenRect.maxY,
            width: screenRect.width,
            height: screenRect.height
        )

        let windowID = CGWindowID(window.windowNumber)
        guard let cgImage = CGWindowListCreateImage(
            cgRect,
            .optionOnScreenBelowWindow,
            windowID,
            .bestResolution
        ) else {
            return nil
        }
        return (cgImage, ScreenshotContext(contentRectInScreen: screenRect, scale: scale))
    }

    /// Capture the window content area and also return coordinate context.
    /// Uses CGWindowListCreateImage with .optionOnScreenBelowWindow so the pob overlay
    /// window itself (including its background) is excluded from the capture.
    func captureWindowContentAreaWithContext(window: NSWindow) -> (NSImage, ScreenshotContext)? {
        guard let screen = window.screen ?? NSScreen.main else { return nil }
        guard let primaryScreen = NSScreen.screens.first else { return nil }

        let contentRect = window.contentLayoutRect
        let screenRect = window.convertToScreen(contentRect)
        let scale = screen.backingScaleFactor

        // Convert NSScreen rect (Y from bottom-left) to CG screen rect (Y from top-left of primary display).
        let cgRect = CGRect(
            x: screenRect.origin.x,
            y: primaryScreen.frame.height - screenRect.maxY,
            width: screenRect.width,
            height: screenRect.height
        )

        let windowID = CGWindowID(window.windowNumber)
        guard let cgImage = CGWindowListCreateImage(
            cgRect,
            .optionOnScreenBelowWindow,
            windowID,
            .bestResolution
        ) else {
            return nil
        }

        let image = NSImage(cgImage: cgImage, size: screenRect.size)
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
        let hotSpot = NSCursor.arrow.hotSpot // in cursor image point coords

        // Scale cursor to a fixed target height in screenshot pixels.
        let targetH: CGFloat = 88
        let aspect = cursorNSImage.size.height > 0 ? cursorNSImage.size.width / cursorNSImage.size.height : 1
        let cursorW = targetH * aspect
        let cursorH = targetH

        // Scale hotspot from cursor image points to our target pixel size.
        let hotPxX = cursorNSImage.size.width > 0 ? hotSpot.x * (cursorW / cursorNSImage.size.width) : 0
        let hotPxY = cursorNSImage.size.height > 0 ? hotSpot.y * (cursorH / cursorNSImage.size.height) : 0

        // In CGContext (Y from bottom), place cursor so its hotspot lands on pixelPos.
        // For image pixel (hx, hy) drawn in rect (rx, ry, cW, cH):
        //   CG position of that pixel = (rx + hx, ry + cH - hy)
        // We want that = (pixelPos.x, pixelH - pixelPos.y), so:
        let rx = pixelPos.x - hotPxX
        let ry = CGFloat(pixelH) - pixelPos.y - cursorH + hotPxY

        if let cursorTiff = cursorNSImage.tiffRepresentation,
           let cursorRep = NSBitmapImageRep(data: cursorTiff),
           let cursorCG = cursorRep.cgImage
        {
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
        let topEdge = max(0, pixelPos.y - radius)
        let bottomEdge = min(imgH, pixelPos.y + radius)
        let leftEdge = max(0, pixelPos.x - radius)
        let rightEdge = min(imgW, pixelPos.x + radius)

        let cropW = rightEdge - leftEdge
        let cropH = bottomEdge - topEdge

        guard cropW > 0, cropH > 0 else { return nil }

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
        let hotX = (pixelPos.x - leftEdge) * zoomFactor
        let hotY = (bottomEdge - pixelPos.y) * zoomFactor // CG: 0 = bottom of crop

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

    /// Crop an image to a pixel rect (top-left origin, same convention as screenshot pixels).
    func crop(_ image: NSImage, to rect: CGRect) -> NSImage? {
        guard let tiff = image.tiffRepresentation,
              let rep = NSBitmapImageRep(data: tiff),
              let sourceCG = rep.cgImage else { return nil }

        let imgH = CGFloat(rep.pixelsHigh)
        // CGImage.cropping uses Y-from-bottom (CG convention).
        let cgRect = CGRect(x: rect.origin.x, y: imgH - rect.origin.y - rect.height,
                            width: rect.width, height: rect.height)
        guard let cropped = sourceCG.cropping(to: cgRect) else { return nil }
        return NSImage(cgImage: cropped, size: rect.size)
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
        guard let n = deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? NSNumber else {
            return CGDirectDisplayID(0)
        }
        return CGDirectDisplayID(n.uint32Value)
    }
}
