import AppKit
import ApplicationServices
// Carbon is what still answers "which key produces this character on the
// layout in use" — UCKeyTranslate and the Text Input Sources around it.
import Carbon.HIToolbox
import CoreGraphics

/// One per instance/window: holds that window's virtual cursor and posts the
/// (system-wide) mouse and keyboard events for its sessions.
class MouseService: ObservableObject {
    /// Virtual cursor in screenshot pixel coordinates (origin: top-left).
    /// Never touches the real system mouse pointer. Starts at the home corner
    /// a reset puts it back at, which is where the overlay draws it from launch
    /// — the top-left corner would read as no cursor at all.
    var virtualCursorPosition = CGPoint(x: 20, y: 20)

    /// Published so the UI can overlay the cursor and animate its movement.
    @Published var displayPosition = CGPoint(x: 20, y: 20)

    /// The overlay window this cursor belongs to; made click-through while
    /// automation events are posted. Set by PobInstance.attach.
    weak var window: NSWindow?

    /// Suspends the click-through policy for the length of an automation burst,
    /// and hands it back afterwards. Set by PobInstance.attach; called on the
    /// main thread with true before any event is posted and false once they all
    /// are.
    ///
    /// It is not enough to set `window.ignoresMouseEvents` here and post. That
    /// property has another owner — PobInstance decides it from where the *real*
    /// pointer is, on a 50 ms poll and on every mouse-moved event the app sees —
    /// and the real pointer has nothing to do with where an automated click is
    /// aimed. A pointer left resting on this window's toolbar or resize edge
    /// makes that policy hand mouse events back to Pob between this burst's
    /// first event and its button-press, and the press then lands on Pob's own
    /// window: the window comes forward and the app below never hears the click,
    /// which is a click that plainly did not happen from anywhere except the
    /// event log. Where the pointer happened to be resting is also why it came
    /// and went.
    var holdClickThrough: ((Bool) -> Void)?

    /// The content area in screenshot pixels — the box the cursor lives in.
    /// Kept up to date by CoreBridge from the same context that maps pixels to
    /// the screen. Zero until the window geometry is known, which is what makes
    /// the clamp below do nothing until there is something to clamp to.
    var contentPixelSize: CGSize = .zero

    /// Holds a position inside the Pob window. Everything the cursor addresses
    /// is inside that window — it is what the screenshots show and what the
    /// clicks are aimed through — so a position outside it can only act on
    /// something nobody asked for and that no screenshot would ever reveal.
    /// Relative moves are what make this necessary: a trackpad drag, or a run
    /// of nudges from a model, adds up in one direction and walks off the edge.
    private func clamped(_ point: CGPoint) -> CGPoint {
        guard contentPixelSize.width > 0, contentPixelSize.height > 0 else { return point }
        // The last pixel is width - 1: a cursor at width would be the first
        // pixel outside the window.
        return CGPoint(x: min(max(point.x, 0), contentPixelSize.width - 1),
                       y: min(max(point.y, 0), contentPixelSize.height - 1))
    }

    func moveCursor(to point: CGPoint) {
        let p = clamped(point)
        virtualCursorPosition = p
        DispatchQueue.main.async { self.displayPosition = p }
    }

    func moveCursorBy(dx: CGFloat, dy: CGFloat) {
        moveCursor(to: CGPoint(x: virtualCursorPosition.x + dx, y: virtualCursorPosition.y + dy))
    }

    func resetCursor() {
        virtualCursorPosition = CGPoint(x: 20, y: 20)
        DispatchQueue.main.async { self.displayPosition = CGPoint(x: 20, y: 20) }
    }

    // MARK: - Mouse actions

