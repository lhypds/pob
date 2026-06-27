import SwiftUI
import AppKit

struct ContentView: View {
    @State private var isExecuting = false
    @State private var currentTask: Task<Void, Never>?
    @State private var isTargeting = false
    @State private var mousePosition: CGPoint? = nil
    @State private var verificationError: String? = nil
    @Environment(\.controlActiveState) private var controlActiveState

    var body: some View {
        ZStack {
            Color.gray.opacity(0.2)

            if isTargeting {
                MouseTrackingOverlay(
                    onPositionChange: { pos in mousePosition = pos },
                    onTap: { pt in
                        let scale = NSApplication.shared.windows.first?.screen?.backingScaleFactor ?? NSScreen.main?.backingScaleFactor ?? 2.0
                        let text = "(\(Int(pt.x * scale)), \(Int(pt.y * scale)))"
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(text, forType: .string)
                        isTargeting = false
                        mousePosition = nil
                    }
                )

                if let pos = mousePosition {
                    positionLabel(at: pos)
                }
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

                Button(action: {
                    isTargeting.toggle()
                    if !isTargeting { mousePosition = nil }
                }) {
                    Image(systemName: "scope")
                        .foregroundStyle(isTargeting ? Color.accentColor : (controlActiveState == .inactive ? Color.secondary : Color.primary))
                }
                .help(isTargeting ? "Stop Targeting" : "Target")

                Button(action: isExecuting ? stop : startVerification) {
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
        .alert("Cannot Execute", isPresented: Binding(
            get: { verificationError != nil },
            set: { if !$0 { verificationError = nil } }
        )) {
            Button("OK") { verificationError = nil }
        } message: {
            Text(verificationError ?? "")
        }
    }

    @ViewBuilder
    private func positionLabel(at pos: CGPoint) -> some View {
        let scale = NSApplication.shared.windows.first?.screen?.backingScaleFactor ?? NSScreen.main?.backingScaleFactor ?? 2.0
        Text("(\(Int(pos.x * scale)), \(Int(pos.y * scale)))")
            .font(.system(size: 11, weight: .medium, design: .monospaced))
            .foregroundColor(.white)
            .padding(.horizontal, 6)
            .padding(.vertical, 3)
            .background(Color.black.opacity(0.75))
            .cornerRadius(4)
            .position(x: pos.x + 55, y: max(14, pos.y - 14))
    }

    private func stop() {
        currentTask?.cancel()
        currentTask = nil
        isExecuting = false
        AppLogger.log("Stopped")
    }

    private func startVerification() {
        isExecuting = true

        currentTask = Task {
            let instruction = SettingsService.shared.getInstruction()

            let verifyMessages: [[String: Any]] = [
                [
                    "role": "user",
                    "content": """
                    You are verifying an automation instruction before it is executed.

                    Instruction:
                    \(instruction)

                    Check: does the instruction include a clear, specific position (coordinates, pixel offsets, \
                    or an unambiguous on-screen location) for every action that requires one?

                    Respond ONLY with JSON: {"ok": true, "reason": ""} if ready, \
                    or {"ok": false, "reason": "<explain what position info is missing>"} if not.
                    """
                ]
            ]

            let result = await OpenAIClient.shared.chat(messages: verifyMessages, jsonMode: true)

            await MainActor.run {
                guard !Task.isCancelled else { return }

                guard result.success,
                      let text = result.contentText,
                      let data = text.data(using: .utf8),
                      let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                    isExecuting = false
                    AppLogger.log("Verification error: \(result.error ?? "no response")")
                    return
                }

                let ok = json["ok"] as? Bool ?? false
                let reason = json["reason"] as? String ?? ""

                if ok {
                    AppLogger.log("Verification passed — executing")
                    executeMain()
                } else {
                    isExecuting = false
                    verificationError = reason.isEmpty ? "Position information is missing from the instruction." : reason
                    AppLogger.log("Verification failed: \(reason)")
                }
            }
        }
    }

    private func executeMain() {
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
            var pendingClick = false

            AppLogger.log("[\(sessionId)] Session started")

            // Take initial screenshot (no cursor overlay — no click requested yet).
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

            let systemMsg: [String: Any] = [
                "role": "system",
                "content": "You are a desktop automation assistant. All coordinates are screenshot pixel coordinates (origin = top-left of the screenshot, x right, y down). The app converts them to real screen positions — you never deal with screen or OS coordinates.\n\nWorkflow:\n1. Call move(dx, dy) to nudge the cursor by a pixel offset. You will see the cursor arrow — the arrow tip is where a click will land. Adjust until the tip is precisely on the target.\n2. Call click() to click at the current cursor position.\n\nThe cursor starts at the top-left (0, 0). Use move() repeatedly to walk it to the target element."
            ]
            messages.append(systemMsg)

            let userMsg: [String: Any] = [
                "role": "user",
                "content": [
                    ["type": "text", "text": instruction],
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
                    if pendingClick && text.uppercased().contains("CONFIRM") {
                        let curPos = MouseService.shared.virtualCursorPosition
                        AppLogger.log("[\(sessionId)] click confirmed at virtual(\(Int(curPos.x)), \(Int(curPos.y)))")
                        if let ctx = lastContext {
                            let cgPt = ctx.toCGEventPoint(pixelX: curPos.x, pixelY: curPos.y)
                            AppLogger.log("[\(sessionId)] executing click at screen(\(Int(cgPt.x)), \(Int(cgPt.y)))")
                            await MouseService.shared.performClick(at: cgPt)
                        }
                        break
                    } else if !text.isEmpty {
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
                        "content": "Continue the task. Use move(dx, dy) to adjust the cursor, or click() if the cursor tip is already on the target."
                    ])
                    continue
                }
                emptyResponseCount = 0

                for toolCall in result.toolCalls {
                    guard !Task.isCancelled else { break }

                    switch toolCall.name {

                    case "move":
                        let dx: CGFloat = (toolCall.arguments["dx"] as? Double).map { CGFloat($0) } ?? 0
                        let dy: CGFloat = (toolCall.arguments["dy"] as? Double).map { CGFloat($0) } ?? 0
                        MouseService.shared.moveCursorBy(dx: dx, dy: dy)
                        let newPos = MouseService.shared.virtualCursorPosition
                        AppLogger.log("[\(sessionId)] move(dx:\(Int(dx)), dy:\(Int(dy))) -> (\(Int(newPos.x)), \(Int(newPos.y)))")

                        messages.append([
                            "role": "tool",
                            "tool_call_id": toolCall.id,
                            "content": "Cursor moved by (\(Int(dx)), \(Int(dy))). New position: (\(Int(newPos.x)), \(Int(newPos.y)))."
                        ])

                        if let (newShot, newCtx) = captureWithCursor(window: window) {
                            lastContext = newCtx
                            lastScreenshot = newShot
                            if let b64 = toBase64(newShot) {
                                messages.append([
                                    "role": "user",
                                    "content": [
                                        ["type": "text", "text": "Cursor at (\(Int(newPos.x)), \(Int(newPos.y))). The arrow tip is the click point. Move again or call click()."],
                                        ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]]
                                    ] as [[String: Any]]
                                ])
                            }
                        }

                    case "click":
                        let curPos = MouseService.shared.virtualCursorPosition
                        AppLogger.log("[\(sessionId)] click() requested at (\(Int(curPos.x)), \(Int(curPos.y)))")

                        messages.append([
                            "role": "tool",
                            "tool_call_id": toolCall.id,
                            "content": "Click requested. Verifying position — reply CONFIRM or call move() to adjust."
                        ])

                        if let (verifyShot, verifyCtx) = captureWithCursor(window: window),
                           let b64 = toBase64(verifyShot) {
                            lastContext = verifyCtx
                            lastScreenshot = verifyShot

                            var contentParts: [[String: Any]] = [
                                ["type": "text", "text": "Cursor at (\(Int(curPos.x)), \(Int(curPos.y))). The arrow tip is the exact click point. Is it on the target? Reply CONFIRM or call move() to adjust."],
                                ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]]
                            ]

                            if let zoomed = ScreenshotService.shared.zoomedView(verifyShot, around: curPos),
                               let zoomB64 = toBase64(zoomed) {
                                contentParts.append(["type": "image_url", "image_url": ["url": "data:image/png;base64,\(zoomB64)"]])
                            }

                            messages.append(["role": "user", "content": contentParts])
                        }

                        pendingClick = true

                    default:
                        AppLogger.log("[\(sessionId)] Unknown tool: \(toolCall.name)")
                    }
                }

                if Task.isCancelled { break }
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

    // MARK: - Mouse Tracking

    private func makeTools() -> [[String: Any]] {
        [
            [
                "type": "function",
                "function": [
                    "name": "move",
                    "description": "Nudge the cursor by a relative pixel offset in screenshot space. All coordinates are screenshot pixels (origin = top-left, x increases right, y increases down). The app converts to real screen coordinates — you never deal with screen-level positions. You will receive a new screenshot showing the updated cursor position.",
                    "parameters": [
                        "type": "object",
                        "properties": [
                            "dx": ["type": "number", "description": "Horizontal offset in screenshot pixels. Positive = right, negative = left."],
                            "dy": ["type": "number", "description": "Vertical offset in screenshot pixels. Positive = down, negative = up."]
                        ],
                        "required": ["dx", "dy"]
                    ] as [String: Any]
                ] as [String: Any]
            ],
            [
                "type": "function",
                "function": [
                    "name": "click",
                    "description": "Click at the current cursor position. You will receive a verification screenshot; reply CONFIRM to execute the click, or call move() to adjust first.",
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

private struct MouseTrackingOverlay: NSViewRepresentable {
    var onPositionChange: (CGPoint?) -> Void
    var onTap: (CGPoint) -> Void

    func makeNSView(context: Context) -> TrackingNSView {
        let view = TrackingNSView()
        view.onPositionChange = onPositionChange
        view.onTap = onTap
        return view
    }

    func updateNSView(_ nsView: TrackingNSView, context: Context) {
        nsView.onPositionChange = onPositionChange
        nsView.onTap = onTap
    }
}

private class TrackingNSView: NSView {
    var onPositionChange: ((CGPoint?) -> Void)?
    var onTap: ((CGPoint) -> Void)?
    private var trackingArea: NSTrackingArea?

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let old = trackingArea { removeTrackingArea(old) }
        let area = NSTrackingArea(
            rect: bounds,
            options: [.mouseMoved, .mouseEnteredAndExited, .activeInKeyWindow],
            owner: self,
            userInfo: nil
        )
        addTrackingArea(area)
        trackingArea = area
    }

    override var isFlipped: Bool { true }

    override func mouseMoved(with event: NSEvent) {
        let pt = convert(event.locationInWindow, from: nil)
        onPositionChange?(pt)
    }

    override func mouseExited(with event: NSEvent) {
        onPositionChange?(nil)
    }

    override func mouseDown(with event: NSEvent) {
        let pt = convert(event.locationInWindow, from: nil)
        onTap?(pt)
        super.mouseDown(with: event)
    }
}
