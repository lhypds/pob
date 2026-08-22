import AppKit
import ApplicationServices

/// Opens an application and puts its window in the frame — what a macro's
/// `launch("Firefox")` asks for.
///
/// The overlay is a hole punched over somebody else's desktop, and every
/// position a macro holds is a position inside that hole. Which means every one
/// of them was written down while some window sat in a particular place under
/// the frame, put there by a person, once. A macro that opens the window itself
/// and places it where the frame is does not need that person again — and a
/// macro that does not need that person is one a schedule can run.
///
/// So this is two jobs, and they are one call because the second needs the
/// first. Opening is Launch Services'; placing is the same Accessibility door
/// the mouse and keyboard already go through, and the same one CarryService
/// carries a window through on every drag. What ties them together is the
/// process: the window to place is a window of the application that was just
/// opened, and nothing but the side that opened it knows which that is.
///
/// The Windows and X11 halves of this live in win/src/Services/LaunchService.cs
/// and linux-x11/src/launch_service.c.
final class LaunchService {
    static let shared = LaunchService()

    private init() {}

    /// Why there was nothing to open at all — the sentence the core fails the
    /// statement with, and the one thing here that is not an `Opened`.
    struct Refusal: Error {
        let message: String
    }

    /// What came of a launch, as the core is told it.
    struct Opened {
        /// The application as this machine knows it — the bundle's name, which
        /// is not always the name it was asked for.
        let app: String
        let pid: pid_t
        /// Whether the window is now the content area.
        let fitted: Bool
        /// What is worth saying about the fit beyond whether it happened: why
        /// there was no window to place, or how a window that was placed fell
        /// short of what it was asked for. Empty when the fit was exact.
        let note: String
    }

    /// How long a launch waits for the application to put a window on screen.
    ///
    /// An application that is already running answers on the first poll, and a
    /// cold start of something large — a browser, an office suite, an IDE — is
    /// seconds rather than tenths of them on a machine that has other things to
    /// do. Twenty is past all of that and still short of a macro looking hung:
    /// what usually reaches it is an application that opened no window at all,
    /// which is a thing to be told about rather than waited on.
    private static let windowWait: TimeInterval = 20

    /// How often the wait looks. Each look is a cross-process Accessibility
    /// round trip, so it is not free — and a window that has just appeared is
    /// worth finding within a frame or two of appearing, since the statement
    /// under this one is about to click into it.
    private static let pollInterval: TimeInterval = 0.2

    /// Accessibility calls block the caller until the other application
    /// answers, and an application in the middle of starting up is exactly the
    /// one that will not answer promptly. Longer than CarryService's quarter of
    /// a second — nothing here runs inside a drag — and short enough that a
    /// process which never answers still costs the wait rather than the run.
    private static let messagingTimeout: Float = 0.5

    /// Windows smaller than this on either side are not what was opened — the
    /// 1×1 markers and off-screen scratch windows an application puts up while
    /// it starts, which appear before the real one and would otherwise be what
    /// the frame got.
    private static let minimumWindowSize: CGFloat = 40

    /// How far the window may end up from what it was asked for and still count
    /// as fitted. A window server rounds to the pixel and a scaled display
    /// rounds twice; two points is under anything a person could see and over
    /// everything rounding can do.
    private static let fitTolerance: CGFloat = 2

    /// Where an application is looked for when it was named rather than
    /// pointed at. The user's own folder last: the ones under `/` are what
    /// `launch("Safari")` means on every Mac, and a copy in `~/Applications` is
    /// the deliberate one, so it is the tie-breaker rather than the default.
    private static let searchRoots = ["/Applications", "/System/Applications", "~/Applications"]

    /// How far into those folders the search goes. Applications are kept one
    /// level down as often as not — `/Applications/Utilities/Terminal.app`,
    /// `/Applications/Adobe Photoshop 2024/Photoshop.app` — and past that a
    /// launch would be walking a disk rather than finding an application.
    private static let searchDepth = 2

    // MARK: - Launching

