import AppKit
import Combine

/// The running Pob instance: its ~/.pob/<instance>/ directory, the machine's
/// settings.json, Go core child process and virtual cursor. Created by
/// ContentView as a @StateObject, so its lifetime is the window's lifetime —
/// closing the window releases the instance, which stops its pob-core.
final class PobInstance: NSObject, ObservableObject {
    /// The live instance (weak), for the app-wide mouse-move monitors
    /// (click-through) and shutdown on app termination. A table rather than a
    /// single reference because SwiftUI can hold a replaced @StateObject
    /// briefly, and a stale one must not be the one the monitors drive.
    private static let registry = NSHashTable<PobInstance>.weakObjects()
    static var all: [PobInstance] { registry.allObjects }

    let settings: SettingsService
    let mouse: MouseService
    let bridge: CoreBridge
    let recorder: UserMacroRecorder
    let carry: CarryService

    private(set) weak var window: NSWindow?
    private var clickThroughEnabled = false
    private var clickThroughPoll: Timer?
    private var windowObservers: [NSObjectProtocol] = []

    override init() {
        let settings = SettingsService()
        let mouse = MouseService()
        self.settings = settings
        self.mouse = mouse
        bridge = CoreBridge(settings: settings, mouse: mouse)
        recorder = UserMacroRecorder(settings: settings)
        carry = CarryService()
        super.init()
        PobInstance.registry.add(self)

        // SwiftUI builds this scene before the app delegate can refuse a
        // second launch, so the refusal has to be honoured here too: without
        // the machine's instance lock, starting a core would leave a
        // pob-core, a control port and a claim on the server port behind for
        // the few moments before the app quits.
        guard SettingsService.holdsInstanceLock else { return }
        bridge.start()
    }

    deinit {
        clickThroughPoll?.invalidate()
        for observer in windowObservers {
            NotificationCenter.default.removeObserver(observer)
        }
        bridge.stop()
    }

    func shutdown() {
        clickThroughPoll?.invalidate()
        clickThroughPoll = nil
        carry.setEnabled(false)
        bridge.stop()
    }

    // MARK: - Window

    /// Called once the SwiftUI window hosting this instance's ContentView
    /// exists: applies the overlay styling (previously done by AppDelegate
    /// for the single window) and restores the saved frame from this
    /// instance's instance.json.
    func attach(window: NSWindow) {
        guard self.window !== window else { return }
        self.window = window
        mouse.window = window
        bridge.window = window
        recorder.window = window

        window.isOpaque = false
        window.backgroundColor = NSColor.clear
        window.titlebarAppearsTransparent = false
        window.titleVisibility = .hidden
        window.title = "Pob \(AppDelegate.version)"
        window.toolbarStyle = .unifiedCompact

        window.styleMask.insert(.resizable)
        window.styleMask.insert(.miniaturizable)
        window.styleMask.insert(.closable)

        window.level = .floating
        window.ignoresMouseEvents = false
        // Without this the window never receives mouseMoved while it is key,
        // so the local monitor below could not keep the click-through state
        // current over the window's own live areas.
        window.acceptsMouseMovedEvents = true
        // macOS remembers the app's window set at quit (per bundle id) and
        // recreates it on the next launch. Opt out: always start with exactly
        // one window, whatever the last run left behind.
        window.isRestorable = false

        if let savedFrame = settings.getWindowFrame() {
            window.setFrame(savedFrame, display: true)
        } else {
            window.setFrame(NSRect(x: 100, y: 100, width: 600, height: 400), display: true)
            window.center()
        }

        // After the frame is restored, so the first drag is measured from where
        // the window actually starts rather than from wherever SwiftUI first
        // put it.
        carry.attach(window: window)

        // Observe rather than replace window.delegate: the delegate belongs
        // to SwiftUI's WindowGroup scene bookkeeping, and stealing it makes
        // SwiftUI lose track of its windows and open spurious extra ones on
        // app activation.
        let nc = NotificationCenter.default
        for observer in windowObservers {
            nc.removeObserver(observer)
        }
        windowObservers = [
            nc.addObserver(forName: NSWindow.didMoveNotification, object: window, queue: .main) { [weak self] _ in
                // First: the window below is carried by the frame's delta, and
                // saving or remapping in front of it only widens the gap
                // between the frame and what it is holding.
                self?.carry.windowDidMove()
                self?.saveWindowFrame()
                self?.bridge.windowGeometryChanged()
            },
            nc.addObserver(forName: NSWindow.didEndLiveResizeNotification, object: window, queue: .main) { [weak self] _ in
                self?.saveWindowFrame()
                self?.bridge.windowGeometryChanged()
            },
            // SwiftUI can keep the window's scene state (and thus this
            // object) alive after close, so stop the Go core explicitly
            // rather than relying on deinit.
            nc.addObserver(forName: NSWindow.willCloseNotification, object: window, queue: .main) { [weak self] _ in
                self?.shutdown()
            },
        ]

        // Seed the pixel→screen mapping and the box the cursor is held inside
        // from the window as it stands, so both are right before the first
        // screenshot rather than only after it.
        bridge.windowGeometryChanged()

        window.standardWindowButton(.closeButton)?.isEnabled = true
        window.standardWindowButton(.miniaturizeButton)?.isEnabled = true
        window.standardWindowButton(.zoomButton)?.isEnabled = true

        // The lock comes back with the frame. The view owns it from here on —
        // it is applied again as the window is attached because the window can
        // arrive after the view has already read the saved state, and a window
        // restored to a locked instance must not be resizable in between. Last,
        // so it is the same order the view's own lock takes: the window set up
        // first, then held to its size.
        if settings.getWindowLocked() {
            window.styleMask.remove(.resizable)
            carry.setEnabled(true)
        }

        updateClickThroughPolling()
        updateIgnoresMouseEvents()
    }

