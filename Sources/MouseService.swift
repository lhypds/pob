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

    func moveCursorBy(dx: CGFloat, dy: CGFloat) {
        virtualCursorPosition.x += dx
        virtualCursorPosition.y += dy
    }

    func resetCursor() {
        virtualCursorPosition = .zero
    }

    // Performs an actual system left-click at the given CG screen point (origin top-left).
    // Requires Accessibility permission (System Preferences > Privacy & Security > Accessibility).
    func performClick(at cgPoint: CGPoint) async {
        // Temporarily pass clicks through the overlay so the event reaches the app below.
        await MainActor.run {
            NSApplication.shared.windows.first?.ignoresMouseEvents = true
        }

        if let down = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDown,
                               mouseCursorPosition: cgPoint, mouseButton: .left) {
            down.post(tap: .cghidEventTap)
        }
        try? await Task.sleep(nanoseconds: 50_000_000)
        if let up = CGEvent(mouseEventSource: nil, mouseType: .leftMouseUp,
                             mouseCursorPosition: cgPoint, mouseButton: .left) {
            up.post(tap: .cghidEventTap)
        }

        await MainActor.run {
            NSApplication.shared.windows.first?.ignoresMouseEvents = false
        }
    }
}
