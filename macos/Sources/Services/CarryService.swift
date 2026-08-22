import AppKit
import ApplicationServices

/// Carries the windows Pob frames along when the frame itself is dragged.
///
/// The overlay is a hole punched over somebody else's desktop: the content area
/// is what a screenshot shows and what every click is aimed through, so how the
/// frame sits over what is under it is the whole arrangement. Moving the frame
/// normally leaves all of that where it was and the arrangement with it — the
/// picture slides off the apps it was framing. Carrying moves everything under
/// the frame by the frame's own delta instead, and the scene stays as it was
/// set up.
///
/// This is what the lock turns on, and half of what the lock means: a locked
/// frame keeps its size and keeps what it frames, which together are what let a
/// macro's coordinates survive the window being nudged (see ContentView's
/// updateWindowLock).
///
/// What counts as under the frame is what the frame shows: a window is carried
/// when it overlaps the content area at all, the same test that decides whether
/// it turns up in a screenshot. A window Pob only shows a corner of is a window
/// Pob is framing, which is the ordinary case of a frame parked over part of
/// something bigger than itself.
///
/// Only the moving is carried. A resize changes what the frame *covers* rather
/// than where it sits, which is a different question, and one an app that
/// refuses to be resized cannot be asked. The lock takes resizing away while
/// this is on, so that case should not arise — the guard below does not rely on
/// it having been taken away.
final class CarryService {
    /// One window carried through a drag, and where it stood when that drag
    /// began.
    private struct Carried {
        let element: AXUIElement
        /// CG screen coordinates (origin top-left), like kAXPositionAttribute.
        let origin: CGPoint
    }

    /// The windows carried through one drag, and where the frame stood when it
    /// began.
    ///
    /// Every move sets each window to `its origin + (frame now − frameOrigin)`
    /// rather than nudging it by each step's delta: a drag is hundreds of
    /// moves, and each one rounded to the pixel the window server hands back
    /// would walk the windows away from the frame a fraction at a time.
    ///
    /// `carried` is empty when the search found nothing to take along. That
    /// case is latched too — the alternative is running the whole window-list
    /// and Accessibility search again on every move of a drag over bare
    /// desktop.
    private struct Latch {
        let carried: [Carried]
        /// NSScreen coordinates (origin bottom-left), like NSWindow.frame.
        let frameOrigin: CGPoint
    }

    /// How often a held latch is checked for the end of the drag that took it.
    private static let latchPollInterval: TimeInterval = 0.1

    /// How long a held button with a frame standing still gives up its latch
    /// anyway. Only a stuck button state gets this far — it is the backstop
    /// that keeps one bad reading from carrying into the next drag.
    private static let latchIdlePolls = 10

    /// Windows smaller than this on either side are not carried — the 1×1
    /// markers some apps park on screen, and the rest of the window list's
    /// degenerate furniture. Anything a person could actually see inside the
    /// frame clears it comfortably.
    private static let minimumWindowSize: CGFloat = 40

    /// A ceiling on how many windows one drag will carry. Each one costs a
    /// cross-process Accessibility write per move, and the frame is redrawn
    /// sixty times a second while it is dragged — past some number of windows
    /// the drag itself would start to stutter. A frame would have to be parked
    /// over most of a full desktop to reach this.
    private static let maximumCarried = 16

    /// How far a window's Accessibility geometry may sit from the same window's
    /// entry in the window list and still be taken for the same window. The two
    /// come from different subsystems and disagree by a hair on some apps.
    private static let matchTolerance: CGFloat = 4

    /// Accessibility calls block the caller until the other app answers, and
    /// this runs on the main thread in the middle of a drag — an app busy
    /// enough not to answer would otherwise take Pob's window with it.
    private static let messagingTimeout: Float = 0.25

    private(set) var isEnabled = false

    weak var window: NSWindow?

    private var latch: Latch?
    private var dragWatch: Timer?
    private var movedSincePoll = false
    private var idlePolls = 0

    /// Where the frame stood before the move now being reported. A latch is
    /// anchored here rather than at the frame's current origin: by the time the
    /// first move of a drag arrives the frame has already left, and anchoring
    /// to where it landed would bake that first step in as a permanent offset
    /// between the frame and what it carries.
    private var previousOrigin: CGPoint = .zero

    // MARK: - Window

    func attach(window: NSWindow) {
        self.window = window
        previousOrigin = window.frame.origin
        release()
    }