    /// Opens `target` and fits its window to `window`'s content area, calling
    /// back with what came of it — or with the sentence to fail the statement
    /// with, when there was nothing to open.
    ///
    /// Called on the main thread, and answers on an arbitrary one. Everything
    /// between is either Launch Services' own callback or the wait, and the
    /// wait is on a queue of its own: it is up to twenty seconds long and made
    /// of cross-process calls that block, neither of which belongs on the
    /// thread drawing the overlay. The one thing it hops back to main for is
    /// reading the frame's geometry, which only the main thread may do.
    func launch(target: String,
                gap: Int,
                fittingTo window: NSWindow?,
                completion: @escaping (Result<Opened, Refusal>) -> Void)
    {
        let wanted = target.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !wanted.isEmpty else {
            return completion(.failure(Refusal(message: "launch was given no application to open")))
        }
        guard let url = resolve(wanted) else {
            return completion(.failure(Refusal(message: "There is no application called \(wanted) on this machine — a launch names an application, a bundle id, or the path to a .app")))
        }
        guard AXIsProcessTrusted() else {
            return completion(.failure(Refusal(message: "Pob has no Accessibility permission, so it cannot place \(wanted)'s window — System Settings ▸ Privacy & Security ▸ Accessibility")))
        }

        let config = NSWorkspace.OpenConfiguration()
        // An application already running is brought forward rather than opened
        // a second time — createsNewApplicationInstance is false by default,
        // and the running instance is what comes back. That is the useful
        // reading of the statement: a macro replayed twice should find one
        // browser in the frame, not two.
        config.activates = true

        NSWorkspace.shared.openApplication(at: url, configuration: config) { [weak self] running, error in
            guard let self else { return }
            if let error {
                return completion(.failure(Refusal(message: "\(wanted) would not open: \(error.localizedDescription)")))
            }
            guard let running else {
                return completion(.failure(Refusal(message: "\(wanted) would not open")))
            }
            let name = running.localizedName ?? url.deletingPathExtension().lastPathComponent
            let pid = running.processIdentifier

            // qos .userInitiated: a statement is waiting on this, and the
            // statement is what the person pressing Execute is waiting on.
            DispatchQueue.global(qos: .userInitiated).async {
                let (fitted, note) = self.waitAndFit(pid: pid, name: name, window: window, gap: gap)
                completion(.success(Opened(app: name, pid: pid, fitted: fitted, note: note)))
            }
        }
    }

    /// Waits for the application to put a placeable window on screen and fits
    /// the first one it does. Off the main thread.
    private func waitAndFit(pid: pid_t, name: String, window: NSWindow?, gap: Int) -> (Bool, String) {
        let deadline = Date().addingTimeInterval(Self.windowWait)
        let app = AXUIElementCreateApplication(pid)
        AXUIElementSetMessagingTimeout(app, Self.messagingTimeout)
        var roused = false

        while true {
            if let element = placeableWindow(of: app, rousing: &roused) {
                guard let rect = contentRect(of: window, gap: gap) else {
                    return (false, "Pob's own window is not on screen to fit it to")
                }
                return fit(element, to: rect)
            }
            if Date() >= deadline {
                return (false, "\(name) put no window on screen within \(Int(Self.windowWait)) seconds")
            }
            Thread.sleep(forTimeInterval: Self.pollInterval)
        }
    }

    /// The frame's content area with the launch gap taken off it — the rect the
    /// window is actually put in.
    ///
    /// Read now rather than when the launch started: a cold start is seconds
    /// long, and the frame is a window somebody can pick up and move in that
    /// time. What the window is fitted to is where the frame is when it is
    /// fitted.
    private func contentRect(of window: NSWindow?, gap: Int) -> CGRect? {
        DispatchQueue.main.sync {
            guard let window,
                  let rect = ScreenshotService.shared.contentRectInCGCoordinates(of: window)
            else { return nil }
            let scale = (window.screen ?? NSScreen.main)?.backingScaleFactor ?? 1
            return Self.inset(rect, byPixels: gap, scale: scale)
        }
    }

