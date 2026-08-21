import Foundation

/// Which of Pob's two logs a shell message belongs in.
///
/// `~/.pob/app.log` is the machine's record across instances and is kept short
/// on purpose: the app starting and stopping, an instance starting and
/// stopping, and errors. Read on its own it should answer "did it come up, and
/// did anything break" without scrolling.
///
/// Everything else is detail, and detail belongs to the instance —
/// `~/.pob/<instance>/instance.log`, the file the toolbar's .log button
/// opens and the one pob-core writes its own steps to. Every message logged
/// here lands there whatever its level, so the shell's side of a run reads in
/// order beside the core's.
///
/// So: `log` for detail, `event` for the lifecycle lines and `error` for
/// failures. Both files are appended to a line at a time, as the core does, so
/// two processes writing at once interleave without corrupting each other.
enum AppLogger {
    private static let appLog: URL = SettingsService.projectRoot.appendingPathComponent("app.log")
    private static let instanceLog: URL = SettingsService.instanceLogFile
    private static let queue = DispatchQueue(label: "app.logger", qos: .utility)
    /// The machine's own clock with the zone's offset on the end, matching
    /// applog.TimeLayout in the core: a log is read next to a run whoever is at
    /// the machine remembers the time of, so it is written in that time rather
    /// than in UTC. `xxxxx` always writes an offset — `+00:00` rather than `Z`
    /// — so every row is the same width and the two writers agree.
    private static let formatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone.current
        formatter.dateFormat = "yyyy-MM-dd'T'HH:mm:ss.SSSSSSxxxxx"
        return formatter
    }()

    /// Detail: the instance log alone.
    static func log(_ message: String) {
        write(level: "INFO", toAppLog: false, message)
    }

    /// A line app.log is kept for — the app or an instance starting or
    /// stopping. Goes to both logs.
    static func event(_ message: String) {
        write(level: "INFO", toAppLog: true, message)
    }

    /// A failure. Goes to both logs, marked ERROR, so app.log answers what went
    /// wrong and instance.log keeps it beside the detail that led there.
    static func error(_ message: String) {
        write(level: "ERROR", toAppLog: true, message)
    }

    private static func write(level: String, toAppLog: Bool, _ message: String) {
        let timestamp = formatter.string(from: Date())
        let marked = level == "INFO" ? message : "\(level) \(message)"
        let appLine = "[\(timestamp)] \(marked)\n"
        // The instance log names its level in a column of its own, the way
        // pob-core writes INSTANCE START and the rest of its events.
        let instanceLine = "[\(timestamp)] \(level) \(message)\n"
        queue.async {
            if toAppLog { append(appLine, to: appLog) }
            append(instanceLine, to: instanceLog)
        }
    }

    private static func append(_ line: String, to file: URL) {
        guard let data = line.data(using: .utf8) else { return }
        if FileManager.default.fileExists(atPath: file.path) {
            if let handle = try? FileHandle(forWritingTo: file) {
                handle.seekToEndOfFile()
                handle.write(data)
                try? handle.close()
            }
        } else {
            try? data.write(to: file)
        }
    }
}
