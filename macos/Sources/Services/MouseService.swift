import AppKit
import ApplicationServices
import CoreGraphics

class MouseService: ObservableObject {
    static let shared = MouseService()

    /// Virtual cursor in screenshot pixel coordinates (origin: top-left).
    /// Never touches the real system mouse pointer.
    var virtualCursorPosition: CGPoint = .zero

    /// Published so the UI can overlay the cursor and animate its movement.
    @Published var displayPosition: CGPoint = .zero

    private init() {}

    func moveCursor(to point: CGPoint) {
        virtualCursorPosition = point
        let p = point
        DispatchQueue.main.async { self.displayPosition = p }
    }

    func moveCursorBy(dx: CGFloat, dy: CGFloat) {
        virtualCursorPosition.x += dx
        virtualCursorPosition.y += dy
        let p = virtualCursorPosition
        DispatchQueue.main.async { self.displayPosition = p }
    }

    func resetCursor() {
        virtualCursorPosition = CGPoint(x: 20, y: 20)
        DispatchQueue.main.async { self.displayPosition = CGPoint(x: 20, y: 20) }
    }

    // MARK: - Mouse actions

    func performClick(at cgPoint: CGPoint) async {
        await passThrough {
            if let down = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDown,
                                  mouseCursorPosition: cgPoint, mouseButton: .left)
            {
                down.post(tap: .cghidEventTap)
            }
            try? await Task.sleep(nanoseconds: 50_000_000)
            if let up = CGEvent(mouseEventSource: nil, mouseType: .leftMouseUp,
                                mouseCursorPosition: cgPoint, mouseButton: .left)
            {
                up.post(tap: .cghidEventTap)
            }
        }
    }

    func performRightClick(at cgPoint: CGPoint) async {
        await passThrough {
            if let down = CGEvent(mouseEventSource: nil, mouseType: .rightMouseDown,
                                  mouseCursorPosition: cgPoint, mouseButton: .right)
            {
                down.post(tap: .cghidEventTap)
            }
            try? await Task.sleep(nanoseconds: 50_000_000)
            if let up = CGEvent(mouseEventSource: nil, mouseType: .rightMouseUp,
                                mouseCursorPosition: cgPoint, mouseButton: .right)
            {
                up.post(tap: .cghidEventTap)
            }
        }
    }

    func performDoubleClick(at cgPoint: CGPoint) async {
        await passThrough {
            for clickCount in [1, 2] {
                if let down = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDown,
                                      mouseCursorPosition: cgPoint, mouseButton: .left)
                {
                    down.setIntegerValueField(.mouseEventClickState, value: Int64(clickCount))
                    down.post(tap: .cghidEventTap)
                }
                try? await Task.sleep(nanoseconds: 30_000_000)
                if let up = CGEvent(mouseEventSource: nil, mouseType: .leftMouseUp,
                                    mouseCursorPosition: cgPoint, mouseButton: .left)
                {
                    up.setIntegerValueField(.mouseEventClickState, value: Int64(clickCount))
                    up.post(tap: .cghidEventTap)
                }
                if clickCount == 1 { try? await Task.sleep(nanoseconds: 50_000_000) }
            }
        }
    }

    func performDrag(from: CGPoint, to: CGPoint) async {
        await passThrough {
            if let down = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDown,
                                  mouseCursorPosition: from, mouseButton: .left)
            {
                down.post(tap: .cghidEventTap)
            }
            try? await Task.sleep(nanoseconds: 50_000_000)
            let steps = 20
            for i in 1 ... steps {
                let t = CGFloat(i) / CGFloat(steps)
                let pt = CGPoint(x: from.x + (to.x - from.x) * t, y: from.y + (to.y - from.y) * t)
                if let drag = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDragged,
                                      mouseCursorPosition: pt, mouseButton: .left)
                {
                    drag.post(tap: .cghidEventTap)
                }
                try? await Task.sleep(nanoseconds: 16_000_000)
            }
            if let up = CGEvent(mouseEventSource: nil, mouseType: .leftMouseUp,
                                mouseCursorPosition: to, mouseButton: .left)
            {
                up.post(tap: .cghidEventTap)
            }
        }
    }

    func performScroll(at cgPoint: CGPoint, dx: Int32, dy: Int32) async {
        await passThrough {
            // wheel1 = vertical (negative = scroll down), wheel2 = horizontal
            if let scroll = CGEvent(scrollWheelEvent2Source: nil, units: .pixel,
                                    wheelCount: 2, wheel1: -dy, wheel2: dx, wheel3: 0)
            {
                scroll.location = cgPoint
                scroll.post(tap: .cghidEventTap)
            }
        }
    }

    // MARK: - Keyboard actions

    func performType(text: String) async {
        // AX direct insertion: works for any script (CJK etc.) without touching the clipboard.
        let sysWide = AXUIElementCreateSystemWide()
        var focusedRef: CFTypeRef?
        if AXUIElementCopyAttributeValue(sysWide, kAXFocusedUIElementAttribute as CFString, &focusedRef) == .success,
           let focusedRef
        {
            let element = focusedRef as! AXUIElement
            if AXUIElementSetAttributeValue(element, kAXSelectedTextAttribute as CFString, text as CFString) == .success {
                return
            }
        }
        AppLogger.log("typeText: AX insertion failed for focused element")
    }

    func performKeyPress(key: String) async {
        let source = CGEventSource(stateID: .hidSystemState)
        let lower = key.lowercased()
        guard let (keyCode, flags) = Self.resolveKey(lower) else {
            AppLogger.log("Unknown key: \(key)")
            return
        }
        if let down = CGEvent(keyboardEventSource: source, virtualKey: keyCode, keyDown: true) {
            down.flags = flags
            down.post(tap: .cghidEventTap)
        }
        try? await Task.sleep(nanoseconds: 30_000_000)
        if let up = CGEvent(keyboardEventSource: source, virtualKey: keyCode, keyDown: false) {
            up.flags = flags
            up.post(tap: .cghidEventTap)
        }
    }

    // MARK: - Helpers

    /// Runs `body` with the overlay window set to click-through and the real mouse cursor frozen
    /// in place, so automation events reach the app below without moving the user's pointer.
    private func passThrough(_ body: () async -> Void) async {
        await MainActor.run {
            NSApplication.shared.windows.first?.ignoresMouseEvents = true
        }
        // Let the window server process the hit-test change before posting events.
        try? await Task.sleep(nanoseconds: 100_000_000)

        // Freeze the visible cursor so it never moves during the action.
        let savedPos = Self.cgCursorPosition()
        CGAssociateMouseAndMouseCursorPosition(0)

        await body()

        // Restore cursor position and re-couple movement.
        CGWarpMouseCursorPosition(savedPos)
        CGAssociateMouseAndMouseCursorPosition(1)

        await MainActor.run {
            NSApplication.shared.windows.first?.ignoresMouseEvents = false
        }
    }

    /// Current system cursor position in CG coordinates (origin: top-left of primary display).
    private static func cgCursorPosition() -> CGPoint {
        guard let primary = NSScreen.screens.first else { return .zero }
        let ns = NSEvent.mouseLocation // NSScreen: y from bottom of primary
        return CGPoint(x: ns.x, y: primary.frame.height - ns.y)
    }

    private static func resolveKey(_ key: String) -> (CGKeyCode, CGEventFlags)? {
        let plain: [String: CGKeyCode] = [
            "return": 0x24, "enter": 0x24,
            "tab": 0x30, "space": 0x31,
            "delete": 0x33, "backspace": 0x33,
            "escape": 0x35, "esc": 0x35,
            "left": 0x7B, "right": 0x7C, "down": 0x7D, "up": 0x7E,
            "home": 0x73, "end": 0x77, "pageup": 0x74, "pagedown": 0x79,
            "f1": 0x7A, "f2": 0x78, "f3": 0x63, "f4": 0x76,
            "f5": 0x60, "f6": 0x61, "f7": 0x62, "f8": 0x64,
            "f9": 0x65, "f10": 0x6D, "f11": 0x67, "f12": 0x6F,
        ]
        let cmdKeys: [String: CGKeyCode] = [
            "a": 0x00, "s": 0x01, "z": 0x06, "x": 0x07,
            "c": 0x08, "v": 0x09, "w": 0x0D, "r": 0x0F, "t": 0x11,
        ]
        if let code = plain[key] { return (code, []) }
        if key.hasPrefix("cmd+"), let letter = key.split(separator: "+").last.map(String.init),
           let code = cmdKeys[letter]
        {
            return (code, .maskCommand)
        }
        return nil
    }
}
