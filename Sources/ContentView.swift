import SwiftUI
import AppKit

struct ContentView: View {
    @State private var isExecuting = false
    @State private var statusMessage = ""
    @State private var currentTask: Task<Void, Never>?

    var body: some View {
        ZStack {
            Color.gray.opacity(0.08)

            VStack {
                if !statusMessage.isEmpty {
                    Text(statusMessage)
                        .font(.caption)
                        .foregroundColor(.secondary)
                        .padding()
                }
                Spacer()
            }
        }
        .toolbar {
            ToolbarItemGroup(placement: .automatic) {
                Button(action: { SettingsService.shared.openSettingsFile() }) {
                    Image(systemName: "gearshape")
                }
                .help("Settings")

                Button(action: { SettingsService.shared.openLogsFolder() }) {
                    Image(systemName: "doc.text")
                }
                .help("Logs")

                Button(action: { SettingsService.shared.openInstructionFile() }) {
                    Image(systemName: "text.alignleft")
                }
                .help("Instruction")

                Button(action: isExecuting ? stop : execute) {
                    Image(systemName: isExecuting ? "stop.fill" : "play.fill")
                }
                .help(isExecuting ? "Stop" : "Execute")
            }
        }
        .onTapGesture {
            NSApplication.shared.activate(ignoringOtherApps: true)
            NSApplication.shared.windows.first?.makeKeyAndOrderFront(nil)
        }
    }

    private func stop() {
        currentTask?.cancel()
        currentTask = nil
        isExecuting = false
        statusMessage = "Stopped"
    }

    private func execute() {
        isExecuting = true
        statusMessage = "Capturing screenshot..."

        currentTask = Task {
            let window = NSApplication.shared.windows.first
            let screenshot: NSImage?
            if let window = window {
                screenshot = ScreenshotService.shared.captureWindowContentArea(window: window)
            } else {
                screenshot = ScreenshotService.shared.captureScreenshot()
            }

            guard let screenshot = screenshot else {
                await MainActor.run {
                    statusMessage = "Failed to capture screenshot"
                    isExecuting = false
                }
                return
            }

            guard !Task.isCancelled else { return }
            await MainActor.run { statusMessage = "Analyzing..." }

            let instruction = SettingsService.shared.getInstruction()

            guard let tiffData = screenshot.tiffRepresentation,
                  let bitmapImage = NSBitmapImageRep(data: tiffData),
                  let pngData = bitmapImage.representation(using: .png, properties: [:]) else {
                await MainActor.run {
                    statusMessage = "Failed to process screenshot"
                    isExecuting = false
                }
                return
            }
            let imageBase64 = pngData.base64EncodedString()

            let result = await OpenAIClient.shared.analyzeScreenshot(imageBase64: imageBase64, prompt: instruction)

            guard !Task.isCancelled else { return }

            await MainActor.run {
                StorageService.shared.saveResult(screenshot: screenshot, prompt: instruction, response: result.content)
                statusMessage = result.success ? "Done" : "Error: \(result.error ?? "Unknown")"
                isExecuting = false
                currentTask = nil
            }
        }
    }
}
