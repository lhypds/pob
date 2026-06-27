import CoreGraphics
import AppKit

class MouseService {
    static let shared = MouseService()

    // Virtual cursor in screenshot pixel coordinates (origin: top-left)
    var virtualCursorPosition: CGPoint = .zero

    private init() {}

    func moveCursor(to point: CGPoint) {
        virtualCursorPosition = point
    }

    func resetCursor() {
        virtualCursorPosition = .zero
    }

    // Performs an actual system left-click at the given CG screen point (origin top-left).
    // Requires Accessibility permission (System Preferences > Privacy & Security > Accessibility).
    func performClick(at cgPoint: CGPoint) async {
        // Post events directly at the target coordinates without moving the real cursor.
        if let down = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDown,
                               mouseCursorPosition: cgPoint, mouseButton: .left) {
            down.post(tap: .cghidEventTap)
        }
        try? await Task.sleep(nanoseconds: 50_000_000)
        if let up = CGEvent(mouseEventSource: nil, mouseType: .leftMouseUp,
                             mouseCursorPosition: cgPoint, mouseButton: .left) {
            up.post(tap: .cghidEventTap)
        }
    }
}
