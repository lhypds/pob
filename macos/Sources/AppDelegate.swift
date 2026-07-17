import Cocoa
import SwiftUI

class AppDelegate: NSObject, NSApplicationDelegate, NSMenuItemValidation {
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

        // Title is state-dependent (Install/Uninstall) — kept current by
        // validateMenuItem(_:) each time the menu opens.
        let cliMenuItem = NSMenuItem(title: "Install 'pob' Command…", action: #selector(toggleCLIInstall), keyEquivalent: "")
        cliMenuItem.target = self
        appMenu.addItem(cliMenuItem)

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

    /// Dock-icon clicks and "open untitled" events must never let SwiftUI
    /// open an extra window (each window is a full instance here) — surface
    /// the existing windows instead.
    func applicationShouldOpenUntitledFile(_: NSApplication) -> Bool {
        false
    }

    func applicationShouldHandleReopen(_ app: NSApplication, hasVisibleWindows: Bool) -> Bool {
        if !hasVisibleWindows {
            for window in app.windows where window.isMiniaturized {
                window.deminiaturize(nil)
            }
        }
        return false
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

    // MARK: - "pob" command-line tool

    /// Where "Install 'pob' Command…" symlinks the bundled CLI.
    private static let cliLinkPath = "/usr/local/bin/pob"

    private enum CLIToolError: LocalizedError {
        case cancelled
        case failed(String)

        var errorDescription: String? {
            switch self {
            case .cancelled: return "Cancelled."
            case let .failed(message): return message
            }
        }
    }

    /// The pob CLI shipped with the app: Contents/Helpers/pob in the packaged
    /// bundle (Helpers, not MacOS — the case-insensitive filesystem would
    /// collide "pob" with the "Pob" app executable), core/bin/pob in the
    /// repository for dev builds (swift build).
    private func bundledCLIURL() -> URL? {
        let fm = FileManager.default
        let helper = Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/pob")
        if fm.isExecutableFile(atPath: helper.path) {
            return helper
        }
        var dir = URL(fileURLWithPath: CommandLine.arguments[0])
            .resolvingSymlinksInPath()
            .deletingLastPathComponent()
        for _ in 0 ..< 6 {
            let candidate = dir.appendingPathComponent("core/bin/pob")
            if fm.isExecutableFile(atPath: candidate.path) {
                return candidate
            }
            dir = dir.deletingLastPathComponent()
        }
        return nil
    }

    /// Installed means the symlink exists and points at this build's CLI —
    /// a dangling or foreign link reads as "not installed" so the menu
    /// offers Install again as the repair path.
    private func cliIsInstalled(source: URL) -> Bool {
        guard let dest = try? FileManager.default.destinationOfSymbolicLink(atPath: Self.cliLinkPath) else {
            return false
        }
        return dest == source.path
    }

    func validateMenuItem(_ menuItem: NSMenuItem) -> Bool {
        guard menuItem.action == #selector(toggleCLIInstall) else { return true }
        guard let source = bundledCLIURL() else {
            menuItem.title = "Install 'pob' Command…"
            return false
        }
        menuItem.title = cliIsInstalled(source: source)
            ? "Uninstall 'pob' Command"
            : "Install 'pob' Command…"
        return true
    }

    @objc private func toggleCLIInstall() {
        guard let source = bundledCLIURL() else { return }
        do {
            if cliIsInstalled(source: source) {
                try removeCLILink()
                showCLIAlert("The 'pob' command was removed from \(Self.cliLinkPath).")
            } else {
                try installCLILink(source: source)
                showCLIAlert("""
                The 'pob' command is now available in the terminal — try `pob help`.

                \(Self.cliLinkPath) → \(source.path)
                """)
            }
        } catch CLIToolError.cancelled {
            // User dismissed the password prompt.
        } catch {
            showCLIAlert(error.localizedDescription, isError: true)
        }
    }

    private func installCLILink(source: URL) throws {
        let fm = FileManager.default
        let dir = (Self.cliLinkPath as NSString).deletingLastPathComponent
        if fm.isWritableFile(atPath: dir) {
            try? fm.removeItem(atPath: Self.cliLinkPath)
            try fm.createSymbolicLink(atPath: Self.cliLinkPath, withDestinationPath: source.path)
        } else {
            try runPrivileged("mkdir -p '\(dir)' && ln -sfn '\(source.path)' '\(Self.cliLinkPath)'")
        }
    }

    private func removeCLILink() throws {
        let fm = FileManager.default
        if fm.isWritableFile(atPath: (Self.cliLinkPath as NSString).deletingLastPathComponent) {
            try fm.removeItem(atPath: Self.cliLinkPath)
        } else {
            try runPrivileged("rm -f '\(Self.cliLinkPath)'")
        }
    }

    /// Runs a shell command behind macOS's admin-password prompt —
    /// /usr/local/bin is root-owned on most systems.
    private func runPrivileged(_ command: String) throws {
        let escaped = command
            .replacingOccurrences(of: "\\", with: "\\\\")
            .replacingOccurrences(of: "\"", with: "\\\"")
        guard let script = NSAppleScript(source: "do shell script \"\(escaped)\" with administrator privileges") else {
            throw CLIToolError.failed("Could not build the install script.")
        }
        var errorInfo: NSDictionary?
        script.executeAndReturnError(&errorInfo)
        guard let errorInfo else { return }
        if errorInfo[NSAppleScript.errorNumber] as? Int == -128 {
            throw CLIToolError.cancelled
        }
        throw CLIToolError.failed(errorInfo[NSAppleScript.errorMessage] as? String ?? "Unknown error.")
    }

    private func showCLIAlert(_ message: String, isError: Bool = false) {
        let alert = NSAlert()
        alert.messageText = "Pob Command-Line Tool"
        alert.informativeText = message
        alert.alertStyle = isError ? .warning : .informational
        alert.runModal()
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

        let fullNameLabel = NSTextField(labelWithString: "Perception & Operation Bridge")
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