    func setEnabled(_ enabled: Bool) {
        guard isEnabled != enabled else { return }
        isEnabled = enabled
        // Turning it off mid-drag has to let go of what it was holding, or the
        // rest of that drag would still be carrying it.
        if !enabled { release() }
    }

    private func release() {
        dragWatch?.invalidate()
        dragWatch = nil
        movedSincePoll = false
        idlePolls = 0
        latch = nil
    }

    // MARK: - Carrying

    /// The frame moved. Called for every step of a drag, so everything here is
    /// either cheap or latched.
    func windowDidMove() {
        guard let window else { return }
        let origin = window.frame.origin
        // Kept current whether or not anything is carried: switching Carry on
        // mid-session must not measure its first drag from wherever the frame
        // happened to be when the window was attached.
        defer { previousOrigin = origin }

        // Dragging the top or left edge moves the origin as it resizes, and a
        // resize is not a move: it changes what the frame covers rather than
        // where it sits, and the window below is meant to stay put under it.
        //
        // Carry follows drags, hence the held button. A window macOS places on
        // its own — restoring a frame at launch, nudging one back on screen
        // when a display goes away, Stage Manager shuffling it aside — moves
        // with nobody holding it, and dragging some app along with that is
        // nobody's intent.
        guard isEnabled, origin != previousOrigin, !window.inLiveResize,
              NSEvent.pressedMouseButtons != 0 else { return }

        let latch = self.latch ?? acquireLatch(anchoredAt: previousOrigin, movedTo: origin)
        self.latch = latch
        startDragWatch()

        // NSScreen counts Y up from the bottom of the primary screen and
        // Accessibility counts it down from the top, so the frame's vertical
        // delta arrives at the windows below with its sign flipped.
        let dx = origin.x - latch.frameOrigin.x
        let dy = origin.y - latch.frameOrigin.y
        for window in latch.carried {
            setPosition(window.element, to: CGPoint(x: window.origin.x + dx,
                                                    y: window.origin.y - dy))
        }
    }

    /// Holds the latch for exactly as long as the drag that took it.
    ///
    /// Letting it lapse on a lull instead would quietly change what is being
    /// carried halfway through a drag: a frame that pauses and then moves on
    /// re-runs the search and picks up whatever has since come under it,
    /// so a slow drag across a busy desktop would gather windows as it went.
    /// The set is decided once, when the frame is picked up.
    private func startDragWatch() {
        movedSincePoll = true
        guard dragWatch == nil else { return }
        let timer = Timer(timeInterval: Self.latchPollInterval, repeats: true) { [weak self] timer in
            guard let self else { timer.invalidate(); return }
            guard NSEvent.pressedMouseButtons != 0 else { return self.release() }
            idlePolls = movedSincePoll ? 0 : idlePolls + 1
            movedSincePoll = false
            if idlePolls >= Self.latchIdlePolls { self.release() }
        }
        // .common, so the watch keeps running while the window server is
        // running a drag's own tracking loop.
        RunLoop.main.add(timer, forMode: .common)
        dragWatch = timer
    }

    /// Finds the windows under the frame as the frame stood at `anchor`, and
    /// notes where each of them was. A latch always comes back — an empty one
    /// when there is nothing under the frame to carry — so the search runs once
    /// per drag either way.
    private func acquireLatch(anchoredAt anchor: CGPoint, movedTo origin: CGPoint) -> Latch {
        let empty = Latch(carried: [], frameOrigin: anchor)
        guard let window, AXIsProcessTrusted() else { return empty }
        // The search wants the content area where the drag started, not where
        // this first step has already put it: a fast grab can cover half a
        // screen before the first move arrives, by which time the frame may be
        // over something else entirely.
        guard var searchRect = ScreenshotService.shared.contentRectInCGCoordinates(of: window) else { return empty }
        searchRect = searchRect.offsetBy(dx: anchor.x - origin.x, dy: origin.y - anchor.y)

        let carried = windows(under: searchRect, below: CGWindowID(window.windowNumber))
        return Latch(carried: carried, frameOrigin: anchor)
    }

    // MARK: - Finding the window below

