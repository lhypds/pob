import AppKit

/// Records the user's own mouse/keyboard actions into macro.txt while the
/// record toggle is on and no session is executing. Uses global event
/// monitors, which only receive events delivered to OTHER applications — so
/// interactions with the Pob window itself (toolbar buttons, etc.) are never
/// recorded. Requires Accessibility trust, which the app already needs for
/// posting CGEvents.
///
/// Output uses the same action grammar the Go core replays:
/// move / click / rightClick / doubleClick / drag / scroll / typeText / keyPress.
/// Mouse movement is not recorded continuously — a single move(dx, dy) with
/// the net displacement of the virtual cursor is emitted right before each
/// click/drag/scroll, matching how replay chains relative moves.
final class UserMacroRecorder {
    /// This instance's settings — macro.txt itself is shared at the root, but
    /// access goes through the owning instance.
    private let settings: SettingsService
    /// The overlay window whose content area defines the pixel coordinate
    /// space; set by PobInstance.attach.
    weak var window: NSWindow?

    private(set) var isActive = false

    private var monitors: [Any] = []

    /// Virtual cursor in screenshot pixel coordinates — mirrors the position
    /// the replay cursor will have after the lines recorded so far.
    private var virtualPos = CGPoint(x: 20, y: 20)

    /// Left-button down position (screenshot pixels), pending until mouse-up
    /// decides between click and drag.
    private var leftDownPixel: CGPoint?

    /// Scroll burst accumulator — one scroll(dx, dy) line per burst.
    private var scrollAccumX: CGFloat = 0
    private var scrollAccumY: CGFloat = 0
    private var scrollPixel: CGPoint?
    private var scrollFlushTimer: Timer?

    /// Pending typed text, coalesced into one typeText(...) line.
    private var textBuffer = ""
    private var textFlushTimer: Timer?

    /// Last line written, used to merge click() + click() into doubleClick().
    private var lastLine: String?

    init(settings: SettingsService) {
        self.settings = settings
    }

    // MARK: - Lifecycle

    /// Starts recording. `position` is where the replay virtual cursor will be
    /// when the lines recorded next are reached: (20, 20) for a fresh macro
    /// (replay starts with a cursor reset), or the AI's final cursor position
    /// when resuming after an execution that recorded its own actions.
    func start(from position: CGPoint = CGPoint(x: 20, y: 20)) {
        guard !isActive else { return }
        isActive = true
        virtualPos = position
        leftDownPixel = nil
        scrollPixel = nil
        scrollAccumX = 0
        scrollAccumY = 0
        textBuffer = ""
        lastLine = nil

        let mouseEvents: NSEvent.EventTypeMask = [.leftMouseDown, .leftMouseUp, .rightMouseUp, .scrollWheel]
        if let m = NSEvent.addGlobalMonitorForEvents(matching: mouseEvents, handler: { [weak self] event in
            self?.handleMouse(event)
        }) {
            monitors.append(m)
        }
        if let m = NSEvent.addGlobalMonitorForEvents(matching: .keyDown, handler: { [weak self] event in
            self?.handleKeyDown(event)
        }) {
            monitors.append(m)
        }
        AppLogger.log("User recording started")
    }

    func stop() {
        guard isActive else { return }
        flushScroll()
        flushText()
        for m in monitors { NSEvent.removeMonitor(m) }
        monitors.removeAll()
        isActive = false
        AppLogger.log("User recording stopped")
    }

    /// Flushes pending scroll/text buffers so lines appended by other writers
    /// (e.g. the core's take_screenshot() on the toolbar screenshot button)
    /// land after the actions the user already performed.
    func flushPending() {
        guard isActive else { return }
        flushScroll()
        flushText()
    }

    /// Stops recording because a session is starting, first rewinding the
    /// virtual cursor to (20, 20) — the position after the cursor reset every
    /// session begins with — so action deltas the AI records next chain
    /// correctly after the user-recorded lines.
    func pauseForExecution() {
        guard isActive else { return }
        flushScroll()
        flushText()
        emitMove(to: CGPoint(x: 20, y: 20))
        stop()
    }

    // MARK: - Mouse

    private func handleMouse(_ event: NSEvent) {
        guard let pixel = currentPixel() else { return }
        switch event.type {
        case .leftMouseDown:
            flushScroll()
            flushText()
            leftDownPixel = pixel

        case .leftMouseUp:
            guard let down = leftDownPixel else { return }
            leftDownPixel = nil
            let dx = pixel.x - down.x
            let dy = pixel.y - down.y
            if dx * dx + dy * dy > 100 { // >10 screenshot px: a drag, not a click
                emitMove(to: down)
                appendLine("drag(\(Int(dx.rounded())), \(Int(dy.rounded())))")
                virtualPos.x += dx.rounded()
                virtualPos.y += dy.rounded()
            } else if event.clickCount == 2 {
                // Second click of a double-click: upgrade the click just recorded.
                if lastLine == "click()", settings.removeLastMacroLine(ifMatches: "click()") {
                    appendLine("doubleClick()")
                } else {
                    emitMove(to: down)
                    appendLine("doubleClick()")
                }
            } else if event.clickCount <= 1 {
                emitMove(to: down)
                appendLine("click()")
            }
            // clickCount >= 3 is already covered by the doubleClick upgrade.

        case .rightMouseUp:
            flushScroll()
            flushText()
            emitMove(to: pixel)
            appendLine("rightClick()")

        case .scrollWheel:
            flushText()
            if scrollPixel == nil { scrollPixel = pixel }
            let unit: CGFloat = event.hasPreciseScrollingDeltas ? 1 : 40
            scrollAccumX += event.scrollingDeltaX * unit
            scrollAccumY += event.scrollingDeltaY * unit
            scrollFlushTimer?.invalidate()
            scrollFlushTimer = Timer.scheduledTimer(withTimeInterval: 0.5, repeats: false) { [weak self] _ in
                self?.flushScroll()
            }

        default:
            break
        }
    }