    /// The content area less the gap on every side.
    ///
    /// The gap arrives in screenshot pixels — the space every coordinate in a
    /// macro is in — and Accessibility places windows in points, which on a
    /// Retina display is half as many of them. So it is divided down here
    /// rather than being a different distance on every Mac.
    ///
    /// A gap is daylight around a window and not a reason to have no window: a
    /// frame too small to hold one with the whole margin on gets the margin it
    /// can afford, which keeps at least half of the frame in each direction for
    /// the window itself.
    private static func inset(_ rect: CGRect, byPixels gap: Int, scale: CGFloat) -> CGRect {
        guard gap > 0, scale > 0 else { return rect }
        let affordable = max(0, min(rect.width, rect.height) / 4)
        return rect.insetBy(dx: min(CGFloat(gap) / scale, affordable),
                            dy: min(CGFloat(gap) / scale, affordable))
    }

    // MARK: - Finding the window

    /// The application's first window that is worth putting in the frame and
    /// can be put there, or nil while it has none.
    ///
    /// A window that will not take a position is passed over rather than ending
    /// the search: an application with a splash screen it refuses to let anyone
    /// move is an application whose real window is one poll away.
    ///
    /// `rousing` is whether a window has already been asked to come out of full
    /// screen or out of the Dock during this launch. Both are places the window
    /// server puts a window rather than sizes it happens to be, both refuse
    /// every attempt to place the window while they last, and both are undone
    /// by an animation — so the window is asked once, passed over for now, and
    /// found on a later poll once it has become an ordinary window again.
    private func placeableWindow(of app: AXUIElement, rousing roused: inout Bool) -> AXUIElement? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(app, kAXWindowsAttribute as CFString, &value) == .success,
              let windows = value as? [AXUIElement] else { return nil }

