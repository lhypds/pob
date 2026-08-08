import AppKit
import Foundation

/// UI-side view of an instance's files. The Go core (pob-core) owns
/// settings.json defaults, instruction.txt, macro.txt and the logs tree;
/// this service only resolves the project root, opens files in the user's
/// editor, persists the window frame and clears user files on request.
///
/// A machine has one instance and it keeps its id for good: the same
/// ~/.pob/<instance>/ directory every run, holding that instance's
/// settings.json, instruction.txt, macro.txt and logs/. Nothing is shared
/// between ids — pointing ~/.pob/INSTANCE at another one starts Pob from a
/// clean set of files.
class SettingsService {
    private let fileManager = FileManager.default

    /// ~/.pob/<instanceID>, everything this instance owns. Passed to pob-core
    /// via --instance so both sides work in the same directory.
    let instanceID: String

    /// Exclusive flock on <instanceID>/.lock, held for the process lifetime.
    /// It marks the directory as belonging to a running Pob, which is how a
    /// second Pob is detected — see claimInstance.
    ///
    /// Static, and taken exactly once. flock is per open file description,
    /// not per process, so a second open() of this same file would be refused
    /// by the lock this process already holds — Pob would find itself and
    /// conclude it was already running.
    private static var lockFD: Int32 = -1

    /// Shared project root (same for every instance in this process).
    static var projectRoot: URL {
        resolveProjectRoot(FileManager.default)
    }

    var projectRoot: URL {
        Self.resolveProjectRoot(fileManager)
    }

    private static func resolveProjectRoot(_ fileManager: FileManager) -> URL {
        // All Pob components share ~/.pob — the same root the pob CLI uses.
        let dir = fileManager.homeDirectoryForCurrentUser.appendingPathComponent(".pob")
        try? fileManager.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir
    }

    var instanceDir: URL {
        projectRoot.appendingPathComponent(instanceID)
    }

    private var settingsFile: URL {
        instanceDir.appendingPathComponent("settings.json")
    }

    private var instructionFile: URL {
        instanceDir.appendingPathComponent("instruction.txt")
    }

    private var macroFile: URL {
        instanceDir.appendingPathComponent("macro.txt")
    }

    private var logsFolder: URL {
        instanceDir.appendingPathComponent("logs")
    }

    init() {
        let fileManager = FileManager.default
        let root = Self.resolveProjectRoot(fileManager)

        instanceID = Self.resolveInstanceID(fileManager, root: root)
        let dir = root.appendingPathComponent(instanceID)
        try? fileManager.createDirectory(at: dir.appendingPathComponent("logs"), withIntermediateDirectories: true)
        // Normally already held — claimInstance ran at launch. This is the
        // path for anything that builds a SettingsService without it.
        Self.acquireInstanceLock(at: dir)
    }

    /// Every instance id starts with this.
    private static let instancePrefix = "pb-"

    /// The file under ~/.pob holding the machine's one instance id. Named in
    /// capitals like VERSION and SYSTEM: a marker the programs write and read,
    /// not a file to edit.
    private static let instancePointer = "INSTANCE"

    /// The machine's instance id — the same one on every run, recorded in
    /// ~/.pob/INSTANCE the first time it is worked out. This mirrors the Go
    /// core's ResolveInstanceID because either side can get there first: the
    /// shell resolves it to show in the toolbar and passes it to pob-core
    /// with --instance, but the CLI can reach ~/.pob without a shell at all.
    ///
    /// Without a pointer to read, the pb-* directory under ~/.pob that was
    /// used last is adopted rather than a new one started; the rest stay where
    /// they are as history.
    private static func resolveInstanceID(_ fileManager: FileManager, root: URL) -> String {
        let pointer = root.appendingPathComponent(instancePointer)

        if let contents = try? String(contentsOf: pointer, encoding: .utf8) {
            let id = contents.trimmingCharacters(in: .whitespacesAndNewlines)
            // Anything that isn't an instance id — a truncated or hand-edited
            // file — sends us back to working it out, rather than into a
            // directory named after junk.
            if id.hasPrefix(instancePrefix), !id.contains("/") {
                return id
            }
        }

        let id = mostRecentInstance(fileManager, root: root) ?? reserveInstanceID(fileManager, root: root)
        try? (id + "\n").write(to: pointer, atomically: true, encoding: .utf8)
        return id
    }

