import Foundation
import Cocoa

class StorageService {
    static let shared = StorageService()

    private let fileManager = FileManager.default

    private var logsDirectory: URL {
        SettingsService.shared.projectRoot.appendingPathComponent("logs")
    }

    private init() {
        try? fileManager.createDirectory(at: logsDirectory, withIntermediateDirectories: true)
    }

    /// Creates a new session folder. Returns the session ID (Unix timestamp string).
    func createSession() -> String {
        let sessionId = "\(Int(Date().timeIntervalSince1970))"
        let dir = logsDirectory.appendingPathComponent(sessionId)
        try? fileManager.createDirectory(at: dir, withIntermediateDirectories: true)
        return sessionId
    }

    /// Copies the current instruction.txt content into the session folder root.
    func saveInstruction(sessionId: String) {
        let instruction = SettingsService.shared.getInstruction()
        let dest = logsDirectory.appendingPathComponent(sessionId).appendingPathComponent("instruction.txt")
        try? instruction.write(to: dest, atomically: true, encoding: .utf8)
    }

    /// Copies the current macro.txt content into the session folder root.
    func saveMacro(sessionId: String) {
        let macro = SettingsService.shared.getMacro()
        let dest = logsDirectory.appendingPathComponent(sessionId).appendingPathComponent("macro.txt")
        try? macro.write(to: dest, atomically: true, encoding: .utf8)
    }

    /// Writes a plan JSON string to logs/sessionId/plan.json.
    func savePlan(_ plan: String, sessionId: String) {
        let dest = logsDirectory.appendingPathComponent(sessionId).appendingPathComponent("plan.json")
        try? plan.write(to: dest, atomically: true, encoding: .utf8)
    }

    /// Saves one conversation log entry under logs/sessionId/unixtime/.
    /// Image data in messages is stripped to keep files readable.
    func saveLog(sessionId: String, logId: Int, messages: [[String: Any]], response: [String: Any], screenshot: NSImage? = nil) {
        let logDir = logsDirectory.appendingPathComponent(sessionId).appendingPathComponent("\(Int(Date().timeIntervalSince1970))")
        do {
            try fileManager.createDirectory(at: logDir, withIntermediateDirectories: true)
        } catch { return }

        if let screenshot = screenshot,
           let tiff = screenshot.tiffRepresentation,
           let rep = NSBitmapImageRep(data: tiff),
           let png = rep.representation(using: .png, properties: [:]) {
            try? png.write(to: logDir.appendingPathComponent("screenshot.png"))
        }

        let stripped = stripImages(from: messages)
        if let data = try? JSONSerialization.data(withJSONObject: stripped, options: .prettyPrinted) {
            try? data.write(to: logDir.appendingPathComponent("messages.json"))
        }
        if let data = try? JSONSerialization.data(withJSONObject: response, options: .prettyPrinted) {
            try? data.write(to: logDir.appendingPathComponent("response.json"))
        }
    }

    /// Saves a screenshot to logs/sessionId/screenshots/<unixtime>.png.
    func saveScreenshot(_ image: NSImage, sessionId: String) {
        let screenshotsDir = logsDirectory.appendingPathComponent(sessionId).appendingPathComponent("screenshots")
        try? fileManager.createDirectory(at: screenshotsDir, withIntermediateDirectories: true)
        guard let tiff = image.tiffRepresentation,
              let rep = NSBitmapImageRep(data: tiff),
              let png = rep.representation(using: .png, properties: [:]) else { return }
        let filename = "\(Int(Date().timeIntervalSince1970)).png"
        try? png.write(to: screenshotsDir.appendingPathComponent(filename))
    }

    private func stripImages(from messages: [[String: Any]]) -> [[String: Any]] {
        messages.map { msg in
            var m = msg
            if let parts = m["content"] as? [[String: Any]] {
                m["content"] = parts.map { part -> [String: Any] in
                    var p = part
                    if p["type"] as? String == "image_url" {
                        p["image_url"] = ["url": "<image_stripped>"]
                    }
                    return p
                }
            }
            return m
        }
    }
}