    /// Every ordinary window overlapping the frame that Accessibility will let
    /// Pob move, front to back.
    ///
    /// The window list answers front-to-back and is asked for the windows below
    /// Pob's own, which is how the capture already excludes the overlay from
    /// its own screenshots. It gives geometry and an owning process but no way
    /// to move anything, so each match is carried into Accessibility — the same
    /// door the mouse and keyboard already go through — by finding that
    /// process's window standing in the same place.
    ///
    /// A window that will not be moved is passed over rather than ending the
    /// search: it is one window in the frame staying behind, not a reason to
    /// leave the rest of them behind with it.
    private func windows(under rect: CGRect, below pobWindow: CGWindowID) -> [Carried] {
        let options: CGWindowListOption = [.optionOnScreenBelowWindow, .excludeDesktopElements]
        guard let list = CGWindowListCopyWindowInfo(options, pobWindow) as? [[String: Any]] else { return [] }

        let ownPID = ProcessInfo.processInfo.processIdentifier
        // One application element per process: an app with several windows in
        // the frame is the common case, and each one is a fresh connection and
        // a fresh window-list fetch otherwise.
        var applications: [pid_t: AXUIElement] = [:]
        var carried: [Carried] = []

        for info in list where carried.count < Self.maximumCarried {
            // Layer 0 is where ordinary document windows live; the menu bar,
            // the Dock, and every floating panel sit above or below it.
            guard info[kCGWindowLayer as String] as? Int == 0,
                  let pid = info[kCGWindowOwnerPID as String] as? pid_t, pid != ownPID,
                  info[kCGWindowAlpha as String] as? Double ?? 1 > 0,
                  let boundsDict = info[kCGWindowBounds as String] as? NSDictionary,
                  let bounds = CGRect(dictionaryRepresentation: boundsDict as CFDictionary),
                  bounds.width >= Self.minimumWindowSize,
                  bounds.height >= Self.minimumWindowSize,
                  bounds.intersects(rect) else { continue }

            let application = applications[pid] ?? {
                let created = AXUIElementCreateApplication(pid)
                AXUIElementSetMessagingTimeout(created, Self.messagingTimeout)
                applications[pid] = created
                return created
            }()

            guard let element = accessibilityWindow(of: application, standingAt: bounds),
                  let origin = axPoint(element, kAXPositionAttribute) else { continue }
            carried.append(Carried(element: element, origin: origin))
        }
        return carried
    }

    private func accessibilityWindow(of app: AXUIElement, standingAt bounds: CGRect) -> AXUIElement? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(app, kAXWindowsAttribute as CFString, &value) == .success,
              let windows = value as? [AXUIElement] else { return nil }

        let tolerance = Self.matchTolerance
        for element in windows {
            guard let origin = axPoint(element, kAXPositionAttribute),
                  let size = axSize(element, kAXSizeAttribute),
                  abs(origin.x - bounds.minX) <= tolerance,
                  abs(origin.y - bounds.minY) <= tolerance,
                  abs(size.width - bounds.width) <= tolerance,
                  abs(size.height - bounds.height) <= tolerance else { continue }

            // Full-screen windows, and windows of apps that expose them without
            // letting them be placed, report the attribute but refuse to take a
            // new value. Asking once here is what keeps a whole drag from
            // making a failed cross-process call per move.
            var settable: DarwinBoolean = false
            guard AXUIElementIsAttributeSettable(element, kAXPositionAttribute as CFString, &settable) == .success,
                  settable.boolValue else { return nil }
            return element
        }
        return nil
    }

    // MARK: - Accessibility values

    private func axPoint(_ element: AXUIElement, _ attribute: String) -> CGPoint? {
        guard let value = axValue(element, attribute) else { return nil }
        var point = CGPoint.zero
        guard AXValueGetValue(value, .cgPoint, &point) else { return nil }
        return point
    }

    private func axSize(_ element: AXUIElement, _ attribute: String) -> CGSize? {
        guard let value = axValue(element, attribute) else { return nil }
        var size = CGSize.zero
        guard AXValueGetValue(value, .cgSize, &size) else { return nil }
        return size
    }

    private func axValue(_ element: AXUIElement, _ attribute: String) -> AXValue? {
        var ref: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, attribute as CFString, &ref) == .success,
              let result = ref, CFGetTypeID(result) == AXValueGetTypeID() else { return nil }
        return (result as! AXValue)
    }

    private func setPosition(_ element: AXUIElement, to point: CGPoint) {
        var target = point
        guard let value = AXValueCreate(.cgPoint, &target) else { return }
        AXUIElementSetAttributeValue(element, kAXPositionAttribute as CFString, value)
    }
}
