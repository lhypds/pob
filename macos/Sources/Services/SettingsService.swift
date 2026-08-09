import AppKit
import Foundation

/// UI-side view of the files Pob works with. The Go core (pob-core) owns
/// settings.json defaults, macro.psl and the logs tree; this service only
/// resolves the project root, opens files in the user's editor, persists the
/// window frame and clears user files on request.
///
/// A machine has one instance and it keeps its id for good: the same
/// ~/.pob/<instance>/ directory every run, holding that instance's macro.psl
/// and logs/. settings.json sits above them at ~/.pob and is shared — pointing
/// ~/.pob/INSTANCE at another id starts Pob on a clean macro, on a machine
/// that is already set up.
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

    /// The machine's settings, shared by every instance: the API key, the model
    /// and the port are how this machine works whichever instance it is
    /// running, so moving ~/.pob/INSTANCE does not mean setting them again.
    private var settingsFile: URL {
        projectRoot.appendingPathComponent("settings.json")
    }

    /// What this instance is, rather than how it is configured: its id, the
    /// name `pob new` gave it, when it last ran, and — while it runs — the pid
    /// and control port. The Go core owns it; the window frame is the shell's
    /// one entry, since where the window was is a property of the machine and
    /// not something anybody sets.
    private var instanceFile: URL {
        instanceDir.appendingPathComponent("instance.json")
    }

    /// This instance's Prompt Script Language program — what Record writes and
    /// Execute replays.
    private var macroFile: URL {
        instanceDir.appendingPathComponent("macro.psl")
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
    /// The pointer is the only thing that says which instance this machine
    /// is: with no readable one a fresh id is drawn and a new directory
    /// reserved, rather than an existing pb-* directory adopted. Deleting the
    /// file is therefore a way to start clean, and what is already there stays
    /// as history.
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

        let id = reserveInstanceID(fileManager, root: root)
        try? (id + "\n").write(to: pointer, atomically: true, encoding: .utf8)
        return id
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

    /// The frame the window was last left at, from instance.json — or from
    /// settings.json, where it used to be kept. Both are read because either
    /// side can get here first on the run that moves it: pob-core carries the
    /// frame over at startup, and this is called as the window is built. The
    /// fallback goes quiet once core has run, which drops it from settings.json.
    func getWindowFrame() -> NSRect? {
        readFrame(loadJSON(instanceFile)) ?? readFrame(loadJSON(settingsFile))
    }

    private func readFrame(_ json: [String: Any]) -> NSRect? {
        guard let x = json["window_x"] as? Double,
              let y = json["window_y"] as? Double,
              let w = json["window_width"] as? Double,
              let h = json["window_height"] as? Double else { return nil }
        return NSRect(x: x, y: y, width: w, height: h)
    }

    func saveWindowFrame(_ frame: NSRect) {
        var json = loadJSON(instanceFile)
        json["window_x"] = Double(frame.origin.x)
        json["window_y"] = Double(frame.origin.y)
        json["window_width"] = Double(frame.size.width)
        json["window_height"] = Double(frame.size.height)
        if let string = serializeJSON(json) {
            try? string.write(to: instanceFile, atomically: true, encoding: .utf8)
        }
    }

    func openSettingsFile() {
        openWithEditor(settingsFile)
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

    /// Appends one action line to macro.psl (same format as the Go core's
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
        let editor = getEditor()
        let process = Process()
        switch editor {
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
            openWithSystemEditor(url)
            return
        }

        // An editor settings.json names may not be installed here: the setting
        // travels between machines, the application does not. `open -a` and
        // osascript both report that in their exit status, which is the only
        // sign the file never appeared, so hand it to the system editor rather
        // than leave the toolbar button looking dead.
        process.terminationHandler = { [weak self] finished in
            guard finished.terminationStatus != 0 else { return }
            AppLogger.log("Settings: \(editor) could not open \(url.path) — using the system editor")
            DispatchQueue.main.async { self?.openWithSystemEditor(url) }
        }
        do {
            try process.run()
        } catch {
            AppLogger.log("Settings: cannot run \(process.launchPath ?? editor): \(error.localizedDescription)")
            openWithSystemEditor(url)
        }
    }

    /// The file in whatever is registered for it, TextEdit behind that. The
    /// last resort on macOS, and one that always has something behind it —
    /// unlike Linux, where xdg-open can come up empty.
    private func openWithSystemEditor(_ url: URL) {
        let process = Process()
        process.launchPath = "/usr/bin/open"
        process.arguments = ["-t", url.path]
        try? process.run()
    }

    private func loadJSON(key: String) -> Any? {
        loadJSON(settingsFile)[key]
    }

    private func loadJSON(_ file: URL) -> [String: Any] {
        guard let data = try? Data(contentsOf: file),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return [:] }
        return json
    }
}