        for element in windows {
            let stowed = axBool(element, kAXMinimizedAttribute) == true
            let full = axBool(element, "AXFullScreen") == true
            if stowed || full {
                if !roused {
                    roused = true
                    if stowed { setBool(element, kAXMinimizedAttribute, false) }
                    if full { setBool(element, "AXFullScreen", false) }
                }
                continue
            }
            guard let size = axSize(element, kAXSizeAttribute),
                  size.width >= Self.minimumWindowSize,
                  size.height >= Self.minimumWindowSize else { continue }
            guard settable(element, kAXPositionAttribute) else { continue }
            return element
        }
        return nil
    }

    // MARK: - Fitting

    /// Puts the window where the frame is and makes it the size the frame is,
    /// and says how close it got.
    private func fit(_ element: AXUIElement, to rect: CGRect) -> (Bool, String) {
        // Position, then size, then position again. An application that clamps
        // the size it was given usually moves the window doing it, and a window
        // the right size in the wrong place is not in the frame — whereas a
        // window in the right place at the wrong size still has the frame's
        // top-left corner, which is what the coordinates under the statement
        // are measured from.
        setPoint(element, kAXPositionAttribute, rect.origin)
        setSize(element, kAXSizeAttribute, rect.size)
        setPoint(element, kAXPositionAttribute, rect.origin)

        guard let origin = axPoint(element, kAXPositionAttribute),
              let size = axSize(element, kAXSizeAttribute) else {
            return (false, "its window would not say where it ended up")
        }
        let placed = abs(origin.x - rect.minX) <= Self.fitTolerance &&
                     abs(origin.y - rect.minY) <= Self.fitTolerance
        if !placed {
            return (false, "its window would not move to the frame")
        }
        let sized = abs(size.width - rect.width) <= Self.fitTolerance &&
                    abs(size.height - rect.height) <= Self.fitTolerance
        if !sized {
            return (true, String(format: "its window would not resize past %.0f×%.0f", size.width, size.height))
        }
        return (true, "")
    }

    // MARK: - Resolving a name

    /// The application `target` names: a path, a bundle id, or a name to go
    /// looking for.
    ///
    /// The three are asked in that order because that is the order they are
    /// unambiguous in. A path is what it says; a bundle id is what Launch
    /// Services says; a name is a guess, and the guess is made last.
    private func resolve(_ target: String) -> URL? {
        let expanded = (target as NSString).expandingTildeInPath
        if expanded.hasPrefix("/") {
            let url = URL(fileURLWithPath: expanded)
            return FileManager.default.fileExists(atPath: url.path) ? url : nil
        }
        // A dot is what a bundle id has and an application's name has not —
        // except for the `.app` on the end of one, which is the other thing a
        // dot means here.
        if expanded.contains("."), !expanded.hasSuffix(".app"),
           let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: expanded) {
            return url
        }
        return find(named: expanded.hasSuffix(".app") ? expanded : expanded + ".app")
    }

    /// The bundle called `name` in the places applications are kept.
    ///
    /// Case-insensitively, because the name in the statement is the name a
    /// person says — `launch("firefox")` is the same wish as
    /// `launch("Firefox")`, and only one of the two is what the bundle on disk
    /// is actually called.
    private func find(named name: String) -> URL? {
        let fm = FileManager.default
        for root in Self.searchRoots {
            let base = URL(fileURLWithPath: (root as NSString).expandingTildeInPath)
            if let found = search(base, for: name, depth: Self.searchDepth, fm: fm) { return found }
        }
        return nil
    }

    private func search(_ directory: URL, for name: String, depth: Int, fm: FileManager) -> URL? {
        guard depth > 0,
              let entries = try? fm.contentsOfDirectory(at: directory,
                                                        includingPropertiesForKeys: nil,
                                                        options: [.skipsHiddenFiles])
        else { return nil }

        var folders: [URL] = []
        for entry in entries {
            if entry.pathExtension == "app" {
                if entry.lastPathComponent.caseInsensitiveCompare(name) == .orderedSame { return entry }
                continue
            }
            folders.append(entry)
        }
        // Bundles first, whole level at a time: `/Applications/Safari.app` is
        // found without opening `/Applications/Utilities` on the way past.
        for folder in folders {
            var isDirectory: ObjCBool = false
            guard fm.fileExists(atPath: folder.path, isDirectory: &isDirectory), isDirectory.boolValue else { continue }
            if let found = search(folder, for: name, depth: depth - 1, fm: fm) { return found }
        }
        return nil
    }

    // MARK: - Accessibility values

    private func axValue(_ element: AXUIElement, _ attribute: String) -> AXValue? {
        var ref: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, attribute as CFString, &ref) == .success,
              let result = ref, CFGetTypeID(result) == AXValueGetTypeID() else { return nil }
        return (result as! AXValue)
    }

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

    private func axBool(_ element: AXUIElement, _ attribute: String) -> Bool? {
        var ref: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, attribute as CFString, &ref) == .success else { return nil }
        return ref as? Bool
    }

    /// Whether the attribute will take a new value at all. Full-screen windows,
    /// and windows of applications that expose them without letting them be
    /// placed, report one and refuse to be given one.
    private func settable(_ element: AXUIElement, _ attribute: String) -> Bool {
        var yes: DarwinBoolean = false
        guard AXUIElementIsAttributeSettable(element, attribute as CFString, &yes) == .success else { return false }
        return yes.boolValue
    }

    private func setPoint(_ element: AXUIElement, _ attribute: String, _ point: CGPoint) {
        var target = point
        guard let value = AXValueCreate(.cgPoint, &target) else { return }
        AXUIElementSetAttributeValue(element, attribute as CFString, value)
    }

    private func setSize(_ element: AXUIElement, _ attribute: String, _ size: CGSize) {
        var target = size
        guard let value = AXValueCreate(.cgSize, &target) else { return }
        AXUIElementSetAttributeValue(element, attribute as CFString, value)
    }

    private func setBool(_ element: AXUIElement, _ attribute: String, _ on: Bool) {
        AXUIElementSetAttributeValue(element, attribute as CFString, on as CFBoolean)
    }
}
