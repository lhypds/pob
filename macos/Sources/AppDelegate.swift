import Cocoa
import SwiftUI

class AppDelegate: NSObject, NSApplicationDelegate, NSMenuItemValidation {
    private var globalMouseMonitor: Any?
    private var localMouseMonitor: Any?

    func applicationDidFinishLaunching(_: Notification) {
        NSApplication.shared.setActivationPolicy(.regular)
        NSApplication.shared.activate(ignoringOtherApps: true)

        // One Pob drives a desktop: there is one pointer and one focused
        // window to drive it with, so a second copy would only fight the
        // first for both. `open` reuses a running app, but the dev scripts
        // run the binary directly, which is where a second one comes from.
        if !SettingsService.claimInstance() {
            let alert = NSAlert()
            alert.messageText = "Pob is already running"
            alert.informativeText = "Only one Pob can run at a time — it drives the desktop, and there is one pointer to drive it with. Use the window that is already open."
            alert.alertStyle = .warning
            alert.runModal()
            NSApplication.shared.terminate(nil)
            return
        }

        // The app coming up, written once per process and only past the claim
        // above — the other half of "Pob stopped". It is logged here rather
        // than from the window's onAppear, which SwiftUI can run more than once
        // for one launch (a fullscreen window is restyled as it is attached,
        // and the view comes back with it) and runs even for the copy the claim
        // has just refused.
        AppLogger.event(AppOptions.fullscreen ? "Pob started (fullscreen)" : "Pob started")

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

        // Last, because an ungranted Pob is not a broken Pob — it comes up,
        // draws, logs and serves exactly as it does when granted, and only the
        // events it posts go nowhere. Nothing downstream can report that, so
        // the launch has to ask.
        PermissionsService.checkAtLaunch()
    }

    func applicationWillTerminate(_: Notification) {
        for instance in PobInstance.all {
            instance.shutdown()
        }
        if let m = globalMouseMonitor { NSEvent.removeMonitor(m) }
        if let m = localMouseMonitor { NSEvent.removeMonitor(m) }
        // The other half of "Pob started": app.log is the record of the app
        // coming up and going down, so the way out is written too.
        AppLogger.event("Pob stopped")
    }

    /// The version this copy of Pob was built as.
    ///
    /// macos/build.sh stamps VERSION into the bundle's Info.plist, so the
    /// answer travels inside Pob.app — dragging it to /Applications leaves the
    /// VERSION file behind in the folder it was unzipped from, which is why
    /// reading that file cannot be the primary source. A `swift build` binary
    /// has no bundle around it, so there the dev checkout's VERSION is read.
    static let version: String = {
        if let stamped = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String,
           !stamped.isEmpty
        {
            return stamped
        }

        let executableURL = URL(fileURLWithPath: CommandLine.arguments[0]).resolvingSymlinksInPath()
        // .build/debug/Pob → macos/, one more up is the repository root where
        // VERSION actually lives in dev.
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
    }()

    private func createMenu() {
        let mainMenu = NSMenu()
        let appMenu = NSMenu()

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
    /// open an extra window — there is one instance and one window for it —
    /// so surface the existing window instead.
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

        let versionLabel = NSTextField(labelWithString: "Version \(Self.version)")
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
