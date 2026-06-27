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

    private func ensureFiles() {
        if !fileManager.fileExists(atPath: settingsFile.path) {
            let json = """
            {
              "model": "gpt-4o",
              "max_tokens": 2000
            }
            """
            try? json.write(to: settingsFile, atomically: true, encoding: .utf8)
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

    func getInstruction() -> String {
        (try? String(contentsOf: instructionFile, encoding: .utf8)) ?? "Describe what you see in this screenshot."
    }

    func openSettingsFile() {
        openWithDefaultEditor(settingsFile)
    }

    func openInstructionFile() {
        openWithDefaultEditor(instructionFile)
    }

    func openLogsFolder() {
        try? fileManager.createDirectory(at: logsFolder, withIntermediateDirectories: true)
        NSWorkspace.shared.open(logsFolder)
    }

    private func openWithDefaultEditor(_ url: URL) {
        let process = Process()
        process.launchPath = "/usr/bin/open"
        process.arguments = ["-t", url.path]
        try? process.run()
    }

    private func loadJSON(key: String) -> Any? {
        guard let data = try? Data(contentsOf: settingsFile),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return nil }
        return json[key]
    }
}