    // MARK: - Click-through

    /// Width of the window-edge band kept live while click-through is on, so
    /// the frame can still be grabbed to resize. The band reaches the same
    /// distance outside the frame, where macOS puts the rest of its own resize
    /// region.
    private static let resizeBorder: CGFloat = 6

    /// How often the pointer position is re-read while click-through is on.
    /// Short enough that the edge band is live by the time a pointer that just
    /// entered it can press a button.
    private static let clickThroughPollInterval: TimeInterval = 0.05

    /// Called by the view whenever isExecuting or isTargeting changes.
    func setClickThrough(_ enabled: Bool) {
        clickThroughEnabled = enabled
        updateClickThroughPolling()
        updateIgnoresMouseEvents()
    }

    /// The app-wide mouseMoved monitors have a blind spot: while Pob is the
    /// focused app and the pointer is over the part of its own window that
    /// clicks pass through, the move reaches neither Pob (its window ignores
    /// it) nor the app below (which is not the active one), so neither monitor
    /// fires. Whatever ignoresMouseEvents was when the pointer entered then
    /// sticks — which is why a focused window could no longer be grabbed by
    /// its edge to resize. Poll for as long as click-through is on to cover it.
    private func updateClickThroughPolling() {
        let wanted = clickThroughEnabled && window != nil
        guard wanted != (clickThroughPoll != nil) else { return }

        guard wanted else {
            clickThroughPoll?.invalidate()
            clickThroughPoll = nil
            return
        }

        let timer = Timer(timeInterval: Self.clickThroughPollInterval, repeats: true) { [weak self] _ in
            self?.updateIgnoresMouseEvents()
        }
        // .common, so the poll keeps running through menu tracking and drags.
        RunLoop.main.add(timer, forMode: .common)
        clickThroughPoll = timer
    }

    /// Central function — called for every mouseMoved event, on every poll AND
    /// on any state change. When click-through is disabled (targeting /
    /// executing) it ACTIVELY sets ignoresMouseEvents = false on every call, so
    /// no stale monitor callback can re-enable it.
    func updateIgnoresMouseEvents() {
        guard let window else { return }

        guard clickThroughEnabled else {
            setIgnoresMouseEvents(false, on: window)
            return
        }

        // Never re-decide under a button that is already down: during a resize
        // the pointer leads the window's edge, so the band would read as left
        // behind and the window would go click-through mid-drag.
        guard !window.inLiveResize, NSEvent.pressedMouseButtons == 0 else { return }

        let mouse = NSEvent.mouseLocation
        let wf = window.frame
        let border = Self.resizeBorder

        // The edges stay live so the window can still be resized while clicks
        // pass through everything inside them. A locked (or executing) window
        // drops .resizable — then the band would only swallow clicks meant for
        // the app below.
        let onEdge = window.styleMask.contains(.resizable) &&
            wf.insetBy(dx: -border, dy: -border).contains(mouse) &&
            (mouse.x - wf.minX <= border || wf.maxX - mouse.x <= border ||
             mouse.y - wf.minY <= border || wf.maxY - mouse.y <= border)
        // Top 50 pt covers the compact unified toolbar + traffic-light buttons.
        let inToolbar = mouse.x >= wf.minX && mouse.x <= wf.maxX &&
            mouse.y >= (wf.maxY - 50) && mouse.y <= wf.maxY

        setIgnoresMouseEvents(!(inToolbar || onEdge), on: window)
    }

    /// Each assignment is a round trip to the window server and the poll runs
    /// twenty times a second, so only write real changes.
    private func setIgnoresMouseEvents(_ ignores: Bool, on window: NSWindow) {
        guard window.ignoresMouseEvents != ignores else { return }
        window.ignoresMouseEvents = ignores
    }

    /// Driven by AppDelegate's app-wide mouseMoved monitors.
    static func updateAllIgnoresMouseEvents() {
        for instance in all {
            instance.updateIgnoresMouseEvents()
        }
    }
}

extension PobInstance {
    private func saveWindowFrame() {
        guard let window else { return }
        settings.saveWindowFrame(window.frame)
    }
}
