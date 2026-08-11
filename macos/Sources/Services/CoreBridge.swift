import AppKit
import Combine

/// Spawns and talks to the Go core (pob-core) over stdin/stdout using
/// line-delimited JSON-RPC. The Go side owns the agent loop, LLM calls, logs
/// and the MCP server; this bridge answers its perception/operation requests
/// (screenshot, mouse, keyboard, UI dialogs) and forwards user commands
/// (run / stop / recording) the other way.
final class CoreBridge: ObservableObject {
    /// True while the Go core is executing a session; drives the cursor
    /// overlay, window lock and click-through logic in the UI.
    @Published var isExecuting = false
    /// True while something outside a replay is driving this instance — an MCP
    /// client connected to the MCP server, a browser on the Pob server — i.e.
    /// the cursor may move at any moment. Drives the cursor overlay only:
    /// unlike isExecuting it does not lock the window or pause the recorder.
    @Published var isMCPDriving = false
    /// Set when the Go core has something to say that the log alone would not
    /// get across — the settings a macro's IF needs and hasn't got, say. The
    /// core owns both strings; the alert only shows them and an OK.
    @Published var showCoreAlert = false
    @Published var coreAlertTitle = ""
    @Published var coreAlertMessage = ""
    /// Where this instance answers on the network —
    /// http://<machine>:<port>/<instance> — or nil while the Pob server is off
    /// ("server": false in settings.json). Pushed by the Go core once at
    /// startup; the instance badge copies it.
    @Published var serverURL: String?
    /// Incremented whenever the Go core requests the screenshot flash effect.
    @Published var flashTick = 0

    /// This instance's settings (project root + instance id for the spawn).
    private let settings: SettingsService
    /// This instance's virtual cursor, moved and queried by the Go core.
    private let mouse: MouseService
    /// The overlay window this instance captures; set by PobInstance.attach.
    weak var window: NSWindow?

    private var process: Process?
    private var stdinHandle: FileHandle?
    private var buffer = Data()
    private let writeQueue = DispatchQueue(label: "corebridge.write")
    
    /// Where captured frames go, so a picture never sits in front of a
    /// keystroke on the JSON-RPC line. Offered by the core at startup; until
    /// it is up, captures answer the old way.
    private let frames = FrameChannel()
    /// Encoding is by the pixel and has no business on the main thread, which
    /// is also the thread every mouse event is posted from. Serial, because
    /// two full-screen encodes at once would cost more in memory than they
    /// win in time.
    private let encodeQueue = DispatchQueue(label: "corebridge.encode", qos: .userInitiated)

    /// Coordinate context of the most recent capture; used to convert
    /// screenshot pixels to CGEvent screen positions for mouse actions.
    private var lastContext: ScreenshotContext? {
        didSet {
            // The area a screenshot covers is also the box the virtual cursor
            // lives in, so the two are always learned together.
            mouse.contentPixelSize = lastContext?.pixelSize ?? .zero
        }
    }
    init(settings: SettingsService, mouse: MouseService) {
        self.settings = settings
        self.mouse = mouse
    }

    // MARK: - Process lifecycle