    private func flushScroll() {
        scrollFlushTimer?.invalidate()
        scrollFlushTimer = nil
        guard let pixel = scrollPixel else { return }
        scrollPixel = nil
        // Replay posts wheel1 = -dy / wheel2 = dx, so invert Y to round-trip.
        let dx = Int(scrollAccumX.rounded())
        let dy = Int((-scrollAccumY).rounded())
        scrollAccumX = 0
        scrollAccumY = 0
        guard dx != 0 || dy != 0 else { return }
        emitMove(to: pixel)
        appendLine("scroll(\(dx), \(dy))")
    }

    // MARK: - Keyboard

    /// Key codes the replayer's keyPress(...) understands, by name.
    private static let specialKeys: [UInt16: String] = [
        0x24: "enter", 0x4C: "enter",
        0x30: "tab",
        0x35: "escape",
        0x7B: "left", 0x7C: "right", 0x7D: "down", 0x7E: "up",
        0x73: "home", 0x77: "end", 0x74: "pageup", 0x79: "pagedown",
        0x7A: "f1", 0x78: "f2", 0x63: "f3", 0x76: "f4",
        0x60: "f5", 0x61: "f6", 0x62: "f7", 0x64: "f8",
        0x65: "f9", 0x6D: "f10", 0x67: "f11", 0x6F: "f12",
    ]

    /// Letters the replayer supports as cmd+<letter>.
    private static let cmdLetters: Set<String> = ["a", "s", "z", "x", "c", "v", "w", "r", "t"]

    private func handleKeyDown(_ event: NSEvent) {
        let flags = event.modifierFlags.intersection(.deviceIndependentFlagsMask)

        if flags.contains(.command) {
            flushText()
            guard flags.subtracting([.command, .numericPad, .function]).isEmpty,
                  let letter = event.charactersIgnoringModifiers?.lowercased(),
                  Self.cmdLetters.contains(letter)
            else {
                AppLogger.log("Recording: skipped unsupported shortcut")
                return
            }
            appendLine("keyPress(\"cmd+\(letter)\")")
            return
        }

        // Backspace: prefer editing the pending text over a keyPress line.
        if event.keyCode == 0x33 {
            if !textBuffer.isEmpty {
                textBuffer.removeLast()
                scheduleTextFlush()
            } else {
                appendLine("keyPress(\"delete\")")
            }
            return
        }

        if let name = Self.specialKeys[event.keyCode] {
            flushText()
            appendLine("keyPress(\"\(name)\")")
            return
        }

        guard !flags.contains(.control) else { return }

        guard let chars = event.characters, !chars.isEmpty,
              chars.unicodeScalars.allSatisfy({ $0.value >= 0x20 && !(0xF700 ... 0xF8FF).contains($0.value) })
        else { return }
        textBuffer += chars
        scheduleTextFlush()
    }

    private func scheduleTextFlush() {
        textFlushTimer?.invalidate()
        textFlushTimer = Timer.scheduledTimer(withTimeInterval: 1.5, repeats: false) { [weak self] _ in
            self?.flushText()
        }
    }

    private func flushText() {
        textFlushTimer?.invalidate()
        textFlushTimer = nil
        guard !textBuffer.isEmpty else { return }
        let escaped = textBuffer
            .replacingOccurrences(of: "\\", with: "\\\\")
            .replacingOccurrences(of: "\"", with: "\\\"")
        textBuffer = ""
        appendLine("typeText(\"\(escaped)\")")
    }

    // MARK: - Helpers

    /// Current mouse position converted to screenshot pixel coordinates
    /// (top-left of the window content area, same space the replay uses).
    private func currentPixel() -> CGPoint? {
        guard let window,
              let screen = window.screen ?? NSScreen.main else { return nil }
        let contentRect = window.convertToScreen(window.contentLayoutRect)
        let scale = screen.backingScaleFactor
        let mouse = NSEvent.mouseLocation
        return CGPoint(x: (mouse.x - contentRect.origin.x) * scale,
                       y: (contentRect.maxY - mouse.y) * scale)
    }

    /// Emits move(dx, dy) bringing the virtual cursor to `pixel`. Tracks the
    /// rounded deltas the replay will apply, so consecutive moves don't drift.
    private func emitMove(to pixel: CGPoint) {
        let dx = (pixel.x - virtualPos.x).rounded()
        let dy = (pixel.y - virtualPos.y).rounded()
        if dx != 0 || dy != 0 {
            appendLine("move(\(Int(dx)), \(Int(dy)))")
        }
        virtualPos.x += dx
        virtualPos.y += dy
    }

    private func appendLine(_ line: String) {
        settings.appendToMacro(line)
        lastLine = line
        AppLogger.log("Recorded: \(line)")
    }
}
