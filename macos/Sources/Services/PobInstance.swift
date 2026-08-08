import AppKit
import Combine

/// The running Pob instance: its logs/<instance>/ directory, settings.json,
/// Go core child process and virtual cursor. Created by ContentView as a
/// @StateObject, so its lifetime is the window's lifetime — closing the
/// window releases the instance, which stops its pob-core.
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

        // SwiftUI builds this scene before the app delegate can refuse a
        // second launch, so the refusal has to be honoured here too: without
        // the machine's instance lock, starting a core would leave a
        // pob-core, a control port and a claim on the server port behind for
        // the few moments before the app quits.
        guard SettingsService.holdsInstanceLock else { return }
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
        // recreates it on the next launch. Opt out: always start with exactly
        // one window, whatever the last run left behind.
        window.isRestorable = false

        if let savedFrame = settings.getWindowFrame() {
            window.setFrame(savedFrame, display: true)
        } else {
            window.setFrame(NSRect(x: 100, y: 100, width: 600, height: 400), display: true)
            window.center()
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

        updateIgnoresMouseEvents()
    }

    // MARK: - Click-through

    /// Width of the window-edge band kept live while click-through is on, so
    /// the frame can still be grabbed to resize.
    private static let resizeBorder: CGFloat = 6

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
        guard mouse.x >= wf.minX, mouse.x <= wf.maxX,
              mouse.y >= wf.minY, mouse.y <= wf.maxY
        else {
            window.ignoresMouseEvents = true
            return
        }

        // Top 50 pt covers the compact unified toolbar + traffic-light buttons.
        let inToolbar = mouse.y >= (wf.maxY - 50)
        // The edges stay live so the window can still be resized while clicks
        // pass through everything inside them. A locked (or executing) window
        // drops .resizable — then the band would only swallow clicks meant for
        // the app below.
        let border = Self.resizeBorder
        let onEdge = window.styleMask.contains(.resizable) &&
            (mouse.x - wf.minX <= border || wf.maxX - mouse.x <= border ||
             mouse.y - wf.minY <= border || wf.maxY - mouse.y <= border)
        window.ignoresMouseEvents = !(inToolbar || onEdge)
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
