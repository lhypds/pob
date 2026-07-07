import AppKit
import Combine

/// One running Pob instance inside this process — the VSCode model: a single
/// app process owns many windows, and each window is a full instance with its
/// own logs/<instance>/ directory, settings.json copy, Go core child process
/// and virtual cursor. Created by ContentView as a @StateObject, so its
/// lifetime is the window's lifetime: closing the window releases the
/// instance, which stops its pob-core.
final class PobInstance: NSObject, ObservableObject {
    /// All live instances (weak). Used for the app-wide mouse-move monitors
    /// (click-through), window cascading and shutdown on app termination.
    private static let registry = NSHashTable<PobInstance>.weakObjects()
    static var all: [PobInstance] { registry.allObjects }

    let settings: SettingsService
    let mouse: MouseService
    let bridge: CoreBridge
    let recorder: UserMacroRecorder

    private(set) weak var window: NSWindow?
    private var clickThroughEnabled = false
    private var windowObservers: [NSObjectProtocol] = []

    override init() {
        let settings = SettingsService()
        let mouse = MouseService()
        self.settings = settings
        self.mouse = mouse
        bridge = CoreBridge(settings: settings, mouse: mouse)
        recorder = UserMacroRecorder(settings: settings)
        super.init()
        PobInstance.registry.add(self)
        bridge.start()
    }

    deinit {
        for observer in windowObservers {
            NotificationCenter.default.removeObserver(observer)
        }
        bridge.stop()
    }

    func shutdown() {
        bridge.stop()
    }

    // MARK: - Window

    /// Called once the SwiftUI window hosting this instance's ContentView
    /// exists: applies the overlay styling (previously done by AppDelegate
    /// for the single window) and restores the saved frame from this
    /// instance's settings.
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
        window.title = "Pob \(AppDelegate.loadVersion())"
        window.toolbarStyle = .unifiedCompact

        window.styleMask.insert(.resizable)
        window.styleMask.insert(.miniaturizable)
        window.styleMask.insert(.closable)

        window.level = .floating
        window.ignoresMouseEvents = false
        // macOS remembers the app's window set at quit (per bundle id) and
        // recreates every window on the next launch — each one a full
        // instance here, so a run that quit with 3 windows would start with
        // 3. Opt out: always start with exactly one window.
        window.isRestorable = false

        if let savedFrame = settings.getWindowFrame() {
            window.setFrame(savedFrame, display: true)
        } else {
            window.setFrame(NSRect(x: 100, y: 100, width: 600, height: 400), display: true)
            window.center()
        }

        // Later windows restore the same template frame — cascade them so
        // they don't stack exactly on top of each other.
        let siblings = PobInstance.all.filter { $0 !== self && $0.window != nil }.count
        if siblings > 0 {
            var frame = window.frame
            frame.origin.x += CGFloat(28 * siblings)
            frame.origin.y -= CGFloat(28 * siblings)
            window.setFrame(frame, display: true)
        }

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
                self?.saveWindowFrame()
            },
            nc.addObserver(forName: NSWindow.didEndLiveResizeNotification, object: window, queue: .main) { [weak self] _ in
                self?.saveWindowFrame()
            },
            // SwiftUI can keep the window's scene state (and thus this
            // object) alive after close, so stop the Go core explicitly
            // rather than relying on deinit.
            nc.addObserver(forName: NSWindow.willCloseNotification, object: window, queue: .main) { [weak self] _ in
                self?.shutdown()
            },
        ]

        window.standardWindowButton(.closeButton)?.isEnabled = true
        window.standardWindowButton(.miniaturizeButton)?.isEnabled = true
        window.standardWindowButton(.zoomButton)?.isEnabled = true

        updateIgnoresMouseEvents()
    }

    // MARK: - Click-through

    /// Called by the view whenever isExecuting or isTargeting changes.
    func setClickThrough(_ enabled: Bool) {
        clickThroughEnabled = enabled
        updateIgnoresMouseEvents()
    }

    /// Central function — called for every mouseMoved event AND on any state
    /// change. When click-through is disabled (targeting / executing) it
    /// ACTIVELY sets ignoresMouseEvents = false on every call, so no stale
    /// monitor callback can re-enable it.
    func updateIgnoresMouseEvents() {
        guard let window else { return }

        guard clickThroughEnabled else {
            window.ignoresMouseEvents = false
            return
        }

        let mouse = NSEvent.mouseLocation
        let wf = window.frame
        // Top 50 pt covers the compact unified toolbar + traffic-light buttons.
        let inToolbar = mouse.x >= wf.minX && mouse.x <= wf.maxX &&
            mouse.y >= (wf.maxY - 50) && mouse.y <= wf.maxY
        window.ignoresMouseEvents = !inToolbar
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
