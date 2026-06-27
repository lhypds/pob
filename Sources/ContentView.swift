import SwiftUI
import AppKit

struct ContentView: View {
    @State private var isExecuting = false
    @State private var currentTask: Task<Void, Never>?

    var body: some View {
        ZStack {
            Color.gray.opacity(0.2)
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
                .animation(nil, value: isExecuting)
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
        AppLogger.log("Stopped")
    }

    private func execute() {
        isExecuting = true

        currentTask = Task {
            let window = await MainActor.run { NSApplication.shared.windows.first }

            // Reset virtual cursor to (0, 0) — top-left of the capture area.
            MouseService.shared.resetCursor()

            let sessionId = StorageService.shared.createSession()
            var logId = 1
            var messages: [[String: Any]] = []
            var lastContext: ScreenshotContext? = nil
            var lastScreenshot: NSImage? = nil
            var emptyResponseCount = 0

            AppLogger.log("[\(sessionId)] Session started")

            // Take initial screenshot with cursor at (0, 0).
            guard let (initShot, initCtx) = captureWithCursor(window: window) else {
                AppLogger.log("Failed to capture screenshot")
                await MainActor.run { isExecuting = false }
                return
            }
            lastContext = initCtx
            lastScreenshot = initShot

            guard let initBase64 = toBase64(initShot) else {
                AppLogger.log("Failed to encode screenshot")
                await MainActor.run { isExecuting = false }
                return
            }

            let instruction = SettingsService.shared.getInstruction()
            let startPos = MouseService.shared.virtualCursorPosition

            let systemMsg: [String: Any] = [
                "role": "system",
                "content": "You are a desktop automation assistant controlling a virtual cursor. Use move() to position the cursor on a UI element, then examine the screenshot to verify the red crosshair cursor is exactly on the target. Keep calling move() to adjust until the cursor is precisely on the target, then call click(). Never call click() unless you have confirmed the cursor is on the correct element in the screenshot."
            ]
            messages.append(systemMsg)

            let userMsg: [String: Any] = [
                "role": "user",
                "content": [
                    ["type": "text", "text": "Current cursor position: (\(Int(startPos.x)), \(Int(startPos.y)))\n\n\(instruction)"],
                    ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(initBase64)"]]
                ] as [[String: Any]]
            ]
            messages.append(userMsg)

            let tools = makeTools()

            while !Task.isCancelled {
                AppLogger.log("[\(sessionId)/\(logId)] Analyzing...")

                let result = await OpenAIClient.shared.chat(messages: messages, tools: tools)

                let responseToSave: [String: Any] = result.success
                    ? result.rawAssistantMessage
                    : ["error": result.error ?? "Unknown error"]
                StorageService.shared.saveLog(sessionId: sessionId, logId: logId,
                                               messages: messages,
                                               response: responseToSave,
                                               screenshot: lastScreenshot)
                logId += 1

                if !result.success {
                    AppLogger.log("Error: \(result.error ?? "Unknown")")
                    break
                }

                messages.append(result.rawAssistantMessage)

                if result.toolCalls.isEmpty {
                    let text = result.contentText ?? ""
                    if !text.isEmpty {
                        AppLogger.log("[\(sessionId)] Done: \(text.prefix(100))")
                        break
                    }
                    // Empty response — prompt the AI to continue rather than ending the session.
                    emptyResponseCount += 1
                    if emptyResponseCount >= 3 {
                        AppLogger.log("[\(sessionId)] Too many empty responses, stopping.")
                        break
                    }
                    AppLogger.log("[\(sessionId)] Empty response, prompting to continue...")
                    messages.append([
                        "role": "user",
                        "content": "Continue the task. Check the cursor position in the screenshot and call move() to adjust if needed, or call click() if the cursor is on the correct target."
                    ])
                    continue
                }
                emptyResponseCount = 0

                var shouldStop = false

                for toolCall in result.toolCalls {
                    guard !Task.isCancelled else { break }

                    switch toolCall.name {

                    case "move":
                        let x: CGFloat = (toolCall.arguments["x"] as? Double).map { CGFloat($0) } ?? 0
                        let y: CGFloat = (toolCall.arguments["y"] as? Double).map { CGFloat($0) } ?? 0
                        MouseService.shared.moveCursor(to: CGPoint(x: x, y: y))
                        AppLogger.log("[\(sessionId)] move(\(Int(x)), \(Int(y)))")

                        // Tool result must be plain text (OpenAI rejects images in role:tool).
                        messages.append([
                            "role": "tool",
                            "tool_call_id": toolCall.id,
                            "content": "Cursor moved to (\(Int(x)), \(Int(y)))."
                        ])

                        // Send the updated screenshot as a follow-up user message.
                        if let (newShot, newCtx) = captureWithCursor(window: window) {
                            lastContext = newCtx
                            lastScreenshot = newShot
                            if let b64 = toBase64(newShot) {
                                messages.append([
                                    "role": "user",
                                    "content": [
                                        ["type": "text", "text": "Current cursor position: (\(Int(x)), \(Int(y))). Here is the updated screenshot."],
                                        ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]]
                                    ] as [[String: Any]]
                                ])
                            }
                        }

                    case "click":
                        let curPos = MouseService.shared.virtualCursorPosition
                        AppLogger.log("[\(sessionId)] click() at (\(Int(curPos.x)), \(Int(curPos.y)))")

                        if let ctx = lastContext {
                            let cgPt = ctx.toCGEventPoint(pixelX: curPos.x, pixelY: curPos.y)
                            await MouseService.shared.performClick(at: cgPt)
                        } else {
                            AppLogger.log("[\(sessionId)] Warning: no context for click")
                        }

                        messages.append([
                            "role": "tool",
                            "tool_call_id": toolCall.id,
                            "content": "Clicked at (\(Int(curPos.x)), \(Int(curPos.y)))."
                        ])
                        shouldStop = true

                    default:
                        AppLogger.log("[\(sessionId)] Unknown tool: \(toolCall.name)")
                    }
                }

                if shouldStop || Task.isCancelled { break }
            }

            await MainActor.run {
                isExecuting = false
                currentTask = nil
            }
        }
    }

    // MARK: - Helpers

    private func captureWithCursor(window: NSWindow?) -> (NSImage, ScreenshotContext)? {
        guard let w = window else { return nil }
        guard let (shot, ctx) = ScreenshotService.shared.captureWindowContentAreaWithContext(window: w) else { return nil }
        let pos = MouseService.shared.virtualCursorPosition
        let withCursor = ScreenshotService.shared.imageWithCursor(shot, at: pos)
        return (withCursor, ctx)
    }

    private func toBase64(_ image: NSImage) -> String? {
        guard let tiff = image.tiffRepresentation,
              let rep = NSBitmapImageRep(data: tiff),
              let png = rep.representation(using: .png, properties: [:]) else { return nil }
        return png.base64EncodedString()
    }

    private func makeTools() -> [[String: Any]] {
        [
            [
                "type": "function",
                "function": [
                    "name": "move",
                    "description": "Move the virtual cursor to a position on the screen. You will receive a new screenshot showing the cursor at the new position.",
                    "parameters": [
                        "type": "object",
                        "properties": [
                            "x": ["type": "number", "description": "X coordinate in screenshot pixels, measured from the left edge."],
                            "y": ["type": "number", "description": "Y coordinate in screenshot pixels, measured from the top edge."]
                        ],
                        "required": ["x", "y"]
                    ] as [String: Any]
                ] as [String: Any]
            ],
            [
                "type": "function",
                "function": [
                    "name": "click",
                    "description": "Click at the current cursor position. This performs an actual system left mouse click.",
                    "parameters": [
                        "type": "object",
                        "properties": [:] as [String: Any],
                        "required": [] as [String]
                    ] as [String: Any]
                ] as [String: Any]
            ]
        ]
    }
}
