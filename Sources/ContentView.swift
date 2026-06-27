import SwiftUI
import AppKit

struct ContentView: View {
    @State private var isExecuting = false
    @State private var currentTask: Task<Void, Never>?
    @State private var isTargeting = false
    @State private var isClickThrough = true
    @State private var mousePosition: CGPoint? = nil
    @State private var verificationError: String? = nil
    @State private var maxStepWarning = false
    @State private var animatedCursorPos: CGPoint = CGPoint(x: 20, y: 20)
    @ObservedObject private var mouseService = MouseService.shared
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

            if isExecuting {
                let scale = NSScreen.main?.backingScaleFactor ?? 2.0
                let cursorImg = NSCursor.arrow.image
                let hot = NSCursor.arrow.hotSpot
                let viewX = animatedCursorPos.x / scale
                let viewY = animatedCursorPos.y / scale
                Image(nsImage: cursorImg)
                    .resizable()
                    .frame(width: cursorImg.size.width, height: cursorImg.size.height)
                    .position(
                        x: viewX + cursorImg.size.width / 2 - hot.x,
                        y: viewY + cursorImg.size.height / 2 - hot.y
                    )
                    .allowsHitTesting(false)
            }
        }
        .onChange(of: mouseService.displayPosition) { newPos in
            withAnimation(.easeOut(duration: 0.1)) {
                animatedCursorPos = newPos
            }
        }
        .onChange(of: isExecuting) { executing in
            NSApplication.shared.windows.first?.isMovable = !executing
            updateClickThrough()
        }
        .onChange(of: isTargeting) { _ in
            updateClickThrough()
        }
        .onAppear {
            updateClickThrough()
        }
        .toolbar {
            ToolbarItemGroup(placement: .automatic) {
                Button(action: {
                    isClickThrough.toggle()
                    updateClickThrough()
                }) {
                    Image(systemName: isClickThrough ? "hand.raised.slash" : "hand.raised")
                        .foregroundStyle(isClickThrough ? Color.accentColor : (controlActiveState == .inactive ? Color.secondary : Color.primary))
                }
                .help(isClickThrough ? "Click-Through On (click to disable)" : "Click-Through Off (click to enable)")

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
                    updateClickThrough()
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
        .alert("Verification Warning", isPresented: Binding(
            get: { verificationError != nil },
            set: { if !$0 { verificationError = nil } }
        )) {
            Button("Execute") {
                verificationError = nil
                executeMain()
            }
            Button("Cancel", role: .cancel) { verificationError = nil }
        } message: {
            Text(verificationError ?? "")
        }
        .alert("Warning", isPresented: $maxStepWarning) {
            Button("Continue") {
                maxStepWarning = false
                executeMain()
            }
            Button("Stop", role: .cancel) {
                maxStepWarning = false
            }
        } message: {
            Text("Max step exceed.")
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
        animatedCursorPos = CGPoint(x: 20, y: 20)

        currentTask = Task {
            let window = await MainActor.run { NSApplication.shared.windows.first }

            // Reset virtual cursor to (20, 20) — near top-left of the capture area.
            MouseService.shared.resetCursor()

            let sessionId = StorageService.shared.createSession()
            var logId = 1
            var messages: [[String: Any]] = []
            var lastContext: ScreenshotContext? = nil
            var lastScreenshot: NSImage? = nil
            var emptyResponseCount = 0
            var stepCount = 0
            let maxSteps = SettingsService.shared.getMaxSteps()

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
                "content": """
                You are a desktop automation assistant. All coordinates are screenshot pixel coordinates \
                (origin = top-left of the screenshot, x right, y down). The app converts them to real \
                screen positions — you never deal with screen or OS coordinates.

                Available actions:
                • move(dx, dy) — nudge the cursor by a relative pixel offset; you receive a new screenshot showing the updated cursor arrow tip.
                • click() — left-click at the current cursor position (executes immediately).
                • rightClick() — right-click at the current cursor position (executes immediately).
                • doubleClick() — double-click at the current cursor position (executes immediately).
                • drag(dx, dy) — drag from the current cursor position to current+(dx,dy); cursor ends at the new position.
                • scroll(dx, dy) — scroll at the current cursor position; dy>0 = down, dy<0 = up, dx>0 = right.
                • typeText(text) — type text at the current keyboard focus.
                • keyPress(key) — press a special key: return, tab, space, delete, escape, left/right/up/down, \
                home, end, pageup, pagedown, f1–f12, cmd+a/c/v/x/z/w/s/t/r.

                Workflow:
                1. Use move(dx, dy) repeatedly to position the cursor arrow tip precisely on the target.
                2. Call the appropriate action (click, rightClick, doubleClick, drag, scroll, type, keyPress).

                The cursor starts at (20, 20).
                """
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
                if stepCount >= maxSteps {
                    AppLogger.log("[\(sessionId)] Max step exceed.")
                    await MainActor.run {
                        maxStepWarning = true
                    }
                    break
                }
                stepCount += 1

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
                    emptyResponseCount += 1
                    if emptyResponseCount >= 3 {
                        AppLogger.log("[\(sessionId)] Too many empty responses, stopping.")
                        break
                    }
                    AppLogger.log("[\(sessionId)] Empty response, prompting to continue...")
                    messages.append([
                        "role": "user",
                        "content": "Continue the task. Use move(dx, dy) to position the cursor, then call the appropriate action."
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
                        AppLogger.log("[\(sessionId)] click at (\(Int(curPos.x)), \(Int(curPos.y)))")
                        if let ctx = lastContext {
                            let cgPt = ctx.toCGEventPoint(pixelX: curPos.x, pixelY: curPos.y)
                            await MouseService.shared.performClick(at: cgPt)
                        }
                        messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                         "content": "Clicked at (\(Int(curPos.x)), \(Int(curPos.y)))."])
                        if let (shot, ctx) = captureWithCursor(window: window), let b64 = toBase64(shot) {
                            lastContext = ctx; lastScreenshot = shot
                            messages.append(["role": "user", "content": [
                                ["type": "text", "text": "Clicked at (\(Int(curPos.x)), \(Int(curPos.y))). Screenshot after click:"],
                                ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]]
                            ] as [[String: Any]]])
                        }

                    case "rightClick":
                        let curPos = MouseService.shared.virtualCursorPosition
                        AppLogger.log("[\(sessionId)] rightClick at (\(Int(curPos.x)), \(Int(curPos.y)))")
                        if let ctx = lastContext {
                            await MouseService.shared.performRightClick(at: ctx.toCGEventPoint(pixelX: curPos.x, pixelY: curPos.y))
                        }
                        messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                         "content": "Right-clicked at (\(Int(curPos.x)), \(Int(curPos.y)))."])
                        if let (shot, ctx) = captureWithCursor(window: window), let b64 = toBase64(shot) {
                            lastContext = ctx; lastScreenshot = shot
                            messages.append(["role": "user", "content": [
                                ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]]
                            ] as [[String: Any]]])
                        }

                    case "doubleClick":
                        let curPos = MouseService.shared.virtualCursorPosition
                        AppLogger.log("[\(sessionId)] doubleClick at (\(Int(curPos.x)), \(Int(curPos.y)))")
                        if let ctx = lastContext {
                            await MouseService.shared.performDoubleClick(at: ctx.toCGEventPoint(pixelX: curPos.x, pixelY: curPos.y))
                        }
                        messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                         "content": "Double-clicked at (\(Int(curPos.x)), \(Int(curPos.y)))."])
                        if let (shot, ctx) = captureWithCursor(window: window), let b64 = toBase64(shot) {
                            lastContext = ctx; lastScreenshot = shot
                            messages.append(["role": "user", "content": [
                                ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]]
                            ] as [[String: Any]]])
                        }

                    case "drag":
                        let dx: CGFloat = (toolCall.arguments["dx"] as? Double).map { CGFloat($0) } ?? 0
                        let dy: CGFloat = (toolCall.arguments["dy"] as? Double).map { CGFloat($0) } ?? 0
                        let startPos = MouseService.shared.virtualCursorPosition
                        let endPos = CGPoint(x: startPos.x + dx, y: startPos.y + dy)
                        AppLogger.log("[\(sessionId)] drag(\(Int(dx)), \(Int(dy))) -> (\(Int(endPos.x)), \(Int(endPos.y)))")
                        if let ctx = lastContext {
                            let from = ctx.toCGEventPoint(pixelX: startPos.x, pixelY: startPos.y)
                            let to   = ctx.toCGEventPoint(pixelX: endPos.x,   pixelY: endPos.y)
                            await MouseService.shared.performDrag(from: from, to: to)
                        }
                        MouseService.shared.moveCursor(to: endPos)
                        messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                         "content": "Dragged to (\(Int(endPos.x)), \(Int(endPos.y)))."])
                        if let (shot, ctx) = captureWithCursor(window: window), let b64 = toBase64(shot) {
                            lastContext = ctx; lastScreenshot = shot
                            messages.append(["role": "user", "content": [
                                ["type": "text", "text": "Cursor at (\(Int(endPos.x)), \(Int(endPos.y)))."],
                                ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]]
                            ] as [[String: Any]]])
                        }

                    case "scroll":
                        let dx = (toolCall.arguments["dx"] as? Double).map { Int32($0) } ?? 0
                        let dy = (toolCall.arguments["dy"] as? Double).map { Int32($0) } ?? 0
                        let curPos = MouseService.shared.virtualCursorPosition
                        AppLogger.log("[\(sessionId)] scroll(dx:\(dx), dy:\(dy)) at (\(Int(curPos.x)), \(Int(curPos.y)))")
                        if let ctx = lastContext {
                            await MouseService.shared.performScroll(at: ctx.toCGEventPoint(pixelX: curPos.x, pixelY: curPos.y), dx: dx, dy: dy)
                        }
                        messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                         "content": "Scrolled dx:\(dx) dy:\(dy) at (\(Int(curPos.x)), \(Int(curPos.y)))."])
                        if let (shot, ctx) = captureWithCursor(window: window), let b64 = toBase64(shot) {
                            lastContext = ctx; lastScreenshot = shot
                            messages.append(["role": "user", "content": [
                                ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]]
                            ] as [[String: Any]]])
                        }

                    case "typeText":
                        let text = toolCall.arguments["text"] as? String ?? ""
                        AppLogger.log("[\(sessionId)] typeText(\"\(text.prefix(80))\")")
                        await MouseService.shared.performType(text: text)
                        messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                         "content": "Typed \"\(text)\"."])
                        if let (shot, ctx) = captureWithCursor(window: window), let b64 = toBase64(shot) {
                            lastContext = ctx; lastScreenshot = shot
                            messages.append(["role": "user", "content": [
                                ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]]
                            ] as [[String: Any]]])
                        }

                    case "keyPress":
                        let key = toolCall.arguments["key"] as? String ?? ""
                        AppLogger.log("[\(sessionId)] keyPress(\"\(key)\")")
                        await MouseService.shared.performKeyPress(key: key)
                        messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                         "content": "Pressed \"\(key)\"."])
                        if let (shot, ctx) = captureWithCursor(window: window), let b64 = toBase64(shot) {
                            lastContext = ctx; lastScreenshot = shot
                            messages.append(["role": "user", "content": [
                                ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]]
                            ] as [[String: Any]]])
                        }

                    default:
                        AppLogger.log("[\(sessionId)] Unknown tool: \(toolCall.name)")
                    }
                }

                if Task.isCancelled { break }
            }

            let wasCancelled = Task.isCancelled
            await MainActor.run {
                isExecuting = false
                currentTask = nil
                if !wasCancelled {
                    let soundCmd = SettingsService.shared.getStopHook()
                    if !soundCmd.isEmpty {
                        let p = Process()
                        p.launchPath = "/bin/sh"
                        p.arguments = ["-c", soundCmd]
                        try? p.run()
                    }
                }
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

    private func updateClickThrough() {
        AppDelegate.shared?.setClickThrough(isClickThrough && !isExecuting && !isTargeting)
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
                    "description": "Left-click at the current cursor position. Executes immediately; you will receive a screenshot after the click.",
                    "parameters": [
                        "type": "object",
                        "properties": [:] as [String: Any],
                        "required": [] as [String]
                    ] as [String: Any]
                ] as [String: Any]
            ],
            [
                "type": "function",
                "function": [
                    "name": "rightClick",
                    "description": "Right-click at the current cursor position. Executes immediately.",
                    "parameters": [
                        "type": "object",
                        "properties": [:] as [String: Any],
                        "required": [] as [String]
                    ] as [String: Any]
                ] as [String: Any]
            ],
            [
                "type": "function",
                "function": [
                    "name": "doubleClick",
                    "description": "Double-click at the current cursor position. Executes immediately.",
                    "parameters": [
                        "type": "object",
                        "properties": [:] as [String: Any],
                        "required": [] as [String]
                    ] as [String: Any]
                ] as [String: Any]
            ],
            [
                "type": "function",
                "function": [
                    "name": "drag",
                    "description": "Drag from the current cursor position by (dx, dy) screenshot pixels. The cursor ends at the new position and you receive an updated screenshot.",
                    "parameters": [
                        "type": "object",
                        "properties": [
                            "dx": ["type": "number", "description": "Horizontal drag offset in screenshot pixels. Positive = right."],
                            "dy": ["type": "number", "description": "Vertical drag offset in screenshot pixels. Positive = down."]
                        ] as [String: Any],
                        "required": ["dx", "dy"]
                    ] as [String: Any]
                ] as [String: Any]
            ],
            [
                "type": "function",
                "function": [
                    "name": "scroll",
                    "description": "Scroll at the current cursor position. dy > 0 = scroll down, dy < 0 = scroll up, dx > 0 = scroll right.",
                    "parameters": [
                        "type": "object",
                        "properties": [
                            "dx": ["type": "number", "description": "Horizontal scroll amount in pixels."],
                            "dy": ["type": "number", "description": "Vertical scroll amount in pixels. Positive = down."]
                        ] as [String: Any],
                        "required": ["dx", "dy"]
                    ] as [String: Any]
                ] as [String: Any]
            ],
            [
                "type": "function",
                "function": [
                    "name": "typeText",
                    "description": "Type text at the current keyboard focus.",
                    "parameters": [
                        "type": "object",
                        "properties": [
                            "text": ["type": "string", "description": "The text to type."]
                        ] as [String: Any],
                        "required": ["text"]
                    ] as [String: Any]
                ] as [String: Any]
            ],
            [
                "type": "function",
                "function": [
                    "name": "keyPress",
                    "description": "Press a special key. Supported: return, tab, space, delete, escape, left, right, up, down, home, end, pageup, pagedown, f1–f12, cmd+a/c/v/x/z/w/s/t/r.",
                    "parameters": [
                        "type": "object",
                        "properties": [
                            "key": ["type": "string", "description": "Key name, e.g. \"return\", \"escape\", \"cmd+v\"."]
                        ] as [String: Any],
                        "required": ["key"]
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

    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let old = trackingArea { removeTrackingArea(old) }
        let area = NSTrackingArea(
            rect: bounds,
            options: [.mouseMoved, .mouseEnteredAndExited, .activeAlways],
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
