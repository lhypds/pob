import AppKit
import Foundation

/// UI-side view of the shared project files. The Go core (pob-core) owns
/// settings.json defaults, instruction.txt, macro.txt and the logs tree;
/// this service only resolves the project root, opens files in the user's
/// editor, persists the window frame and clears user files on request.
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
        if fileManager.fileExists(atPath: cwd.appendingPathComponent("core").path) {
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

    private init() {}

    private func serializeJSON(_ object: Any) -> String? {
        guard let data = try? JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted, .sortedKeys]),
              var string = String(data: data, encoding: .utf8) else { return nil }
        string = string.replacingOccurrences(of: "\" : ", with: "\": ")
        return string
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
