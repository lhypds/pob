import Foundation

struct AppSettings: Codable {
    var apiKey: String
}

class SettingsService {
    static let shared = SettingsService()

    private let fileManager = FileManager.default
    private let settingsFile: URL
    private let legacySettingsFile: URL

    private init() {
        let appSupportPath = fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        let appDirectory = appSupportPath.appendingPathComponent("Pob")
        let legacyAppDirectory = appSupportPath.appendingPathComponent("AII")
        try? fileManager.createDirectory(at: appDirectory, withIntermediateDirectories: true)
        settingsFile = appDirectory.appendingPathComponent("settings.json")
        legacySettingsFile = legacyAppDirectory.appendingPathComponent("settings.json")
    }

    func loadSettings() -> AppSettings {
        if let data = try? Data(contentsOf: settingsFile),
           let settings = try? JSONDecoder().decode(AppSettings.self, from: data) {
            return settings
        }

        if let data = try? Data(contentsOf: legacySettingsFile),
           let settings = try? JSONDecoder().decode(AppSettings.self, from: data) {
            _ = saveSettings(settings)
            return settings
        }

        // Backward compatibility: pull existing key from Keychain once if present.
        if let keychainKey = KeychainHelper.shared.retrieve(key: "openai_api_key"), !keychainKey.isEmpty {
            let migrated = AppSettings(apiKey: keychainKey)
            saveSettings(migrated)
            return migrated
        }

        return AppSettings(apiKey: "")
    }

    @discardableResult
    func saveSettings(_ settings: AppSettings) -> Bool {
        do {
            let data = try JSONEncoder().encode(settings)
            try data.write(to: settingsFile, options: .atomic)
            return true
        } catch {
            print("Error saving settings: \(error)")
            return false
        }
    }

    func getAPIKey() -> String {
        loadSettings().apiKey
    }

    @discardableResult
    func setAPIKey(_ value: String) -> Bool {
        var settings = loadSettings()
        settings.apiKey = value
        return saveSettings(settings)
    }
}
