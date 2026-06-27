import Cocoa
import SwiftUI

class AppDelegate: NSObject, NSApplicationDelegate {
    weak static var shared: AppDelegate?

    var window: NSWindow?
    private var globalMouseMonitor: Any?
    private var localMouseMonitor: Any?
    private var clickThroughEnabled = false

    override init() {
        super.init()
        AppDelegate.shared = self
    }

    /// Called by ContentView whenever isExecuting or isTargeting changes.
    func setClickThrough(_ enabled: Bool) {
        clickThroughEnabled = enabled
        updateIgnoresMouseEvents()
    }

    /// Central function — always called for every mouseMoved event AND on any state change.
    /// When click-through is disabled (targeting / executing) it ACTIVELY sets ignoresMouseEvents = false
    /// on every call, so no stale monitor callback can re-enable it.
    private func updateIgnoresMouseEvents() {
        // Always use the live first window; avoids stale reference after SwiftUI window recreation.
        guard let window = NSApplication.shared.windows.first else { return }

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

    func applicationDidFinishLaunching(_: Notification) {
        NSApplication.shared.setActivationPolicy(.regular)

        if let window = NSApplication.shared.windows.first {
            self.window = window

            window.isOpaque = false
            window.backgroundColor = NSColor.clear
            window.titlebarAppearsTransparent = false
            window.titleVisibility = .hidden
            window.title = "Pob \(loadVersion())"
            window.toolbarStyle = .unifiedCompact

            window.styleMask.insert(.resizable)
            window.styleMask.insert(.miniaturizable)
            window.styleMask.insert(.closable)

            window.level = .floating
            window.ignoresMouseEvents = false

            if let savedFrame = SettingsService.shared.getWindowFrame() {
                window.setFrame(savedFrame, display: true)
            } else {
                window.setFrame(NSRect(x: 100, y: 100, width: 600, height: 400), display: true)
                window.center()
            }

            window.delegate = self
            window.makeKeyAndOrderFront(nil)
            NSApplication.shared.activate(ignoringOtherApps: true)

            window.standardWindowButton(.closeButton)?.isEnabled = true
            window.standardWindowButton(.miniaturizeButton)?.isEnabled = true
            window.standardWindowButton(.zoomButton)?.isEnabled = true
        }

        createMenu()

        // Monitors run for the full app lifetime — no start/stop needed.
        // updateIgnoresMouseEvents handles both click-through and non-click-through cases.
        globalMouseMonitor = NSEvent.addGlobalMonitorForEvents(matching: .mouseMoved) { [weak self] _ in
            self?.updateIgnoresMouseEvents()
        }
        localMouseMonitor = NSEvent.addLocalMonitorForEvents(matching: .mouseMoved) { [weak self] event in
            self?.updateIgnoresMouseEvents()
            return event
        }

        updateIgnoresMouseEvents()

        MCPServer.shared.start(port: SettingsService.shared.getMCPPort())
    }

    func applicationWillTerminate(_: Notification) {
        if let m = globalMouseMonitor { NSEvent.removeMonitor(m) }
        if let m = localMouseMonitor { NSEvent.removeMonitor(m) }
    }

    private func loadVersion() -> String {
        let fallback = "0.0.0"

        let fileManager = FileManager.default
        let currentDirectoryPath = fileManager.currentDirectoryPath
        let currentDirectoryVersionPath = URL(fileURLWithPath: currentDirectoryPath).appendingPathComponent("VERSION")

        if let value = try? String(contentsOf: currentDirectoryVersionPath, encoding: .utf8)
            .trimmingCharacters(in: .whitespacesAndNewlines),
            !value.isEmpty
        {
            return value
        }

        let executableURL = URL(fileURLWithPath: CommandLine.arguments[0]).resolvingSymlinksInPath()
        let repositoryRootFromBinary = executableURL
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
        let executableRelativeVersionPath = repositoryRootFromBinary.appendingPathComponent("VERSION")

        if let value = try? String(contentsOf: executableRelativeVersionPath, encoding: .utf8)
            .trimmingCharacters(in: .whitespacesAndNewlines),
            !value.isEmpty
        {
            return value
        }

        return fallback
    }

    private func saveWindowFrame() {
        guard let window = window else { return }
        SettingsService.shared.saveWindowFrame(window.frame)
    }

    private func createMenu() {
        let mainMenu = NSMenu()
        let appMenu = NSMenu()

        let aboutMenuItem = NSMenuItem(title: "About Pob", action: #selector(showAbout), keyEquivalent: "")
        aboutMenuItem.target = self
        appMenu.addItem(aboutMenuItem)

        appMenu.addItem(.separator())

        let quitMenuItem = NSMenuItem(title: "Quit Pob", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appMenu.addItem(quitMenuItem)

        let appMenuItem = NSMenuItem(title: "Pob", action: nil, keyEquivalent: "")
        appMenuItem.submenu = appMenu
        mainMenu.addItem(appMenuItem)

        NSApplication.shared.mainMenu = mainMenu
    }

    @objc private func showAbout() {
        let panel = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: 280, height: 130),
            styleMask: [.titled, .closable],
            backing: .buffered,
            defer: false
        )
        panel.title = ""
        panel.isFloatingPanel = true

        let container = NSView(frame: NSRect(x: 0, y: 0, width: 280, height: 130))

        let nameLabel = NSTextField(labelWithString: "Pob")
        nameLabel.font = NSFont.boldSystemFont(ofSize: 16)
        nameLabel.frame = NSRect(x: 20, y: 82, width: 240, height: 22)
        container.addSubview(nameLabel)

        let fullNameLabel = NSTextField(labelWithString: "Perception and Operation Bridge")
        fullNameLabel.font = NSFont.systemFont(ofSize: 12)
        fullNameLabel.textColor = .secondaryLabelColor
        fullNameLabel.frame = NSRect(x: 20, y: 60, width: 240, height: 18)
        container.addSubview(fullNameLabel)

        let versionLabel = NSTextField(labelWithString: "Version \(loadVersion())")
        versionLabel.font = NSFont.systemFont(ofSize: 13)
        versionLabel.textColor = .secondaryLabelColor
        versionLabel.frame = NSRect(x: 20, y: 38, width: 240, height: 18)
        container.addSubview(versionLabel)

        let okButton = NSButton(title: "OK", target: panel, action: #selector(NSWindow.close))
        okButton.bezelStyle = .rounded
        okButton.keyEquivalent = "\r"
        okButton.frame = NSRect(x: 200, y: 10, width: 60, height: 22)
        container.addSubview(okButton)

        panel.contentView = container
        panel.center()
        panel.makeKeyAndOrderFront(nil)
    }
}

extension AppDelegate: NSWindowDelegate {
    func windowDidMove(_: Notification) {
        saveWindowFrame()
    }

    func windowDidEndLiveResize(_: Notification) {
        saveWindowFrame()
    }
}
