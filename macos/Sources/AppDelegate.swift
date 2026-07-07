import Cocoa
import SwiftUI

class AppDelegate: NSObject, NSApplicationDelegate {
    /// Set by ContentView with the SwiftUI openWindow action, so the AppKit
    /// menu can open a new WindowGroup window (a new instance) in-process —
    /// the VSCode model: one app, one window per instance.
    static var openNewInstanceWindow: (() -> Void)?

    private var globalMouseMonitor: Any?
    private var localMouseMonitor: Any?

    func applicationDidFinishLaunching(_: Notification) {
        NSApplication.shared.setActivationPolicy(.regular)
        NSApplication.shared.activate(ignoringOtherApps: true)

        createMenu()

        // Monitors run for the full app lifetime — no start/stop needed.
        // Each instance handles its own click-through / non-click-through case.
        globalMouseMonitor = NSEvent.addGlobalMonitorForEvents(matching: .mouseMoved) { _ in
            PobInstance.updateAllIgnoresMouseEvents()
        }
        localMouseMonitor = NSEvent.addLocalMonitorForEvents(matching: .mouseMoved) { event in
            PobInstance.updateAllIgnoresMouseEvents()
            return event
        }
    }

    func applicationWillTerminate(_: Notification) {
        for instance in PobInstance.all {
            instance.shutdown()
        }
        if let m = globalMouseMonitor { NSEvent.removeMonitor(m) }
        if let m = localMouseMonitor { NSEvent.removeMonitor(m) }
    }

    static func loadVersion() -> String {
        let executableURL = URL(fileURLWithPath: CommandLine.arguments[0]).resolvingSymlinksInPath()
        // .build/debug/Pob → macos/ (packaged: next to the bundle), one more
        // up is the repository root where VERSION actually lives in dev.
        let binaryParent3 = executableURL
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()

        let candidates = [
            URL(fileURLWithPath: FileManager.default.currentDirectoryPath).appendingPathComponent("VERSION"),
            binaryParent3.appendingPathComponent("VERSION"),
            binaryParent3.deletingLastPathComponent().appendingPathComponent("VERSION"),
        ]

        for candidate in candidates {
            if let value = try? String(contentsOf: candidate, encoding: .utf8)
                .trimmingCharacters(in: .whitespacesAndNewlines),
                !value.isEmpty
            {
                return value
            }
        }

        return "0.0.0"
    }

    private func createMenu() {
        let mainMenu = NSMenu()
        let appMenu = NSMenu()

        let newInstanceMenuItem = NSMenuItem(title: "New Instance", action: #selector(newInstance), keyEquivalent: "n")
        newInstanceMenuItem.target = self
        appMenu.addItem(newInstanceMenuItem)

        appMenu.addItem(.separator())

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

    func applicationShouldTerminateAfterLastWindowClosed(_: NSApplication) -> Bool {
        true
    }

    /// Right-click menu on the Dock icon.
    func applicationDockMenu(_: NSApplication) -> NSMenu? {
        let menu = NSMenu()
        let newInstanceItem = NSMenuItem(title: "New Instance", action: #selector(newInstance), keyEquivalent: "")
        newInstanceItem.target = self
        menu.addItem(newInstanceItem)
        return menu
    }

    /// Opens a new window in this process; its ContentView creates a fresh
    /// PobInstance with its own logs/<instance>/ directory, settings copy and
    /// pob-core child.
    @objc private func newInstance() {
        AppDelegate.openNewInstanceWindow?()
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

        let versionLabel = NSTextField(labelWithString: "Version \(Self.loadVersion())")
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
