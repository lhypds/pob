import ApplicationServices
import Cocoa

/// The two macOS privacy grants Pob cannot work without, checked at launch.
///
/// Both fail silently, which is the whole reason this exists. Without
/// Accessibility every event `CGEvent.post` sends is dropped on the way out —
/// and Pob draws its own cursor and walks it to the target itself, so the
/// pointer still moves, the overlay still tracks it, and nothing under it ever
/// hears the click. Without Screen Recording a capture still succeeds and comes
/// back as the desktop picture. Neither raises an error a caller could report,
/// so an ungranted Pob looks like a working Pob that does nothing, and the only
/// place a person can be told is here.
///
/// macOS grants both per app identity, and Pob has two of those: `./start.sh`
/// runs the binary as a child of a terminal, so its events are attributed to
/// the terminal app and borrow whatever that app was granted, while Pob.app
/// carries its own identity. A dev run usually passes this check without anyone
/// having granted Pob anything; the bundle has to be granted on its own.
enum PermissionsService {
    enum Permission {
        case accessibility
        case screenRecording

        var name: String {
            switch self {
            case .accessibility: return "Accessibility"
            case .screenRecording: return "Screen Recording"
            }
        }

        var isGranted: Bool {
            switch self {
            // What CGEvent.post needs.
            case .accessibility: return AXIsProcessTrusted()
            // What CGWindowListCreateImage / CGDisplayCreateImage need.
            case .screenRecording: return CGPreflightScreenCaptureAccess()
            }
        }

        /// The line the alert lists this one under.
        var detail: String {
            switch self {
            case .accessibility:
                return "to move the pointer, click and type. Nothing prompts for this one: open the pane, press +, and add Pob by hand."
            case .screenRecording:
                return "to capture the screen. macOS prompts the first time Pob captures anything."
            }
        }

        /// The System Settings pane that grants it.
        var settingsURL: URL {
            switch self {
            case .accessibility:
                return URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility")!
            case .screenRecording:
                return URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture")!
            }
        }
    }

    static var missing: [Permission] {
        [.accessibility, .screenRecording].filter { !$0.isGranted }
    }

    /// Asked once per launch, from `applicationDidFinishLaunching`.
    static func checkAtLaunch() {
        let missing = self.missing
        guard !missing.isEmpty else { return }

        AppLogger.error("Permissions not granted: \(missing.map(\.name).joined(separator: ", ")) — "
            + "clicks and keystrokes are dropped without Accessibility, screenshots come back blank without Screen Recording")

        // After the window: an alert with nothing behind it reads as a crash
        // report rather than as something this app is asking for.
        DispatchQueue.main.async { showAlert(missing: missing) }
    }

    // MARK: - The alert

    private static func showAlert(missing: [Permission]) {
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = missing.count == 1
            ? "Pob needs \(missing[0].name) access"
            : "Pob needs Accessibility and Screen Recording access"

        var text = "macOS is not granting it to this copy of Pob, and neither one fails loudly — "
            + "an ungranted click is dropped on its way out, and an ungranted capture comes back as the desktop picture.\n"
        for permission in missing {
            text += "\n• \(permission.name) — \(permission.detail)"
        }
        // The way out of a grant macOS is holding against a copy of Pob that no
        // longer exists. Only worth offering for the bundle: a dev binary has no
        // identity of its own to reset, it borrows the terminal's.
        let bundleID = Bundle.main.bundleIdentifier
        if let bundleID {
            text += "\n\nAlready switched on in the list? Then macOS is holding the grant against an older copy — "
                + "Pob is not signed with a Developer ID, so a rebuilt or upgraded app is a stranger to a grant "
                + "whose switch still reads as on. Resetting clears both (tccutil reset All \(bundleID)) and quits Pob; "
                + "grant them again to the copy you reopen."
        }
        alert.informativeText = text

        // Built as a pair so the button that was clicked maps to its action by
        // position — the reset button is not always there to count on.
        var actions: [() -> Void] = []

        alert.addButton(withTitle: "Open System Settings")
        actions.append { openSettings(for: missing) }

        if let bundleID {
            alert.addButton(withTitle: "Reset Permissions and Quit")
            actions.append { resetAndQuit(bundleID: bundleID) }
        }

        alert.addButton(withTitle: "Continue Anyway")
        actions.append {
            AppLogger.error("Permissions: continuing without \(missing.map(\.name).joined(separator: ", "))")
        }

        let clicked = alert.runModal().rawValue - NSApplication.ModalResponse.alertFirstButtonReturn.rawValue
        if actions.indices.contains(clicked) { actions[clicked]() }
    }

    private static func openSettings(for missing: [Permission]) {
        // Asking first matters for Screen Recording: an app that has never
        // asked has no row in that pane to switch on. Accessibility takes any
        // app added with +, so it needs nothing beforehand.
        if missing.contains(where: { if case .screenRecording = $0 { return true } else { return false } }) {
            _ = CGRequestScreenCaptureAccess()
        }
        // One destination, opened last so it is the window in front.
        NSWorkspace.shared.open(missing[0].settingsURL)
    }

    // MARK: - Reset

    private static func resetAndQuit(bundleID: String) {
        let reset = runReset(bundleID: bundleID)

        let alert = NSAlert()
        alert.alertStyle = reset ? .informational : .warning
        alert.messageText = reset ? "Permissions reset" : "Could not reset the permissions"
        alert.informativeText = reset
            ? "macOS is no longer holding a grant for \(bundleID). Reopen Pob, add it under Accessibility, "
            + "and allow Screen Recording when it asks."
            : "Run this in a terminal instead, then reopen Pob:\n\ntccutil reset All \(bundleID)"
        alert.addButton(withTitle: "Quit Pob")
        alert.runModal()

        NSApplication.shared.terminate(nil)
    }

    /// `tccutil reset All <bundle id>` — every service at once, so the two Pob
    /// needs are cleared without naming them (`Accessibility` and
    /// `ScreenCapture`, if they ever need naming separately).
    ///
    /// tccutil exits 0 whether or not it found anything to clear, so a true
    /// return says the command ran, not that a grant was there to remove. The
    /// check that it worked is Screen Recording prompting again.
    private static func runReset(bundleID: String) -> Bool {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/tccutil")
        process.arguments = ["reset", "All", bundleID]
        do {
            try process.run()
            process.waitUntilExit()
        } catch {
            AppLogger.error("Permissions: could not run tccutil reset All \(bundleID) — \(error.localizedDescription)")
            return false
        }
        guard process.terminationStatus == 0 else {
            AppLogger.error("Permissions: tccutil reset All \(bundleID) exited \(process.terminationStatus)")
            return false
        }
        AppLogger.event("Permissions reset (tccutil reset All \(bundleID))")
        return true
    }
}
