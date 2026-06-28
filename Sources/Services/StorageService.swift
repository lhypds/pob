import Cocoa
import Foundation

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

    /// Writes plan.json, messages.json, and response.json to logs/sessionId/plan/.
    /// Also creates numbered subdirectories (1/, 2/, 3/, ...) for each step in the plan.
    func savePlan(_ plan: String, messages: [[String: Any]], response: [String: Any], sessionId: String) {
        let planDir = logsDirectory.appendingPathComponent(sessionId).appendingPathComponent("plan")
        try? fileManager.createDirectory(at: planDir, withIntermediateDirectories: true)
        try? plan.write(to: planDir.appendingPathComponent("plan.json"), atomically: true, encoding: .utf8)
        let stripped = stripImages(from: messages)
        if let data = try? JSONSerialization.data(withJSONObject: stripped, options: .prettyPrinted) {
            try? data.write(to: planDir.appendingPathComponent("messages.json"))
        }
        if let data = try? JSONSerialization.data(withJSONObject: response, options: .prettyPrinted) {
            try? data.write(to: planDir.appendingPathComponent("response.json"))
        }
        if let planData = plan.data(using: .utf8),
           let json = try? JSONSerialization.jsonObject(with: planData) as? [String: Any],
           let steps = json["steps"] as? [[String: Any]]
        {
            for step in steps {
                guard let seq = step["sequence"] as? Int else { continue }
                let stepDir = planDir.appendingPathComponent("\(seq)")
                try? fileManager.createDirectory(at: stepDir, withIntermediateDirectories: true)
                let stepEntry: [String: Any] = [
                    "sequence": seq,
                    "instruction": step["instruction"] as? String ?? "",
                    "expectation": step["expectation"] as? String ?? "",
                ]
                if let data = try? JSONSerialization.data(withJSONObject: stepEntry, options: .prettyPrinted) {
                    try? data.write(to: stepDir.appendingPathComponent("step.json"))
                }
            }
        }
    }

    /// Saves one conversation log entry under logs/sessionId/unixtime/.
    /// Image data in messages is stripped to keep files readable.
    func saveLog(sessionId: String, logId _: Int, messages: [[String: Any]], response: [String: Any], screenshot: NSImage? = nil) {
        let logDir = logsDirectory.appendingPathComponent(sessionId).appendingPathComponent("\(Int(Date().timeIntervalSince1970))")
        do {
            try fileManager.createDirectory(at: logDir, withIntermediateDirectories: true)
        } catch { return }

        if let screenshot = screenshot,
           let tiff = screenshot.tiffRepresentation,
           let rep = NSBitmapImageRep(data: tiff),
           let png = rep.representation(using: .png, properties: [:])
        {
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

    /// Saves messages.json and response.json to logs/sessionId/plan/stepSeq/verification/.
    func saveVerification(sessionId: String, stepSeq: Int, messages: [[String: Any]], response: [String: Any]) {
        let verifyDir = logsDirectory
            .appendingPathComponent(sessionId)
            .appendingPathComponent("plan")
            .appendingPathComponent("\(stepSeq)")
            .appendingPathComponent("verification")
        try? fileManager.createDirectory(at: verifyDir, withIntermediateDirectories: true)
        let stripped = stripImages(from: messages)
        if let data = try? JSONSerialization.data(withJSONObject: stripped, options: .prettyPrinted) {
            try? data.write(to: verifyDir.appendingPathComponent("messages.json"))
        }
        if let data = try? JSONSerialization.data(withJSONObject: response, options: .prettyPrinted) {
            try? data.write(to: verifyDir.appendingPathComponent("response.json"))
        }
    }

    /// Renames logs/sessionId/plan/ to logs/sessionId/plan_<unixtime>/ to archive it before a restart.
    func archivePlan(sessionId: String) {
        let planDir = logsDirectory.appendingPathComponent(sessionId).appendingPathComponent("plan")
        guard fileManager.fileExists(atPath: planDir.path) else { return }
        let archiveName = "plan_\(Int(Date().timeIntervalSince1970))"
        let archiveDir = logsDirectory.appendingPathComponent(sessionId).appendingPathComponent(archiveName)
        try? fileManager.moveItem(at: planDir, to: archiveDir)
    }

    /// Returns the URL of the STATUS file for a plan step.
    func stepStatusFile(sessionId: String, stepSeq: Int) -> URL {
        logsDirectory
            .appendingPathComponent(sessionId)
            .appendingPathComponent("plan")
            .appendingPathComponent("\(stepSeq)")
            .appendingPathComponent("status.txt")
    }

    /// Writes a status string to logs/sessionId/plan/stepSeq/STATUS.
    func writeStepStatus(_ status: String, sessionId: String, stepSeq: Int) {
        let stepDir = logsDirectory
            .appendingPathComponent(sessionId)
            .appendingPathComponent("plan")
            .appendingPathComponent("\(stepSeq)")
        try? fileManager.createDirectory(at: stepDir, withIntermediateDirectories: true)
        try? status.write(to: stepDir.appendingPathComponent("status.txt"), atomically: true, encoding: .utf8)
    }

    /// Saves one conversation log entry under logs/sessionId/plan/stepSeq/unixtime/.
    func saveStepLog(sessionId: String, stepSeq: Int, logId _: Int, messages: [[String: Any]], response: [String: Any], screenshot: NSImage? = nil) {
        let logDir = logsDirectory
            .appendingPathComponent(sessionId)
            .appendingPathComponent("plan")
            .appendingPathComponent("\(stepSeq)")
            .appendingPathComponent("\(Int(Date().timeIntervalSince1970))")
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
