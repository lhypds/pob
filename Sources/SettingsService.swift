import Foundation
import AppKit

class SettingsService {
    static let shared = SettingsService()

    private let fileManager = FileManager.default

    var projectRoot: URL {
        URL(fileURLWithPath: fileManager.currentDirectoryPath)
    }

    private var settingsFile: URL { projectRoot.appendingPathComponent("settings.json") }
    private var envFile: URL { projectRoot.appendingPathComponent(".env") }
    private var instructionFile: URL { projectRoot.appendingPathComponent("instruction.txt") }
    private var logsFolder: URL { projectRoot.appendingPathComponent("logs") }

    private init() {
        ensureFiles()
    }

    private let defaultSettings: [String: Any] = [
        "model": "gpt-4o",
        "max_tokens": 2000,
        "editor": "system",
        "terminal": "system"
    ]

    private func serializeJSON(_ object: Any) -> String? {
        guard let data = try? JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted, .sortedKeys]),
              var string = String(data: data, encoding: .utf8) else { return nil }
        string = string.replacingOccurrences(of: "\" : ", with: "\": ")
        return string
    }

    private func ensureFiles() {
        if !fileManager.fileExists(atPath: settingsFile.path) {
            if let string = serializeJSON(defaultSettings) {
                try? string.write(to: settingsFile, atomically: true, encoding: .utf8)
            }
        } else {
            // Add any missing keys to an existing settings file
            if var json = (try? Data(contentsOf: settingsFile)).flatMap({ try? JSONSerialization.jsonObject(with: $0) as? [String: Any] }) {
                var changed = false
                for (key, value) in defaultSettings where json[key] == nil {
                    json[key] = value
                    changed = true
                }
                if changed, let string = serializeJSON(json) {
                    try? string.write(to: settingsFile, atomically: true, encoding: .utf8)
                }
            }
        }
        if !fileManager.fileExists(atPath: instructionFile.path) {
            let defaultText = "Describe what you see in this screenshot and identify any UI elements."
            try? defaultText.write(to: instructionFile, atomically: true, encoding: .utf8)
        }
        try? fileManager.createDirectory(at: logsFolder, withIntermediateDirectories: true)
    }

    func getAPIKey() -> String {
        guard let content = try? String(contentsOf: envFile, encoding: .utf8) else { return "" }
        for line in content.components(separatedBy: .newlines) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.hasPrefix("OPENAI_API_KEY=") {
                return String(trimmed.dropFirst("OPENAI_API_KEY=".count))
                    .trimmingCharacters(in: .whitespaces)
                    .trimmingCharacters(in: CharacterSet(charactersIn: "\"'"))
            }
        }
        return ""
    }

    func getModel() -> String {
        loadJSON(key: "model") as? String ?? "gpt-4o"
    }

    func getMaxTokens() -> Int {
        loadJSON(key: "max_tokens") as? Int ?? 2000
    }

    func getEditor() -> String {
        loadJSON(key: "editor") as? String ?? "system"
    }

    func getTerminal() -> String {
        loadJSON(key: "terminal") as? String ?? "system"
    }

    func getWindowFrame() -> NSRect? {
        guard let x = loadJSON(key: "window_x") as? Double,
              let y = loadJSON(key: "window_y") as? Double,
              let w = loadJSON(key: "window_width") as? Double,
              let h = loadJSON(key: "window_height") as? Double else { return nil }
        return NSRect(x: x, y: y, width: w, height: h)
    }

    func saveWindowFrame(_ frame: NSRect) {
        var json: [String: Any] = [:]
        if let data = try? Data(contentsOf: settingsFile),
           let existing = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
            json = existing
        }
        json["window_x"] = Double(frame.origin.x)
        json["window_y"] = Double(frame.origin.y)
        json["window_width"] = Double(frame.size.width)
        json["window_height"] = Double(frame.size.height)
        if let string = serializeJSON(json) {
            try? string.write(to: settingsFile, atomically: true, encoding: .utf8)
        }
    }

    func getInstruction() -> String {
        (try? String(contentsOf: instructionFile, encoding: .utf8)) ?? "Describe what you see in this screenshot."
    }

    func openSettingsFile() {
        openWithEditor(settingsFile)
    }

    func openInstructionFile() {
        openWithEditor(instructionFile)
    }

    func openLogsFolder() {
        try? fileManager.createDirectory(at: logsFolder, withIntermediateDirectories: true)
        NSWorkspace.shared.open(logsFolder)
    }

    private func openWithEditor(_ url: URL) {
        let process = Process()
        switch getEditor() {
        case "vscode":
            process.launchPath = "/usr/bin/open"
            process.arguments = ["-a", "Visual Studio Code", url.path]
        case "zed":
            process.launchPath = "/usr/bin/open"
            process.arguments = ["-a", "Zed", url.path]
        case "sublime_text":
            process.launchPath = "/usr/bin/open"
            process.arguments = ["-a", "Sublime Text", url.path]
        case "vim":
            process.launchPath = "/usr/bin/osascript"
            let escaped = url.path.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"")
            let cmd = "vim \\\"\(escaped)\\\""
            if getTerminal() == "iterm2" {
                process.arguments = ["-e", "tell application \"iTerm\" to create window with default profile command \"vim \\\"\(escaped)\\\"\"", "-e", "tell application \"iTerm\" to activate"]
            } else {
                process.arguments = ["-e", "tell application \"Terminal\" to do script \"\(cmd)\"", "-e", "tell application \"Terminal\" to activate"]
            }
        default: // "system"
            process.launchPath = "/usr/bin/open"
            process.arguments = ["-t", url.path]
        }
        try? process.run()
    }

    private func loadJSON(key: String) -> Any? {
        guard let data = try? Data(contentsOf: settingsFile),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return nil }
        return json[key]
    }
}
