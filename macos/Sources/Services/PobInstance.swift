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
    /// True while MouseService is posting an automation burst at the app below,
    /// during which the click-through decision below is suspended — see
    /// setAutomationPassThrough.
    private var automationPassThrough = false
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
        mouse.holdClickThrough = { [weak self] posting in
            self?.setAutomationPassThrough(posting)
        }

        window.isOpaque = false
        window.backgroundColor = NSColor.clear
        window.titlebarAppearsTransparent = false
        window.titleVisibility = .hidden
        window.title = "Pob \(AppDelegate.version)"
        window.toolbarStyle = .unifiedCompact

        if AppOptions.fullscreen {
            applyFullscreenStyle(to: window)
        } else {
            window.styleMask.insert(.resizable)
            window.styleMask.insert(.miniaturizable)
            window.styleMask.insert(.closable)
            window.level = .floating
        }
        window.ignoresMouseEvents = false
        // Without this the window never receives mouseMoved while it is key,
        // so the local monitor below could not keep the click-through state
        // current over the window's own live areas.
        window.acceptsMouseMovedEvents = true
        // macOS remembers the app's window set at quit (per bundle id) and
        // recreates it on the next launch. Opt out: always start with exactly
        // one window, whatever the last run left behind.
        window.isRestorable = false

        if AppOptions.fullscreen {
            window.setFrame(fullscreenFrame(for: window), display: true)
        } else if let savedFrame = settings.getWindowFrame() {
            window.setFrame(savedFrame, display: true)
        } else {
            window.setFrame(startingFrame(for: window), display: true)
            window.center()
        }

        // After the frame is restored, so the first drag is measured from where
        // the window actually starts rather than from wherever SwiftUI first
        // put it. A fullscreen window is never dragged — there is no titlebar
        // to take hold of — so nothing is ever carried under it either.
        if !AppOptions.fullscreen {
            carry.attach(window: window)
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
        //
        // Nothing to apply in fullscreen: the frame is the display, and it is
        // already held there by having no way to move or resize it.
        if !AppOptions.fullscreen, settings.getWindowLocked() {
            window.styleMask.remove(.resizable)
            carry.setEnabled(true)
        }

        updateClickThroughPolling()
        updateIgnoresMouseEvents()
    }

    // MARK: - Fullscreen

    /// The fullscreen overlay: the whole display, with nothing of Pob's own
    /// drawn on it.
    ///
    /// Borderless rather than a titlebar hidden by other means, because the
    /// titlebar is also what `contentLayoutRect` is measured against — the area
    /// a screenshot covers, and the box the virtual cursor lives in. With no
    /// titlebar at all the window's content *is* the display, which is what a
    /// fullscreen Pob is for.
    ///
    /// The level clears the menu bar (`.mainMenu`, 24) and the Dock (20), so
    /// "the whole display" includes the two strips a zoomed window leaves
    /// alone. It joins every Space and stays out of macOS's own full-screen
    /// mode, which would put it on a Space of its own with nothing underneath.
    private func applyFullscreenStyle(to window: NSWindow) {
        window.styleMask = [.borderless]
        window.toolbar = nil
        window.isMovable = false
        window.level = NSWindow.Level(rawValue: NSWindow.Level.mainMenu.rawValue + 1)
        window.collectionBehavior = [.canJoinAllSpaces, .stationary, .fullScreenNone]
    }

    /// The display the window came up on, whole: `frame` rather than
    /// `visibleFrame`, which stops short of the menu bar and the Dock.
    private func fullscreenFrame(for window: NSWindow) -> NSRect {
        let screen = window.screen ?? NSScreen.main ?? NSScreen.screens.first
        return screen?.frame ?? NSRect(x: 0, y: 0, width: 1440, height: 900)
    }

    /// What a brand new instance opens at, before it has a frame of its own.
    ///
    /// 1024×768 rather than something smaller, because the frame is the screen
    /// as far as a macro is concerned: every coordinate a macro holds is a
    /// position inside it, and a frame that has to be dragged bigger before the
    /// first macro is recorded is a frame whose coordinates were all written
    /// against a size nobody meant. It is also exactly the microVM's screen
    /// (see vm/msb/run.sh), so an instance recorded here is an instance that
    /// fits when the same ~/.pob is copied into the guest.
    ///
    /// The same size on all three shells — the Windows and X11 halves are in
    /// win/src/App.xaml.cs and linux-x11/src/main.c.
    private static let startingSize = NSSize(width: 1024, height: 768)

    /// That size, less whatever of it this display has not got. A default big
    /// enough to hang off the screen would be a window with its titlebar out of
    /// reach on the machines that can least afford it.
    private func startingFrame(for window: NSWindow) -> NSRect {
        let screen = window.screen ?? NSScreen.main ?? NSScreen.screens.first
        let room = screen?.visibleFrame.size ?? Self.startingSize
        return NSRect(x: 0, y: 0,
                      width: min(Self.startingSize.width, room.width),
                      height: min(Self.startingSize.height, room.height))
    }

    // MARK: - Hidden menu

    /// Where the dot a hidden menu leaves behind sits, and how big it is —
    /// shared by the view that draws it (ContentView) and the click-through
    /// decision that has to keep those pixels live. Its center is `inset` from
    /// the content's top-right corner; `hit` is the box that takes the click,
    /// which is a good deal wider than the dot — it is small on purpose, and
    /// the window is dragged by it as well as pressed.
    enum MenuDot {
        static let inset: CGFloat = 20
        static let diameter: CGFloat = 8
        static let hit: CGFloat = 20
    }

    private(set) var isMenuHidden = false

    /// The titlebar the window was wearing when the menu went away: what the
    /// frame gives up on the way out and takes back on the way in.
    private var hiddenTitlebarHeight: CGFloat = 0

    /// The toolbar itself, held for as long as the menu is hidden.
    ///
    /// AppKit takes the toolbar off a window the moment it stops being titled —
    /// `window.toolbar` is nil on the other side of the flip — and SwiftUI does
    /// not put its own back. Without this, what comes back with the titlebar is
    /// an empty strip, and every button on it is gone for the rest of the run.
    private var hiddenToolbar: NSToolbar?

    /// Takes the titlebar off the window — toolbar, title and window buttons
    /// with it — and puts it back.
    ///
    /// The frame gives up exactly the titlebar it was wearing, so the content
    /// stands over the same pixels it did a moment before: the content is what a
    /// screenshot is of and what every click is aimed through, and a macro
    /// recorded before the menu went away still lands where it was aimed.
    ///
    /// The mode belongs to the run rather than to the instance — nothing is
    /// written to instance.json, so the next launch comes up with its toolbar.
    /// A window that opened with nothing on it but a dot would be a window whose
    /// only way back was a dot nobody was told about.
    func setMenuHidden(_ hidden: Bool) {
        guard let window, !AppOptions.fullscreen, hidden != isMenuHidden else { return }
        isMenuHidden = hidden

        if hidden {
            let contentInScreen = window.convertToScreen(window.contentLayoutRect)
            hiddenTitlebarHeight = window.frame.height - contentInScreen.height
            hiddenToolbar = window.toolbar
            // Not resizable: a borderless window has no frame to take hold of,
            // so an edge kept live would be an edge that swallows the clicks
            // meant for the application below and resizes nothing. The lock's
            // half of the mask comes back with the titlebar — the view's
            // updateWindowLock runs again the moment this returns.
            window.styleMask = [.borderless]
            window.setFrame(contentInScreen, display: true)
        } else {
            var frame = window.frame
            frame.size.height += hiddenTitlebarHeight
            window.styleMask = [.titled, .closable, .miniaturizable]
            // A titlebar rebuilt from a styleMask comes back with none of the
            // styling attach(window:) gave the first one — the toolbar least of
            // all, which is why it was kept.
            window.titlebarAppearsTransparent = false
            window.titleVisibility = .hidden
            window.title = "Pob \(AppDelegate.version)"
            window.toolbar = hiddenToolbar
            hiddenToolbar = nil
            window.toolbarStyle = .unifiedCompact
            window.toolbar?.isVisible = true
            // Last: the frame is only right once the titlebar it makes room for
            // is the one that will be there.
            window.setFrame(frame, display: true)
        }

        // The frame moved even though the content did not, and the two are
        // measured against each other everywhere downstream.
        bridge.windowGeometryChanged()
        updateIgnoresMouseEvents()
    }

    /// The dot's hit box in screen coordinates. With the menu hidden the frame
    /// *is* the content, so the box hangs off the frame's own top-right corner.
    private func menuDotRectInScreen(for frame: NSRect) -> NSRect {
        let hit = MenuDot.hit
        return NSRect(x: frame.maxX - MenuDot.inset - hit / 2,
                      y: frame.maxY - MenuDot.inset - hit / 2,
                      width: hit, height: hit)
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
        // Nothing for it to watch for in fullscreen: the decision there is the
        // same wherever the pointer is, so a poll twenty times a second would
        // only be arriving at it again.
        let wanted = clickThroughEnabled && window != nil && !AppOptions.fullscreen
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

    /// Suspends the click-through decision for the length of an automation
    /// burst, and resumes it after. Called by MouseService (main thread) around
    /// every click, drag, scroll and their events.
    ///
    /// Everything below decides from `NSEvent.mouseLocation` — where the user's
    /// real pointer is — which is the right question for a person reaching for
    /// the window and the wrong one for a click Pob is posting somewhere else
    /// entirely. The two ran concurrently: the poll fires every 50 ms, the burst
    /// takes about 150 ms, so a real pointer resting on the toolbar or the
    /// resize edge — where this window must keep its own clicks — reliably took
    /// mouse events back mid-burst and swallowed the press meant for the app
    /// below. Holding the decision is what makes the burst's own window state
    /// hold for as long as the burst.
    func setAutomationPassThrough(_ posting: Bool) {
        automationPassThrough = posting
        if posting {
            guard let window else { return }
            setIgnoresMouseEvents(true, on: window)
        } else {
            updateIgnoresMouseEvents()
        }
    }

    /// Central function — called for every mouseMoved event, on every poll AND
    /// on any state change. When click-through is disabled (targeting /
    /// executing) it ACTIVELY sets ignoresMouseEvents = false on every call, so
    /// no stale monitor callback can re-enable it.
    func updateIgnoresMouseEvents() {
        guard let window else { return }

        // A burst is in flight and aimed at the app below: it owns the window's
        // click-through until it says otherwise. This comes before the
        // clickThroughEnabled branch because that branch actively sets the
        // window live, and an executing session posts these same events.
        guard !automationPassThrough else {
            setIgnoresMouseEvents(true, on: window)
            return
        }

        // Fullscreen passes everything through, always. It has no live areas to
        // keep — no toolbar to press, no edge to resize by, since the frame is
        // the display and stays that way — and it covers every pixel the user
        // has: a window that took a click here would be one nothing could be
        // done about, since there is no button on it to hand the desktop back.
        // Ahead of the check below for exactly that reason, so no saved state
        // and no mode can end in a screen that answers to nobody.
        guard !AppOptions.fullscreen else {
            setIgnoresMouseEvents(true, on: window)
            return
        }

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
        // The chrome that stays live while everything inside it passes clicks
        // through. Ordinarily the top 50 pt, which covers the compact unified
        // toolbar and the traffic-light buttons; with the menu hidden there is
        // no toolbar left to keep, and the dot is the whole of it instead.
        let inChrome: Bool
        if isMenuHidden {
            inChrome = menuDotRectInScreen(for: wf).contains(mouse)
        } else {
            inChrome = mouse.x >= wf.minX && mouse.x <= wf.maxX &&
                mouse.y >= (wf.maxY - 50) && mouse.y <= wf.maxY
        }

        setIgnoresMouseEvents(!(inChrome || onEdge), on: window)
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
    /// The frame is not saved in fullscreen: it is the display rather than
    /// anything the user placed, and writing it down would bring the next
    /// ordinary launch up as a window the size of the screen.
    private func saveWindowFrame() {
        guard let window, !AppOptions.fullscreen else { return }
        var frame = window.frame
        // With the menu hidden the frame is the content and nothing else. What
        // is written down is the frame with its titlebar back on, so the next
        // run — which starts with the toolbar showing — opens the content over
        // the same pixels rather than a titlebar's worth short of them.
        if isMenuHidden { frame.size.height += hiddenTitlebarHeight }
        settings.saveWindowFrame(frame)
    }
}