    func performClick(at cgPoint: CGPoint) async {
        await passThrough(arrivingAt: cgPoint) {
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
        await passThrough(arrivingAt: cgPoint) {
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
        await passThrough(arrivingAt: cgPoint) {
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

    /// `onProgress` is called with the interpolation fraction after each drag
    /// step, so the overlay cursor can track the real pointer instead of
    /// sitting at the start position until the drag completes.
    func performDrag(from: CGPoint, to: CGPoint, onProgress: ((CGFloat) -> Void)? = nil) async {
        await passThrough(arrivingAt: from) {
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
                onProgress?(t)
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
        await passThrough(arrivingAt: cgPoint) {
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
        if text.isEmpty { return }
        // AX direct insertion first: it puts the text in whole, in any script,
        // without touching the clipboard or depending on the keyboard layout.
        if await insertViaAX(text) { return }
        // Most apps refuse it — browsers, Electron windows and terminals all
        // do, because the field isn't an AX text element that can be written
        // to. So type the characters instead, which is what the Windows and
        // Linux shells do in the first place.
        await typeAsKeystrokes(text)
    }

    /// Puts `text` into the focused element's selection, and answers whether it
    /// actually arrived there.
    ///
    /// A refusal does not come back as one. `AXUIElementSetAttributeValue`
    /// returns `.success` from any app whose accessibility bridge accepts the
    /// message, which the browsers and Electron windows above all do while
    /// putting no character on screen — so success is claimed for exactly the
    /// apps the keystroke path exists to serve, and taking it at face value is
    /// a typeText that reliably types nothing. The field's value is read either
    /// side of the write instead, and only a value that moved counts.
    private func insertViaAX(_ text: String) async -> Bool {
        let sysWide = AXUIElementCreateSystemWide()
        var focusedRef: CFTypeRef?
        guard AXUIElementCopyAttributeValue(sysWide, kAXFocusedUIElementAttribute as CFString, &focusedRef) == .success,
              let focusedRef
        else { return false }
        let element = focusedRef as! AXUIElement

        // Pob's own window is never what a macro is typing into: when focus has
        // stayed here the text is meant for the app underneath, and writing it
        // into our own field would put it somewhere the macro cannot see.
        var pid: pid_t = 0
        if AXUIElementGetPid(element, &pid) == .success, pid == getpid() { return false }

        var settable: DarwinBoolean = false
        guard AXUIElementIsAttributeSettable(element, kAXSelectedTextAttribute as CFString, &settable) == .success,
              settable.boolValue
        else { return false }

        // An unreadable value is one the write could not be checked against
        // afterwards, and an unverified write that turns out to be a no-op
        // becomes typed-twice text once the keystrokes follow it. Hand those
        // straight to the keystrokes, which need no such proof.
        guard let before = axValue(of: element) else { return false }
        guard AXUIElementSetAttributeValue(element, kAXSelectedTextAttribute as CFString,
                                           text as CFString) == .success
        else { return false }
        // The setter returns once the other app has handled it, but the field
        // it redraws is that app's own main thread's work, so give it a beat
        // before reading back.
        try? await Task.sleep(nanoseconds: 30_000_000)
        return axValue(of: element) != before
    }

    private func axValue(of element: AXUIElement) -> String? {
        var valueRef: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, kAXValueAttribute as CFString, &valueRef) == .success
        else { return nil }
        return valueRef as? String
    }

    /// Types text one character at a time as synthesised key events. The
    /// character travels in the event itself rather than as a key position, so
    /// any script arrives intact whatever layout is active — the same trick as
    /// KEYEVENTF_UNICODE on Windows and the spare keycode on X11.
    private func typeAsKeystrokes(_ text: String) async {
        let source = CGEventSource(stateID: .hidSystemState)
        for character in text {
            if character == "\r" { continue }
            if character == "\n" {
                // Return is a key rather than a character: an app watching for
                // the keypress (a form, a shell) sees nothing in a newline
                // delivered as text.
                _ = await performKeyPress(key: "return")
                continue
            }
            // UTF-16 because that is what the event carries; a character
            // outside the basic plane is two units and must go in one event,
            // or it arrives as two halves of nothing.
            let units = Array(String(character).utf16)
            for down in [true, false] {
                guard let event = CGEvent(keyboardEventSource: source, virtualKey: 0, keyDown: down) else { continue }
                event.keyboardSetUnicodeString(stringLength: units.count, unicodeString: units)
                event.post(tap: .cghidEventTap)
            }
            try? await Task.sleep(nanoseconds: 12_000_000) // match the other shells
        }
    }

    /// Presses one key, optionally with modifiers held: "escape", "cmd+v",
    /// "ctrl+alt+shift+f5". The key named is a *position* on the board rather
    /// than a character, so the system's own layout decides what it produces —
    /// which is what lets a keyboard elsewhere forward keys verbatim. A single
    /// character is the other way round: it presses whatever key produces that
    /// character here.
    ///
    /// Answers with the reason nothing was pressed, or nil when the key went
    /// down. A name nobody can resolve used to be logged and shrugged off, and
    /// the caller was told the key was pressed either way — which is how a
    /// macro full of `keyPress("×")` comes back a clean run having done half of
    /// what it says.
    func performKeyPress(key: String) async -> String? {
        let source = CGEventSource(stateID: .hidSystemState)
        // A named key needs nothing from the system to resolve. A character
        // needs the active layout, which only the main thread may ask for — so
        // that hop is taken once the first pass has come back empty, and never
        // on the keys a macro is mostly made of.
        var resolved = Self.resolveKey(key, layout: nil)
        if resolved == nil, let layout = await MainActor.run(body: { Self.currentLayout() }) {
            resolved = Self.resolveKey(key, layout: layout)
        }
        guard let (keyCode, flags) = resolved else {
            AppLogger.log("Unknown key: \(key)")
            return "Unknown key: \(key)"
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
        return nil
    }

    // MARK: - Helpers

    /// Runs `body` with the overlay window set to click-through and the real mouse cursor frozen
    /// in place, so automation events reach the app below without moving the user's pointer.
    ///
    /// `arrivingAt` is where the action is aimed. The pointer is walked there
    /// and given a moment before `body` posts anything, because an app decides
    /// what a button-press landed on from where it believes the pointer is — the
    /// element under it, the hover state that element is in, whether a window
    /// that was not frontmost takes this click or merely comes forward on it.
    /// A press arriving out of nowhere is one it can only half place: the window
    /// activates and the thing under the cursor never hears about it, which
    /// reads exactly like a click that did not happen and takes a second call
    /// to land. The Linux and Windows shells always did this — XTestFakeMotionEvent
    /// and SetCursorPos, respectively, before any button event — and macOS was
    /// the one shell that pressed without ever arriving.
    private func passThrough(arrivingAt target: CGPoint? = nil, _ body: () async -> Void) async {
        await MainActor.run {
            // Before the window property, and for the whole burst rather than
            // just this instant: the policy that also owns it has to be told to
            // hold still, or it puts back whatever it thinks the real pointer
            // implies — most of the way through this method, and again on any
            // mouse-moved event the burst itself generates.
            self.holdClickThrough?(true)
            self.window?.ignoresMouseEvents = true
        }
        // Let the window server process the hit-test change before posting events.
        try? await Task.sleep(nanoseconds: 100_000_000)

        // Freeze the visible cursor so it never moves during the action.
        let savedPos = Self.cgCursorPosition()
        CGAssociateMouseAndMouseCursorPosition(0)

        if let target {
            if let move = CGEvent(mouseEventSource: nil, mouseType: .mouseMoved,
                                  mouseCursorPosition: target, mouseButton: .left)
            {
                move.post(tap: .cghidEventTap)
            }
            // Long enough for the app to have run its own hit-test off the move.
            // It is the app's main thread doing that work, not the window
            // server's, so flushing the event is not the same as it having
            // been acted on.
            try? await Task.sleep(nanoseconds: 50_000_000)
        }

        await body()

        // Restore cursor position and re-couple movement.
        CGWarpMouseCursorPosition(savedPos)
        CGAssociateMouseAndMouseCursorPosition(1)

        await MainActor.run {
            if let holdClickThrough = self.holdClickThrough {
                // Hands the property back to its owner, which decides afresh
                // what the pointer's real position now calls for. Setting it
                // false here instead would be this method's parting guess at
                // that, and wrong for a pointer sitting where clicks should
                // pass through — until the next poll corrected it.
                holdClickThrough(false)
            } else {
                self.window?.ignoresMouseEvents = false
            }
        }
    }

    /// Current system cursor position in CG coordinates (origin: top-left of primary display).
    private static func cgCursorPosition() -> CGPoint {
        guard let primary = NSScreen.screens.first else { return .zero }
        let ns = NSEvent.mouseLocation // NSScreen: y from bottom of primary
        return CGPoint(x: ns.x, y: primary.frame.height - ns.y)
    }

    /// Modifiers a chord may hold, by the names the core sends.
    ///
    /// "cmd" stays what it always was — Command here, Control on the Windows
    /// and Linux shells — because that is what a macro or an MCP call means by
    /// a shortcut, and it should keep meaning the same thing on each platform.
    /// "gui" is the other thing you might mean: the physical key beside the
    /// space bar, which on this machine happens to be Command too. A keyboard
    /// forwarding real keypresses sends that one.
    private static let modifierFlags: [String: CGEventFlags] = [
        "cmd": .maskCommand, "command": .maskCommand,
        "gui": .maskCommand, "win": .maskCommand, "super": .maskCommand, "meta": .maskCommand,
        "ctrl": .maskControl, "control": .maskControl,
        "alt": .maskAlternate, "option": .maskAlternate, "opt": .maskAlternate,
        "shift": .maskShift,
    ]

    /// Virtual key codes, which are positions on the board rather than
    /// characters: 0x00 is wherever "a" sits on a US layout, and the system
    /// applies whatever layout is actually active. Names are the core's.
    private static let keyCodes: [String: CGKeyCode] = [
        "a": 0x00, "b": 0x0B, "c": 0x08, "d": 0x02, "e": 0x0E, "f": 0x03,
        "g": 0x05, "h": 0x04, "i": 0x22, "j": 0x26, "k": 0x28, "l": 0x25,
        "m": 0x2E, "n": 0x2D, "o": 0x1F, "p": 0x23, "q": 0x0C, "r": 0x0F,
        "s": 0x01, "t": 0x11, "u": 0x20, "v": 0x09, "w": 0x0D, "x": 0x07,
        "y": 0x10, "z": 0x06,

        "0": 0x1D, "1": 0x12, "2": 0x13, "3": 0x14, "4": 0x15,
        "5": 0x17, "6": 0x16, "7": 0x1A, "8": 0x1C, "9": 0x19,

        "return": 0x24, "enter": 0x24,
        "tab": 0x30, "space": 0x31,
        "delete": 0x33, "backspace": 0x33, "forwarddelete": 0x75,
        "escape": 0x35, "esc": 0x35,
        "left": 0x7B, "right": 0x7C, "down": 0x7D, "up": 0x7E,
        "home": 0x73, "end": 0x77, "pageup": 0x74, "pagedown": 0x79,
        // A Mac board has no insert key; Help sits in that position.
        "insert": 0x72,
        "capslock": 0x39,

        "minus": 0x1B, "equals": 0x18,
        "leftbracket": 0x21, "rightbracket": 0x1E, "backslash": 0x2A,
        "semicolon": 0x29, "quote": 0x27, "grave": 0x32,
        "comma": 0x2B, "period": 0x2F, "slash": 0x2C,

        "f1": 0x7A, "f2": 0x78, "f3": 0x63, "f4": 0x76,
        "f5": 0x60, "f6": 0x61, "f7": 0x62, "f8": 0x64,
        "f9": 0x65, "f10": 0x6D, "f11": 0x67, "f12": 0x6F,
        "f13": 0x69, "f14": 0x6B, "f15": 0x71, "f16": 0x6A,
        "f17": 0x40, "f18": 0x4F, "f19": 0x50, "f20": 0x5A,

        // The three keys right of a PC's function row. A Mac board puts
        // F13-F15 there instead, so that is what those names reach — the same
        // keycap in the same place.
        "printscreen": 0x69, "scrolllock": 0x6B, "pause": 0x71,
    ]

    /// Resolves "escape", "cmd+v" or "ctrl+alt+shift+f5" into the key to post
    /// and the modifiers to post it with. Everything before the last "+" is a
    /// modifier; the last part is the key.
    ///
    /// `layout` is only consulted for a key written as a character, and a nil
    /// one simply leaves those unresolved — see `currentLayout`, which has to
    /// be read from the main thread and so is not read at all until a first
    /// pass has come back empty.
    private static func resolveKey(_ key: String, layout: KeyboardLayout?) -> (CGKeyCode, CGEventFlags)? {
        // "+" is both the separator and a key somebody may want pressed, and a
        // chord ending in one is the only place the two are told apart:
        // "cmd++" holds Command over the plus key, "+" is that key alone.
        let name: String
        let modifiers: [String]
        if key.hasSuffix("+") {
            name = "+"
            modifiers = key.dropLast().split(separator: "+").map(String.init)
        } else {
            var parts = key.split(separator: "+").map(String.init)
            guard let last = parts.popLast() else { return nil }
            name = last
            modifiers = parts
        }

        // A name is looked up in lower case, the way it is documented; a
        // character is looked up as it was written, since case is what tells
        // "%" from "5".
        guard var resolved = keyCodes[name.lowercased()].map({ ($0, CGEventFlags()) })
            ?? layout.flatMap({ characterKey(name, on: $0) })
        else { return nil }

        for modifier in modifiers {
            guard let flag = modifierFlags[modifier.lowercased()] else { return nil }
            resolved.1.insert(flag)
        }
        return resolved
    }

    /// The active keyboard layout, copied out of the system so the lookup below
    /// can run anywhere.
    struct KeyboardLayout {
        let data: Data
        let type: UInt32
    }

    /// Reads the layout in use. Main thread only, and not by choice: the Text
    /// Input Sources API asserts the queue it is called on and takes the
    /// process down with it from anywhere else.
    @MainActor
    static func currentLayout() -> KeyboardLayout? {
        guard let source = TISCopyCurrentKeyboardLayoutInputSource()?.takeRetainedValue(),
              let dataRef = TISGetInputSourceProperty(source, kTISPropertyUnicodeKeyLayoutData)
        else { return nil }
        // Copied rather than referenced: what comes back belongs to the input
        // source, and the lookup reads it on another thread.
        let cfData = Unmanaged<CFData>.fromOpaque(dataRef).takeUnretainedValue()
        let length = CFDataGetLength(cfData)
        guard length > 0 else { return nil }
        var bytes = [UInt8](repeating: 0, count: length)
        CFDataGetBytes(cfData, CFRange(location: 0, length: length), &bytes)
        return KeyboardLayout(data: Data(bytes), type: UInt32(LMGetKbdType()))
    }

    /// The key that produces `character` on the layout in use, and the
    /// modifiers to hold while pressing it — "*" is shift and the 8 key on a US
    /// board, and a different key somewhere else.
    ///
    /// The named keys above are positions and stay positions. This is the other
    /// question, the one a model asking for `keyPress("=")` or `keyPress("*")`
    /// is really asking: press whatever key puts this character on screen. It
    /// is answered against the active layout rather than a table of US keycaps,
    /// so the answer is right on the board actually plugged in — and there is
    /// no answer at all for a character the layout cannot produce, which is a
    /// failure the caller hears about rather than a key pressed at random.
    private static func characterKey(_ character: String, on layout: KeyboardLayout)
        -> (CGKeyCode, CGEventFlags)?
    {
        guard character.count == 1 else { return nil }

        // Plain first, so a character reachable without modifiers is never
        // reached with them.
        let levels: [(UInt32, CGEventFlags)] = [
            (0, []),
            (UInt32(shiftKey >> 8), .maskShift),
            (UInt32(optionKey >> 8), .maskAlternate),
            (UInt32((shiftKey | optionKey) >> 8), [.maskShift, .maskAlternate]),
        ]
        let keyboardType = layout.type

        return layout.data.withUnsafeBytes { raw -> (CGKeyCode, CGEventFlags)? in
            guard let layout = raw.baseAddress?.assumingMemoryBound(to: UCKeyboardLayout.self)
            else { return nil }
            for (modifierState, flags) in levels {
                for code in CGKeyCode(0) ..< CGKeyCode(128) {
                    var deadKeyState: UInt32 = 0
                    var length = 0
                    var characters = [UniChar](repeating: 0, count: 4)
                    let status = UCKeyTranslate(layout, code, UInt16(kUCKeyActionDown),
                                                modifierState, keyboardType,
                                                OptionBits(kUCKeyTranslateNoDeadKeysBit),
                                                &deadKeyState, characters.count, &length,
                                                &characters)
                    guard status == noErr, length > 0 else { continue }
                    if String(utf16CodeUnits: characters, count: length) == character {
                        return (code, flags)
                    }
                }
            }
            return nil
        }
    }
}
