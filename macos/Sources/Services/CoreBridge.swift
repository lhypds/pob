import AppKit
import Combine

/// Spawns and talks to the Go core (pob-core) over stdin/stdout using
/// line-delimited JSON-RPC. The Go side owns the agent loop, LLM calls, logs
/// and the MCP server; this bridge answers its perception/operation requests
/// (screenshot, mouse, keyboard, UI dialogs) and forwards user commands
/// (run / stop / recording) the other way.
final class CoreBridge: ObservableObject {
    static let shared = CoreBridge()

    /// True while the Go core is executing a session; drives the cursor
    /// overlay, window lock and click-through logic in the UI.
    @Published var isExecuting = false
    /// Set when the Go core asks the user whether to continue past max_steps.
    @Published var showMaxStepWarning = false
    /// Incremented whenever the Go core requests the screenshot flash effect.
    @Published var flashTick = 0

    private var process: Process?
    private var stdinHandle: FileHandle?
    private var buffer = Data()
    private let writeQueue = DispatchQueue(label: "corebridge.write")

    /// Coordinate context of the most recent capture; used to convert
    /// screenshot pixels to CGEvent screen positions for mouse actions.
    private var lastContext: ScreenshotContext?
    /// Pending ui.confirmMaxStep request id, answered via resolveMaxStep.
    private var maxStepRequestId: Any?

    private init() {}

    // MARK: - Process lifecycle

    func start() {
        let root = SettingsService.shared.projectRoot

        guard let binary = locateCoreBinary(projectRoot: root) else {
            AppLogger.log("CoreBridge: pob-core binary not found — run ./setup.sh")
            return
        }

        let process = Process()
        process.executableURL = binary
        process.arguments = ["--root", root.path]
        process.currentDirectoryURL = root

        let stdinPipe = Pipe()
        let stdoutPipe = Pipe()
        process.standardInput = stdinPipe
        process.standardOutput = stdoutPipe
        process.standardError = FileHandle.standardError

        stdoutPipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
            let data = handle.availableData
            guard !data.isEmpty else { return }
            self?.consume(data)
        }

        process.terminationHandler = { [weak self] _ in
            AppLogger.log("CoreBridge: pob-core exited")
            DispatchQueue.main.async { self?.isExecuting = false }
        }

