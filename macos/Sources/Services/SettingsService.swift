import AppKit
import Foundation

/// UI-side view of the shared project files. The Go core (pob-core) owns
/// settings.json defaults, instruction.txt, macro.txt and the logs tree;
/// this service only resolves the project root, opens files in the user's
/// editor, persists the window frame and clears user files on request.
///
/// Each instance (one per window) reserves its own logs/<instance>/ directory
/// at creation and seeds it with a copy of the root settings.json, so
/// instances read and edit their own settings side by side. instruction.txt
/// and macro.txt stay shared at the root.
class SettingsService {
    private let fileManager = FileManager.default

    /// logs/<instanceID> reserved for this instance; holds its settings.json
    /// and the session logs the Go core writes. Passed to pob-core via
    /// --instance so both sides use the same directory.
    let instanceID: String

    /// Exclusive flock on logs/<instanceID>/.lock, held for the instance's
    /// lifetime so clearLogs (from this or another process) can tell the
    /// directory belongs to a running instance.
    private var lockFD: Int32 = -1

    /// Shared project root (same for every instance in this process).
    static var projectRoot: URL {
        resolveProjectRoot(FileManager.default)
    }

    var projectRoot: URL {
        Self.resolveProjectRoot(fileManager)
    }

    private static func resolveProjectRoot(_ fileManager: FileManager) -> URL {
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

    var instanceDir: URL {
        logsFolder.appendingPathComponent(instanceID)
    }

    private var settingsFile: URL {
        instanceDir.appendingPathComponent("settings.json")
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

    init() {
        let fileManager = FileManager.default
        let root = Self.resolveProjectRoot(fileManager)
        let logs = root.appendingPathComponent("logs")
        try? fileManager.createDirectory(at: logs, withIntermediateDirectories: true)

        // Reserve logs/<unixtime>/ exclusively; if another instance grabbed
        // the same second, bump until a free one is found (mirrors the Go
        // core's newInstanceID).
        var id = Int(Date().timeIntervalSince1970)
        while true {
            do {
                try fileManager.createDirectory(at: logs.appendingPathComponent(String(id)), withIntermediateDirectories: false)
                break
            } catch CocoaError.fileWriteFileExists {
                id += 1
            } catch {
                break
            }
        }
        instanceID = String(id)
        acquireInstanceLock()

        // Seed this instance's settings.json from the root template.
        let rootSettings = root.appendingPathComponent("settings.json")
        let instanceSettings = logs.appendingPathComponent(instanceID).appendingPathComponent("settings.json")
        if fileManager.fileExists(atPath: rootSettings.path),
           !fileManager.fileExists(atPath: instanceSettings.path)
        {
            try? fileManager.copyItem(at: rootSettings, to: instanceSettings)
        }
    }

    deinit {
        // Instances share this process (one per window) — release the lock
        // when the window closes so the directory becomes clearable.
        if lockFD >= 0 {
            close(lockFD)
        }
    }

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

    /// Appends one action line to macro.txt (same format as the Go core's
    /// AppendToMacro). Only called while no session is executing, so it never
    /// races with the Go core's own appends.
    func appendToMacro(_ line: String) {
        var content = getMacro()
        if !content.isEmpty, !content.hasSuffix("\n") { content += "\n" }
        content += line + "\n"
        try? content.write(to: macroFile, atomically: true, encoding: .utf8)
    }

    /// Removes the last non-empty line if it equals `expected`; used by the
    /// recorder to upgrade a click() into a doubleClick(). Returns whether a
    /// line was removed.
    func removeLastMacroLine(ifMatches expected: String) -> Bool {
        var lines = getMacro().components(separatedBy: "\n")
        while let last = lines.last, last.trimmingCharacters(in: .whitespaces).isEmpty {
            lines.removeLast()
        }
        guard lines.last == expected else { return false }
        lines.removeLast()
        var content = lines.joined(separator: "\n")
        if !content.isEmpty { content += "\n" }
        try? content.write(to: macroFile, atomically: true, encoding: .utf8)
        return true
    }

    func clearInstruction() {
        try? "".write(to: instructionFile, atomically: true, encoding: .utf8)
    }

    func clearLogs() {
        // Delete only directories of instances that are no longer running —
        // every live instance (this or another process) holds a flock on its
        // logs/<instance>/.lock, so a held lock means "in use, skip".
        if let children = try? fileManager.contentsOfDirectory(at: logsFolder, includingPropertiesForKeys: nil) {
            for child in children where child.lastPathComponent != instanceID {
                if isInstanceRunning(child) { continue }
                try? fileManager.removeItem(at: child)
            }
        }

        // Wipe this instance's own logs, carrying over its live settings.json.
        // The .lock goes down with the directory, so re-acquire it after.
        let settingsData = try? Data(contentsOf: settingsFile)
        if lockFD >= 0 {
            close(lockFD)
            lockFD = -1
        }
        try? fileManager.removeItem(at: instanceDir)
        try? fileManager.createDirectory(at: instanceDir, withIntermediateDirectories: true)
        acquireInstanceLock()
        if let settingsData {
            try? settingsData.write(to: settingsFile)
        }
        let appLog = projectRoot.appendingPathComponent("app.log")
        try? "".write(to: appLog, atomically: true, encoding: .utf8)
    }

    private func acquireInstanceLock() {
        let lockPath = logsFolder.appendingPathComponent(instanceID).appendingPathComponent(".lock").path
        lockFD = open(lockPath, O_CREAT | O_RDWR, 0o644)
        if lockFD >= 0 {
            flock(lockFD, LOCK_EX)
        }
    }

    /// True when a live instance still holds the directory's .lock. Entries
    /// without a lock file (stale instances, stray files) count as not running.
    private func isInstanceRunning(_ dir: URL) -> Bool {
        let fd = open(dir.appendingPathComponent(".lock").path, O_RDWR)
        guard fd >= 0 else { return false }
        defer { close(fd) }
        if flock(fd, LOCK_EX | LOCK_NB) == 0 {
            flock(fd, LOCK_UN)
            return false
        }
        return true
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