    func start() {
        let root = settings.projectRoot

        guard let binary = locateCoreBinary() else {
            AppLogger.log("CoreBridge: pob-core binary not found — run ./setup.sh")
            return
        }

        let process = Process()
        process.executableURL = binary
        process.arguments = ["--root", root.path, "--instance", settings.instanceID]
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

    private func locateCoreBinary() -> URL? {
        let fm = FileManager.default
        // Packaged app: pob-core sits next to the main executable in the bundle.
        if let executable = Bundle.main.executableURL {
            let bundled = executable.deletingLastPathComponent().appendingPathComponent("pob-core")
            if fm.isExecutableFile(atPath: bundled.path) { return bundled }
        }
        // Dev workflow (swift build): walk up from the executable towards the
        // repository root and use core/bin/pob-core built by the dev scripts.
        var dir = URL(fileURLWithPath: CommandLine.arguments[0])
            .resolvingSymlinksInPath()
            .deletingLastPathComponent()
        for _ in 0 ..< 6 {
            let candidate = dir.appendingPathComponent("core/bin/pob-core")
            if fm.isExecutableFile(atPath: candidate.path) { return candidate }
            dir = dir.deletingLastPathComponent()
        }
        return nil
    }

    // MARK: - Commands (Swift -> Go notifications)

    func runMacro() {
        notify(method: "run.macro", params: [:])
    }

    func stopExecution() {
        notify(method: "run.stop", params: [:])
    }

    func recordingChanged(_ recording: Bool) {
        notify(method: "recording.changed", params: ["recording": recording])
    }

    /// Toolbar screenshot button: the Go core flashes, captures and saves the
    /// shot, and records a takeScreenshot() macro line while recording.
    func takeScreenshot() {
        notify(method: "screenshot.take", params: [:])
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

        case "mcp.state":
            let active = params["active"] as? Bool ?? false
            DispatchQueue.main.async {
                // Park the cursor at its home position so it is visible the
                // moment a client takes hold, rather than sitting on the
                // top-left corner where it reads as "no cursor at all".
                if active { self.mouse.resetCursor() }
                self.isMCPDriving = active
            }

        case "server.state":
            let running = params["running"] as? Bool ?? false
            let url = params["url"] as? String ?? ""
            DispatchQueue.main.async {
                self.serverURL = running && !url.isEmpty ? url : nil
            }

        case "frames.channel":
            let port = params["port"] as? Int ?? 0
            let token = params["token"] as? String ?? ""
            if port > 0, port <= Int(UInt16.max), !token.isEmpty {
                frames.connect(port: UInt16(port), token: token)
            }

        case "screenshot.capture":
            guard let id else { return }
            handleScreenshotCapture(id: id, params: params)

        case "cursor.reset":
            guard let id else { return }
            mouse.resetCursor()
            respondPosition(id: id)

        case "cursor.move":
            guard let id else { return }
            let dx = CGFloat(params["dx"] as? Double ?? 0)
            let dy = CGFloat(params["dy"] as? Double ?? 0)
            self.mouse.moveCursorBy(dx: dx, dy: dy)
            respondPosition(id: id)

        case "mouse.click":
            guard let id else { return }
            performAtCursor(id: id) { await self.mouse.performClick(at: $0) }

        case "mouse.rightClick":
            guard let id else { return }
            performAtCursor(id: id) { await self.mouse.performRightClick(at: $0) }

        case "mouse.doubleClick":
            guard let id else { return }
            performAtCursor(id: id) { await self.mouse.performDoubleClick(at: $0) }

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
                await self.mouse.performType(text: text)
                self.respond(id: id, result: [:])
            }

        case "keyboard.keyPress":
            guard let id else { return }
            let key = params["key"] as? String ?? ""
            Task {
                await self.mouse.performKeyPress(key: key)
                self.respond(id: id, result: [:])
            }

        case "ui.flash":
            DispatchQueue.main.async { self.flashTick += 1 }
            if let id { respond(id: id, result: [:]) }

        case "ui.alert":
            let title = params["title"] as? String ?? "Pob"
            let message = params["message"] as? String ?? ""
            DispatchQueue.main.async {
                self.coreAlertTitle = title
                self.coreAlertMessage = message
                self.showCoreAlert = true
            }
            if let id { respond(id: id, result: [:]) }

        default:
            if let id { respondError(id: id, message: "Unknown method: \(method)") }
        }
    }

    // MARK: - Handlers

    private func respondPosition(id: Any) {
        let pos = self.mouse.virtualCursorPosition
        respond(id: id, result: ["x": Double(pos.x), "y": Double(pos.y)])
    }

    /// The mapping from screenshot pixels to screen coordinates. Falls back to
    /// the window's current geometry when nothing has been captured yet:
    /// without this, every click, drag and scroll issued before the first
    /// screenshot was dropped on the floor while still reporting success.
    private func currentContext() async -> ScreenshotContext? {
        if let ctx = lastContext { return ctx }
        return await MainActor.run { () -> ScreenshotContext? in
            guard let window = self.window,
                  let ctx = ScreenshotService.shared.context(for: window) else {
                AppLogger.log("CoreBridge: no window for coordinate mapping")
                return nil
            }
            self.lastContext = ctx
            return ctx
        }
    }

    /// The window moved or was resized, so both the mapping from screenshot
    /// pixels to the screen and the box the cursor is held inside have
    /// changed. Recomputed rather than just invalidated, because this runs on
    /// the main thread — the only place the window's geometry can be read.
    func windowGeometryChanged() {
        guard let window, let ctx = ScreenshotService.shared.context(for: window) else { return }
        lastContext = ctx
    }

    /// Runs a mouse action at the virtual cursor position, converting to
    /// screen coordinates via the most recent screenshot context.
    private func performAtCursor(id: Any, action: @escaping (CGPoint) async -> Void) {
        let pos = self.mouse.virtualCursorPosition
        Task {
            guard let ctx = await self.currentContext() else {
                self.respondError(id: id, message: "No window available to map screenshot coordinates")
                return
            }
            await action(ctx.toCGEventPoint(pixelX: pos.x, pixelY: pos.y))
            self.respond(id: id, result: ["x": Double(pos.x), "y": Double(pos.y)])
        }
    }

    private func handleDrag(id: Any, params: [String: Any]) {
        let dx = CGFloat(params["dx"] as? Double ?? 0)
        let dy = CGFloat(params["dy"] as? Double ?? 0)
        let startPos = self.mouse.virtualCursorPosition
        let endPos = CGPoint(x: startPos.x + dx, y: startPos.y + dy)
        Task {
            guard let ctx = await self.currentContext() else {
                self.respondError(id: id, message: "No window available to map screenshot coordinates")
                return
            }
            let from = ctx.toCGEventPoint(pixelX: startPos.x, pixelY: startPos.y)
            let to = ctx.toCGEventPoint(pixelX: endPos.x, pixelY: endPos.y)
            await self.mouse.performDrag(from: from, to: to) { t in
                self.mouse.moveCursor(to: CGPoint(
                    x: startPos.x + (endPos.x - startPos.x) * t,
                    y: startPos.y + (endPos.y - startPos.y) * t
                ))
            }
            self.mouse.moveCursor(to: endPos)
            self.respond(id: id, result: ["x": Double(endPos.x), "y": Double(endPos.y)])
        }
    }

    private func handleScroll(id: Any, params: [String: Any]) {
        let dx = Int32(params["dx"] as? Double ?? 0)
        let dy = Int32(params["dy"] as? Double ?? 0)
        let pos = self.mouse.virtualCursorPosition
        Task {
            guard let ctx = await self.currentContext() else {
                self.respondError(id: id, message: "No window available to map screenshot coordinates")
                return
            }
            await self.mouse.performScroll(at: ctx.toCGEventPoint(pixelX: pos.x, pixelY: pos.y), dx: dx, dy: dy)
            self.respond(id: id, result: ["x": Double(pos.x), "y": Double(pos.y)])
        }
    }

    private func handleScreenshotCapture(id: Any, params: [String: Any]) {
        let withCursor = params["withCursor"] as? Bool ?? true
        let format = params["format"] as? String ?? "png"
        let maxWidth = params["maxWidth"] as? Int ?? 0
        let quality = params["quality"] as? Int ?? 0
        let cropRect: CGRect? = (params["crop"] as? [String: Any]).flatMap { crop in
            guard let x = crop["x"] as? Double,
                  let y = crop["y"] as? Double,
                  let w = crop["width"] as? Double,
                  let h = crop["height"] as? Double else { return nil }
            return CGRect(x: x, y: y, width: w, height: h)
        }

        // The capture itself has to happen on the main thread — it reads the
        // window's geometry, which only the main thread may — but that is all
        // that does. Everything after it is pixels, and pixels are what the
        // encode queue is for: at a watchable frame rate, encoding here would
        // mean the UI and every mouse event queueing behind a picture.
        DispatchQueue.main.async {
            guard let window = self.window,
                  let (image, ctx) = ScreenshotService.shared.captureWindowContentCGImage(window: window)
            else {
                self.respondError(id: id, message: "Screenshot capture failed")
                return
            }
            self.lastContext = ctx
            let cursor = withCursor ? self.mouse.virtualCursorPosition : nil

            self.encodeQueue.async {
                guard let shot = ScreenshotService.shared.encode(
                    image,
                    cursorAt: cursor,
                    crop: cropRect,
                    maxWidth: maxWidth,
                    format: format,
                    quality: quality
                ) else {
                    self.respondError(id: id, message: "Screenshot encoding failed")
                    return
                }
                self.deliver(id: id, shot: shot)
            }
        }
    }

    /// Sends a captured frame down the frame channel, or — if it is not up —
    /// the JSON-RPC line as base64, which is what every shell did before the
    /// channel existed and what an older core still expects.
    private func deliver(id: Any, shot: ScreenshotService.EncodedShot) {
        let meta: [String: Any] = [
            "width": shot.width,
            "height": shot.height,
            "sourceWidth": shot.sourceWidth,
            "sourceHeight": shot.sourceHeight,
        ]
        // Ids are minted by the core as strings ("go-42"); anything else did
        // not come from a channel-aware core.
        if let requestID = id as? String, frames.send(id: requestID, meta: meta, payload: shot.data) {
            return
        }
        var result = meta
        result["image"] = shot.data.base64EncodedString()
        respond(id: id, result: result)
    }
}
