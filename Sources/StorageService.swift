import Foundation
import Cocoa

class StorageService {
    static let shared = StorageService()
    
    private let fileManager = FileManager.default
    private let logsDirectory: URL
    
    private init() {
        let appSupportPath = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        let appDirectory = appSupportPath.appendingPathComponent("Pob")
        logsDirectory = appDirectory.appendingPathComponent("logs")
        let legacyLogsDirectory = appSupportPath.appendingPathComponent("AII/logs")

        if !fileManager.fileExists(atPath: logsDirectory.path),
           fileManager.fileExists(atPath: legacyLogsDirectory.path) {
            try? fileManager.createDirectory(at: appDirectory, withIntermediateDirectories: true)
            try? fileManager.moveItem(at: legacyLogsDirectory, to: logsDirectory)
        }
        
        // Create logs directory if it doesn't exist
        try? fileManager.createDirectory(at: logsDirectory, withIntermediateDirectories: true)
    }
    // MARK: - Public Methods
    
    func saveAnalysisResult(screenshot: NSImage, request: AnalysisRequest, response: AnalysisResponse) -> Bool {
        let timestamp = Int(Date().timeIntervalSince1970)
        let logFolder = logsDirectory.appendingPathComponent(String(timestamp))
        
        do {
            try fileManager.createDirectory(at: logFolder, withIntermediateDirectories: true)
            
            // Save screenshot
            if let tiffData = screenshot.tiffRepresentation,
               let bitmapImage = NSBitmapImageRep(data: tiffData),
               let pngData = bitmapImage.representation(using: .png, properties: [:]) {
                let screenshotPath = logFolder.appendingPathComponent("screenshot.png")
                try pngData.write(to: screenshotPath)
            }
            
            // Save request JSON
            let requestData = try JSONEncoder().encode(request)
            let requestPath = logFolder.appendingPathComponent("request.json")
            try requestData.write(to: requestPath)
            
            // Save response JSON
            let responseData = try JSONEncoder().encode(response)
            let responsePath = logFolder.appendingPathComponent("response.json")
            try responseData.write(to: responsePath)
            
            return true
        } catch {
            print("Error saving analysis result: \(error)")
            return false
        }
    }
    
    func getLogEntries() -> [LogEntry] {
        do {
            let contents = try fileManager.contentsOfDirectory(at: logsDirectory, includingPropertiesForKeys: nil)
            
            return contents
                .filter { $0.hasDirectoryPath }
                .compactMap { url in
                    guard let timestamp = Int(url.lastPathComponent) else { return nil }
                    
                    let responsePath = url.appendingPathComponent("response.json")
                    var status = "unknown"
                    
                    if let data = try? Data(contentsOf: responsePath),
                       let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                       let responseStatus = json["status"] as? String {
                        status = responseStatus
                    }
                    
                    return LogEntry(id: url.lastPathComponent, timestamp: timestamp, status: status)
                }
                .sorted { $0.timestamp > $1.timestamp }
        } catch {
            print("Error reading log entries: \(error)")
            return []
        }
    }
    
    func getEntryDetails(_ entry: LogEntry) -> (request: String, response: String) {
        let logFolder = logsDirectory.appendingPathComponent(entry.id)
        
        var requestStr = "No data"
        var responseStr = "No data"
        
        let requestPath = logFolder.appendingPathComponent("request.json")
        if let data = try? Data(contentsOf: requestPath),
           let json = try? JSONSerialization.jsonObject(with: data),
           let jsonData = try? JSONSerialization.data(withJSONObject: json, options: .prettyPrinted),
           let formatted = String(data: jsonData, encoding: .utf8) {
            requestStr = formatted
        }
        
        let responsePath = logFolder.appendingPathComponent("response.json")
        if let data = try? Data(contentsOf: responsePath),
           let json = try? JSONSerialization.jsonObject(with: data),
           let jsonData = try? JSONSerialization.data(withJSONObject: json, options: .prettyPrinted),
           let formatted = String(data: jsonData, encoding: .utf8) {
            responseStr = formatted
        }
        
        return (requestStr, responseStr)
    }
}

// MARK: - Models

struct AnalysisRequest: Codable {
    let timestamp: Int
    let model: String
    let prompt: String
    let image_filename: String
}

struct AnalysisResponse: Codable {
    let status: String
    let content: String?
    let error: String?
    let timestamp: Int
}
