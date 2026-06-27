import Cocoa
import Foundation

class ScreenshotService {
    static let shared = ScreenshotService()
    
    private init() {}
    
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
