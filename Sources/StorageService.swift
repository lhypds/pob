import Foundation
import Cocoa

class StorageService {
    static let shared = StorageService()

    private let fileManager = FileManager.default

    private var logsDirectory: URL {
        let root = URL(fileURLWithPath: fileManager.currentDirectoryPath)
        return root.appendingPathComponent("logs")
    }

    private init() {
        try? fileManager.createDirectory(at: logsDirectory, withIntermediateDirectories: true)
    }

    @discardableResult
    func saveResult(screenshot: NSImage, prompt: String, response: String) -> Bool {
        let timestamp = Int(Date().timeIntervalSince1970)
        let logFolder = logsDirectory.appendingPathComponent("\(timestamp)")

        do {
            try fileManager.createDirectory(at: logFolder, withIntermediateDirectories: true)

            if let tiffData = screenshot.tiffRepresentation,
               let bitmapImage = NSBitmapImageRep(data: tiffData),
               let pngData = bitmapImage.representation(using: .png, properties: [:]) {
                try pngData.write(to: logFolder.appendingPathComponent("screenshot.png"))
            }

            try prompt.write(to: logFolder.appendingPathComponent("request.txt"), atomically: true, encoding: .utf8)
            try response.write(to: logFolder.appendingPathComponent("response.txt"), atomically: true, encoding: .utf8)

            return true
        } catch {
            print("Error saving result: \(error)")
            return false
        }
    }
}
