import Cocoa
import Foundation

class ScreenshotService {
    static let shared = ScreenshotService()

    private init() {}

    /// Capture screenshot of the transparent content area of the given window.
    /// Uses CGDisplayCreateImage + crop to reliably restrict capture to the layout area.
    func captureWindowContentArea(window: NSWindow) -> NSImage? {
        guard let screen = window.screen ?? NSScreen.main else { return nil }

        // contentLayoutRect: area in window coords not obscured by toolbar/titlebar
        let contentRect = window.contentLayoutRect
        let screenRect = window.convertToScreen(contentRect)  // NSScreen coords (origin bottom-left)

        print("[ScreenshotService] contentLayoutRect=\(contentRect) screenRect=\(screenRect)")

        let scale = screen.backingScaleFactor
        let sf = screen.frame  // screen frame in NSScreen coords

        // Flip from NSScreen (Y-up, origin bottom-left) to pixel coords (Y-down, origin top-left)
        let pixelRect = CGRect(
            x: (screenRect.origin.x - sf.origin.x) * scale,
            y: (sf.maxY - screenRect.maxY) * scale,
            width: screenRect.width * scale,
            height: screenRect.height * scale
        )

        print("[ScreenshotService] pixelRect=\(pixelRect) scale=\(scale)")

        guard let displayImage = CGDisplayCreateImage(screen.displayID),
              let cropped = displayImage.cropping(to: pixelRect) else {
            print("[ScreenshotService] Failed to crop display image")
            return nil
        }

        print("[ScreenshotService] captured \(cropped.width)x\(cropped.height)px")
        return NSImage(cgImage: cropped, size: screenRect.size)
    }

    /// Capture screenshot of the main display
    func captureScreenshot() -> NSImage? {
        guard let screen = NSScreen.main else {
            print("Unable to get main screen")
            return nil
        }

        let screenFrame = screen.frame
        guard let cgImage = CGDisplayCreateImage(screen.displayID) else {
            print("Unable to create CGImage from display")
            return nil
        }

        let image = NSImage(cgImage: cgImage, size: screenFrame.size)
        return image
    }
    
    /// Capture screenshot and save to file
    func captureAndSave(to path: String) -> Bool {
        guard let image = captureScreenshot() else {
            return false
        }
        
        guard let tiffData = image.tiffRepresentation,
              let bitmapImage = NSBitmapImageRep(data: tiffData),
              let pngData = bitmapImage.representation(using: .png, properties: [:]) else {
            print("Unable to convert image to PNG")
            return false
        }
        
        do {
            try pngData.write(to: URL(fileURLWithPath: path))
            return true
        } catch {
            print("Error saving screenshot: \(error)")
            return false
        }
    }
}

extension NSScreen {
    /// Get CGDirectDisplayID from NSScreen
    var displayID: CGDirectDisplayID {
        guard let screenNumber = self.deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? NSNumber else {
            return CGDirectDisplayID(0)
        }
        return CGDirectDisplayID(screenNumber.uint32Value)
    }
}
