import AppKit
import Foundation

class SettingsService {
    static let shared = SettingsService()

    private let fileManager = FileManager.default

    var projectRoot: URL {
        let cwd = URL(fileURLWithPath: fileManager.currentDirectoryPath)
        // Dev workflow: start.sh runs the binary from the project root, which has settings.json
        if fileManager.fileExists(atPath: cwd.appendingPathComponent("settings.json").path) {
            return cwd
        }
        // Also accept a directory that looks like the project source tree
        if fileManager.fileExists(atPath: cwd.appendingPathComponent("Sources").path) {
            return cwd
        }
        // Production: app launched from Finder/Applications — use Application Support
        let appSupport = fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        let dir = appSupport.appendingPathComponent("Pob")
        try? fileManager.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir
    }

    private var settingsFile: URL {
        projectRoot.appendingPathComponent("settings.json")
    }

    private var instructionFile: URL {
        projectRoot.appendingPathComponent("instruction.txt")
    }

    private var macroFile: URL {
        projectRoot.appendingPathComponent("macro.txt")
    }

    private var logsFolder: URL {
        projectRoot.appendingPathComponent("logs")
    }

    private init() {
        ensureFiles()
    }

    private let defaultSettings: [String: Any] = [
        "openai_api_key": "",
        "model": "gpt-4o",
        "mcp_server_port": 8032,
        "start_mcp": true,
        "max_steps": 12,
        "max_resumes": 5,
        "max_steplogs": 10,
        "macro_default_delay": 1000,
        "editor": "system",
        "terminal": "system",
        "stop_hook": "",
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
        if !fileManager.fileExists(atPath: macroFile.path) {
            try? "".write(to: macroFile, atomically: true, encoding: .utf8)
        }
        try? fileManager.createDirectory(at: logsFolder, withIntermediateDirectories: true)
    }

    func getAPIKey() -> String {
        return loadJSON(key: "openai_api_key") as? String ?? ""
    }

    func getMCPPort() -> UInt16 {
        if let n = loadJSON(key: "mcp_server_port") as? Int, n > 0 { return UInt16(n) }
        if let n = loadJSON(key: "mcp_server_port") as? Double, n > 0 { return UInt16(n) }
        return 8032
    }

    func getModel() -> String {
        loadJSON(key: "model") as? String ?? "gpt-4o"
    }

    func getMaxSteps() -> Int {
        if let value = loadJSON(key: "max_steps") as? Int {
            return max(1, value)
        }
        if let value = loadJSON(key: "max_steps") as? Double {
            return max(1, Int(value))
        }
        if let value = loadJSON(key: "max_steps") as? String,
           let intValue = Int(value.trimmingCharacters(in: .whitespacesAndNewlines))
        {
            return max(1, intValue)
        }
        return 12
    }

    func getMaxStepLogs() -> Int {
        if let value = loadJSON(key: "max_steplogs") as? Int {
            return max(1, value)
        }
        if let value = loadJSON(key: "max_steplogs") as? Double {
            return max(1, Int(value))
        }
        if let value = loadJSON(key: "max_steplogs") as? String,
           let intValue = Int(value.trimmingCharacters(in: .whitespacesAndNewlines))
        {
            return max(1, intValue)
        }
        return 10
    }

    func getMaxResumes() -> Int {
        if let value = loadJSON(key: "max_resumes") as? Int {
            return max(1, value)
        }
        if let value = loadJSON(key: "max_resumes") as? Double {
            return max(1, Int(value))
        }
        if let value = loadJSON(key: "max_resumes") as? String,
           let intValue = Int(value.trimmingCharacters(in: .whitespacesAndNewlines))
        {
            return max(1, intValue)
        }
        return 5
    }

    func getEditor() -> String {
        loadJSON(key: "editor") as? String ?? "system"
    }

    func getTerminal() -> String {
        loadJSON(key: "terminal") as? String ?? "system"
    }

    func getStopHook() -> String {
        loadJSON(key: "stop_hook") as? String ?? ""
    }

    func getMacroDefaultDelay() -> Int {
        if let value = loadJSON(key: "macro_default_delay") as? Int { return max(0, value) }
        if let value = loadJSON(key: "macro_default_delay") as? Double { return max(0, Int(value)) }
        return 1000
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
           let existing = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        {
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

    func openMacroFile() {
        openWithEditor(macroFile)
    }

    func getMacro() -> String {
        (try? String(contentsOf: macroFile, encoding: .utf8)) ?? ""
    }

    func clearMacro() {
        try? "".write(to: macroFile, atomically: true, encoding: .utf8)
    }

    func clearInstruction() {
        try? "".write(to: instructionFile, atomically: true, encoding: .utf8)
    }

    func clearLogs() {
        try? fileManager.removeItem(at: logsFolder)
        try? fileManager.createDirectory(at: logsFolder, withIntermediateDirectories: true)
        let appLog = projectRoot.appendingPathComponent("app.log")
        try? "".write(to: appLog, atomically: true, encoding: .utf8)
    }

    func appendToMacro(_ line: String) {
        guard let data = (line + "\n").data(using: .utf8) else { return }
        if let handle = try? FileHandle(forWritingTo: macroFile) {
            defer { try? handle.close() }
            handle.seekToEndOfFile()
            handle.write(data)
        }
    }

    func openLogsFolder() {
        try? fileManager.createDirectory(at: logsFolder, withIntermediateDirectories: true)
        NSWorkspace.shared.open(logsFolder)
    }

    func openAppLog() {
        let appLog = projectRoot.appendingPathComponent("app.log")
        openWithEditor(appLog)
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
