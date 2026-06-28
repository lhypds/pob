import Foundation

enum AppLogger {
    private static let logFile: URL = SettingsService.shared.projectRoot.appendingPathComponent("app.log")
    private static let queue = DispatchQueue(label: "app.logger", qos: .utility)
    private static let formatter: ISO8601DateFormatter = ISO8601DateFormatter()

    static func log(_ message: String) {
        let timestamp = formatter.string(from: Date())
        let line = "[\(timestamp)] \(message)\n"
        guard let data = line.data(using: .utf8) else { return }
        queue.async {
            if FileManager.default.fileExists(atPath: logFile.path) {
                if let handle = try? FileHandle(forWritingTo: logFile) {
                    handle.seekToEndOfFile()
                    handle.write(data)
                    try? handle.close()
                }
            } else {
                try? data.write(to: logFile)
            }
        }
    }
}