    /// The pb-* directory modified last, or nil when there are none. By
    /// modification time rather than by name: the directory is touched every
    /// time a session is written into it, so the newest is the one that was
    /// actually in use.
    private static func mostRecentInstance(_ fileManager: FileManager, root: URL) -> String? {
        let keys: [URLResourceKey] = [.isDirectoryKey, .contentModificationDateKey]
        guard let children = try? fileManager.contentsOfDirectory(
            at: root, includingPropertiesForKeys: keys) else { return nil }

        var newest: (id: String, at: Date)?
        for child in children where child.lastPathComponent.hasPrefix(instancePrefix) {
            guard let values = try? child.resourceValues(forKeys: Set(keys)),
                  values.isDirectory == true,
                  let at = values.contentModificationDate else { continue }
            if newest == nil || at > newest!.at {
                newest = (child.lastPathComponent, at)
            }
        }
        return newest?.id
    }

    /// Reserves a fresh `pb-<4 hex>` directory — the last two bytes of a new
    /// UID as lowercase hex, the same scheme the pico-hid firmware uses for
    /// its `ph-` board id. Shown in the toolbar beside the window buttons and
    /// used as the instance directory name, so the id on screen names the
    /// directory to look in.
    private static func reserveInstanceID(_ fileManager: FileManager, root: URL) -> String {
        while true {
            let id = instancePrefix + UUID().uuidString.suffix(4).lowercased()
            do {
                try fileManager.createDirectory(at: root.appendingPathComponent(id), withIntermediateDirectories: false)
                return id
            } catch CocoaError.fileWriteFileExists {
                continue // drawn one that already exists; draw another
            } catch {
                return id
            }
        }
    }

    /// True when this process holds the machine's instance. Anything that
    /// would drive the desktop — starting pob-core above all — has to check
    /// it, because the scene is built before the app delegate gets to refuse
    /// the launch, and a core started in that window would outlive the
    /// refusal.
    static var holdsInstanceLock: Bool { lockFD >= 0 }

    /// Claims this machine's instance for this process and reports whether
    /// it was free; false means another Pob already holds it. Called at
    /// launch, before any window is built, since only one Pob drives a
    /// desktop.
    ///
    /// Claiming and asking are the same operation on purpose. Asking first
    /// and taking it after would leave a gap for a second Pob to slip
    /// through — and, because flock belongs to the open file description
    /// rather than the process, the asking itself would collide with the lock
    /// this process had already taken.
    static func claimInstance() -> Bool {
        let fileManager = FileManager.default
        let root = resolveProjectRoot(fileManager)
        let dir = root.appendingPathComponent(resolveInstanceID(fileManager, root: root))
        try? fileManager.createDirectory(at: dir, withIntermediateDirectories: true)
        return acquireInstanceLock(at: dir)
    }

    deinit {
        // The lock is the process's, not this object's: SwiftUI can release
        // and rebuild a @StateObject while the app runs, and dropping the
        // lock there would let a second Pob in. The OS releases it on exit.
    }

    private func serializeJSON(_ object: Any) -> String? {
        guard let data = try? JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]),
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

    /// Takes the flock, or reports that someone else has it. Already holding
    /// it counts as success — this process is the someone else.
    @discardableResult
    private static func acquireInstanceLock(at instanceDir: URL) -> Bool {
        if lockFD >= 0 { return true }
        let fd = open(instanceDir.appendingPathComponent(".lock").path, O_CREAT | O_RDWR, 0o644)
        // A lock file that won't open is a broken ~/.pob, not a second Pob.
        // Start anyway rather than refuse over something unrelated.
        guard fd >= 0 else { return true }
        // Non-blocking: a held lock is an answer, not something to wait for.
        if flock(fd, LOCK_EX | LOCK_NB) != 0 {
            close(fd)
            return false
        }
        lockFD = fd
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
