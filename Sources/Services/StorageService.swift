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
        let entry: [String: Any] = [
            "start_time": Int(Date().timeIntervalSince1970),
            "settings": SettingsService.shared.getSettingsDict(),
        ]
        if let data = try? JSONSerialization.data(withJSONObject: entry, options: .prettyPrinted) {
            try? data.write(to: dir.appendingPathComponent("session.json"))
        }
        return sessionId
    }

    /// Creates a new plan folder under the session. Returns the plan ID (Unix timestamp string).
    func createPlan(sessionId: String) -> String {
        let planId = "\(Int(Date().timeIntervalSince1970))"
        let dir = logsDirectory.appendingPathComponent(sessionId).appendingPathComponent(planId)
        try? fileManager.createDirectory(at: dir, withIntermediateDirectories: true)
        return planId
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

    /// Writes plan.json, messages.json, response.json, and screenshot.png to logs/sessionId/planId/.
    /// Also creates numbered subdirectories (1/, 2/, 3/, ...) for each step in the plan.
    func savePlan(_ plan: String, messages: [[String: Any]], response: [String: Any], sessionId: String, planId: String, screenshot: NSImage? = nil) {
        let planDir = logsDirectory.appendingPathComponent(sessionId).appendingPathComponent(planId)
        try? fileManager.createDirectory(at: planDir, withIntermediateDirectories: true)
        try? plan.write(to: planDir.appendingPathComponent("plan.json"), atomically: true, encoding: .utf8)
        let stripped = stripImages(from: messages)
        if let data = try? JSONSerialization.data(withJSONObject: stripped, options: .prettyPrinted) {
            try? data.write(to: planDir.appendingPathComponent("messages.json"))
        }
        if let data = try? JSONSerialization.data(withJSONObject: response, options: .prettyPrinted) {
            try? data.write(to: planDir.appendingPathComponent("response.json"))
        }
        if let screenshot = screenshot,
           let tiff = screenshot.tiffRepresentation,
           let rep = NSBitmapImageRep(data: tiff),
           let png = rep.representation(using: .png, properties: [:]) {
            try? png.write(to: planDir.appendingPathComponent("screenshot.png"))
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

    /// Saves messages.json, response.json, and screenshot.png to logs/sessionId/planId/stepSeq/verification/.
    func saveVerification(sessionId: String, planId: String, stepSeq: Int, messages: [[String: Any]], response: [String: Any], screenshot: NSImage? = nil) {
        let verifyDir = logsDirectory
            .appendingPathComponent(sessionId)
            .appendingPathComponent(planId)
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
        if let screenshot = screenshot,
           let tiff = screenshot.tiffRepresentation,
           let rep = NSBitmapImageRep(data: tiff),
           let png = rep.representation(using: .png, properties: [:]) {
            try? png.write(to: verifyDir.appendingPathComponent("screenshot.png"))
        }
    }

    /// Returns the URL of the STATUS file for a plan step.
    func stepStatusFile(sessionId: String, planId: String, stepSeq: Int) -> URL {
        logsDirectory
            .appendingPathComponent(sessionId)
            .appendingPathComponent(planId)
            .appendingPathComponent("\(stepSeq)")
            .appendingPathComponent("status.txt")
    }

    /// Writes a status string to logs/sessionId/planId/stepSeq/status.txt.
    func writeStepStatus(_ status: String, sessionId: String, planId: String, stepSeq: Int) {
        let stepDir = logsDirectory
            .appendingPathComponent(sessionId)
            .appendingPathComponent(planId)
            .appendingPathComponent("\(stepSeq)")
        try? fileManager.createDirectory(at: stepDir, withIntermediateDirectories: true)
        try? status.write(to: stepDir.appendingPathComponent("status.txt"), atomically: true, encoding: .utf8)
    }

    /// Saves one conversation log entry under logs/sessionId/planId/stepSeq/unixtime/.
    func saveStepLog(sessionId: String, planId: String, stepSeq: Int, logId _: Int, messages: [[String: Any]], response: [String: Any], screenshot: NSImage? = nil) {
        let logDir = logsDirectory
            .appendingPathComponent(sessionId)
            .appendingPathComponent(planId)
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

    /// Writes start and end time to logs/sessionId/session.json.
    func saveSessionTimes(sessionId: String, startTime: Date, endTime: Date) {
        let dest = logsDirectory.appendingPathComponent(sessionId).appendingPathComponent("session.json")
        var entry: [String: Any] = (try? Data(contentsOf: dest))
            .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]
        entry["start_time"] = Int(startTime.timeIntervalSince1970)
        entry["end_time"] = Int(endTime.timeIntervalSince1970)
        if let data = try? JSONSerialization.data(withJSONObject: entry, options: .prettyPrinted) {
            try? data.write(to: dest)
        }
    }

    /// Recursively accumulates usage from all response.json files under sessionId/ and writes session.json.
    func saveSessionUsage(sessionId: String) {
        let sessionDir = logsDirectory.appendingPathComponent(sessionId)
        guard let enumerator = fileManager.enumerator(at: sessionDir, includingPropertiesForKeys: nil) else { return }

        var promptTokens = 0
        var completionTokens = 0
        var totalTokens = 0
        var reasoningTokens = 0
        var cachedTokens = 0

        for case let url as URL in enumerator where url.lastPathComponent == "response.json" {
            guard let data = try? Data(contentsOf: url),
                  let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let usage = json["usage"] as? [String: Any] else { continue }

            promptTokens += usage["prompt_tokens"] as? Int ?? 0
            completionTokens += usage["completion_tokens"] as? Int ?? 0
            totalTokens += usage["total_tokens"] as? Int ?? 0

            if let details = usage["completion_tokens_details"] as? [String: Any] {
                reasoningTokens += details["reasoning_tokens"] as? Int ?? 0
            }
            if let details = usage["prompt_tokens_details"] as? [String: Any] {
                cachedTokens += details["cached_tokens"] as? Int ?? 0
            }
        }

        let dest = sessionDir.appendingPathComponent("session.json")
        var summary: [String: Any] = (try? Data(contentsOf: dest))
            .flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } ?? [:]
        summary["usage"] = [
            "prompt_tokens": promptTokens,
            "completion_tokens": completionTokens,
            "total_tokens": totalTokens,
            "completion_tokens_details": ["reasoning_tokens": reasoningTokens],
            "prompt_tokens_details": ["cached_tokens": cachedTokens],
        ]
        if let data = try? JSONSerialization.data(withJSONObject: summary, options: .prettyPrinted) {
            try? data.write(to: dest)
        }
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