        do {
            try process.run()
            self.process = process
            stdinHandle = stdinPipe.fileHandleForWriting
            AppLogger.log("CoreBridge: pob-core started (\(binary.path))")
        } catch {
            AppLogger.log("CoreBridge: failed to start pob-core: \(error)")
        }
    }

    func stop() {
        process?.terminate()
        process = nil
        stdinHandle = nil
    }

    private func locateCoreBinary(projectRoot: URL) -> URL? {
        let fm = FileManager.default
        // Packaged app: pob-core sits next to the main executable in the bundle.
        if let executable = Bundle.main.executableURL {
            let bundled = executable.deletingLastPathComponent().appendingPathComponent("pob-core")
            if fm.isExecutableFile(atPath: bundled.path) { return bundled }
        }
        // Dev workflow: built by restart.sh into core/bin/.
        let dev = projectRoot.appendingPathComponent("core/bin/pob-core")
        if fm.isExecutableFile(atPath: dev.path) { return dev }
        return nil
    }

    // MARK: - Commands (Swift -> Go notifications)

    func runInstruction(recording: Bool) {
        notify(method: "run.instruction", params: ["recording": recording])
    }

    func runMacro() {
        notify(method: "run.macro", params: [:])
    }

    func stopExecution() {
        resolveMaxStep(false)
        notify(method: "run.stop", params: [:])
    }

    func recordingChanged(_ recording: Bool) {
        notify(method: "recording.changed", params: ["recording": recording])
    }

    /// Answers the pending max-step confirmation from the Go core.
    func resolveMaxStep(_ shouldContinue: Bool) {
        guard let id = maxStepRequestId else { return }
        maxStepRequestId = nil
        DispatchQueue.main.async { self.showMaxStepWarning = false }
        respond(id: id, result: ["continue": shouldContinue])
    }

    // MARK: - Message plumbing

    private func notify(method: String, params: [String: Any]) {
        write(["jsonrpc": "2.0", "method": method, "params": params])
    }

    private func respond(id: Any, result: [String: Any]) {
        write(["jsonrpc": "2.0", "id": id, "result": result])
    }

    private func respondError(id: Any, message: String) {
        write(["jsonrpc": "2.0", "id": id, "error": ["code": -32603, "message": message] as [String: Any]])
    }

    private func write(_ message: [String: Any]) {
        guard let handle = stdinHandle,
              var data = try? JSONSerialization.data(withJSONObject: message) else { return }
        data.append(0x0A)
        writeQueue.async {
            try? handle.write(contentsOf: data)
        }
    }

    private func consume(_ data: Data) {
        buffer.append(data)
        while let newline = buffer.firstIndex(of: 0x0A) {
            let line = buffer.subdata(in: buffer.startIndex ..< newline)
            buffer.removeSubrange(buffer.startIndex ... newline)
            guard !line.isEmpty,
                  let msg = (try? JSONSerialization.jsonObject(with: line)) as? [String: Any] else { continue }
            dispatch(msg)
        }
    }

    private func dispatch(_ msg: [String: Any]) {
        guard let method = msg["method"] as? String else { return }
        let params = msg["params"] as? [String: Any] ?? [:]
        let id = msg["id"]

        switch method {
        case "session.state":
            let executing = params["executing"] as? Bool ?? false
            DispatchQueue.main.async { self.isExecuting = executing }

        case "screenshot.capture":
            guard let id else { return }
            handleScreenshotCapture(id: id, params: params)

        case "cursor.reset":
            guard let id else { return }
            MouseService.shared.resetCursor()
            respondPosition(id: id)

        case "cursor.move":
            guard let id else { return }
            let dx = CGFloat(params["dx"] as? Double ?? 0)
            let dy = CGFloat(params["dy"] as? Double ?? 0)
            MouseService.shared.moveCursorBy(dx: dx, dy: dy)
            respondPosition(id: id)

        case "mouse.click":
            guard let id else { return }
            performAtCursor(id: id) { await MouseService.shared.performClick(at: $0) }

        case "mouse.rightClick":
            guard let id else { return }
            performAtCursor(id: id) { await MouseService.shared.performRightClick(at: $0) }

        case "mouse.doubleClick":
            guard let id else { return }
            performAtCursor(id: id) { await MouseService.shared.performDoubleClick(at: $0) }

        case "mouse.drag":
            guard let id else { return }
            handleDrag(id: id, params: params)

        case "mouse.scroll":
            guard let id else { return }
            handleScroll(id: id, params: params)

        case "keyboard.type":
            guard let id else { return }
            let text = params["text"] as? String ?? ""
            Task {
                await MouseService.shared.performType(text: text)
                self.respond(id: id, result: [:])
            }

        case "keyboard.keyPress":
            guard let id else { return }
            let key = params["key"] as? String ?? ""
            Task {
                await MouseService.shared.performKeyPress(key: key)
                self.respond(id: id, result: [:])
            }

        case "ui.flash":
            DispatchQueue.main.async { self.flashTick += 1 }
            if let id { respond(id: id, result: [:]) }

        case "ui.confirmMaxStep":
            guard let id else { return }
            maxStepRequestId = id
            DispatchQueue.main.async { self.showMaxStepWarning = true }

        default:
            if let id { respondError(id: id, message: "Unknown method: \(method)") }
        }
    }

    // MARK: - Handlers

    private func respondPosition(id: Any) {
        let pos = MouseService.shared.virtualCursorPosition
        respond(id: id, result: ["x": Double(pos.x), "y": Double(pos.y)])
    }

    /// Runs a mouse action at the virtual cursor position, converting to
    /// screen coordinates via the most recent screenshot context.
    private func performAtCursor(id: Any, action: @escaping (CGPoint) async -> Void) {
        let pos = MouseService.shared.virtualCursorPosition
        Task {
            if let ctx = self.lastContext {
                await action(ctx.toCGEventPoint(pixelX: pos.x, pixelY: pos.y))
            }
            self.respond(id: id, result: ["x": Double(pos.x), "y": Double(pos.y)])
        }
    }

    private func handleDrag(id: Any, params: [String: Any]) {
        let dx = CGFloat(params["dx"] as? Double ?? 0)
        let dy = CGFloat(params["dy"] as? Double ?? 0)
        let startPos = MouseService.shared.virtualCursorPosition
        let endPos = CGPoint(x: startPos.x + dx, y: startPos.y + dy)
        Task {
            if let ctx = self.lastContext {
                let from = ctx.toCGEventPoint(pixelX: startPos.x, pixelY: startPos.y)
                let to = ctx.toCGEventPoint(pixelX: endPos.x, pixelY: endPos.y)
                await MouseService.shared.performDrag(from: from, to: to)
            }
            MouseService.shared.moveCursor(to: endPos)
            self.respond(id: id, result: ["x": Double(endPos.x), "y": Double(endPos.y)])
        }
    }

    private func handleScroll(id: Any, params: [String: Any]) {
        let dx = Int32(params["dx"] as? Double ?? 0)
        let dy = Int32(params["dy"] as? Double ?? 0)
        let pos = MouseService.shared.virtualCursorPosition
        Task {
            if let ctx = self.lastContext {
                await MouseService.shared.performScroll(at: ctx.toCGEventPoint(pixelX: pos.x, pixelY: pos.y), dx: dx, dy: dy)
            }
            self.respond(id: id, result: ["x": Double(pos.x), "y": Double(pos.y)])
        }
    }

    private func handleScreenshotCapture(id: Any, params: [String: Any]) {
        let withCursor = params["withCursor"] as? Bool ?? true
        let cropRect: CGRect? = (params["crop"] as? [String: Any]).flatMap { crop in
            guard let x = crop["x"] as? Double,
                  let y = crop["y"] as? Double,
                  let w = crop["width"] as? Double,
                  let h = crop["height"] as? Double else { return nil }
            return CGRect(x: x, y: y, width: w, height: h)
        }

        DispatchQueue.main.async {
            guard let window = NSApplication.shared.windows.first,
                  let (shot, ctx) = ScreenshotService.shared.captureWindowContentAreaWithContext(window: window)
            else {
                self.respondError(id: id, message: "Screenshot capture failed")
                return
            }
            self.lastContext = ctx

            var image = shot
            if withCursor {
                image = ScreenshotService.shared.imageWithCursor(shot, at: MouseService.shared.virtualCursorPosition)
            }
            if let rect = cropRect, let cropped = ScreenshotService.shared.crop(image, to: rect) {
                image = cropped
            }

            guard let tiff = image.tiffRepresentation,
                  let rep = NSBitmapImageRep(data: tiff),
                  let png = rep.representation(using: .png, properties: [:])
            else {
                self.respondError(id: id, message: "Screenshot encoding failed")
                return
            }
            self.respond(id: id, result: ["image": png.base64EncodedString()])
        }
    }
}
