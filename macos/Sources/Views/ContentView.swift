import AppKit
import SwiftUI

/// Root view of the window. Owns the PobInstance for the window's lifetime
/// and attaches it to the hosting NSWindow once that exists.
struct ContentView: View {
    @StateObject private var instance = PobInstance()

    var body: some View {
        InstanceContentView(instance: instance, bridge: instance.bridge, mouseService: instance.mouse)
            .background(WindowAccessor { window in
                instance.attach(window: window)
            })
    }
}

/// Grabs the NSWindow hosting this SwiftUI hierarchy once it exists.
private struct WindowAccessor: NSViewRepresentable {
    let onWindow: (NSWindow) -> Void

    func makeNSView(context _: Context) -> NSView {
        let view = NSView()
        DispatchQueue.main.async {
            if let window = view.window { onWindow(window) }
        }
        return view
    }

    func updateNSView(_ view: NSView, context _: Context) {
        DispatchQueue.main.async {
            if let window = view.window { onWindow(window) }
        }
    }
}

struct InstanceContentView: View {
    let instance: PobInstance
    @ObservedObject var bridge: CoreBridge
    @ObservedObject var mouseService: MouseService

    @State private var isTargeting = false
    @State private var isCropping = false
    @State private var cropStart: CGPoint? = nil
    @State private var cropCurrent: CGPoint? = nil
    @State private var isClickThrough = true
    @State private var isLocked = false
    @State private var isRecording = false
    @State private var showRecordWarning = false
    @State private var mousePosition: CGPoint? = nil
    @State private var animatedCursorPos: CGPoint = .init(x: 20, y: 20)
    @State private var screenshotFlashOpacity: Double = 0
    @State private var toastMessage: String? = nil
    @State private var toastToken = 0
    @Environment(\.controlActiveState) private var controlActiveState

    var body: some View {
        ZStack {
            Color.gray.opacity(0.2)

            if isTargeting {
                MouseTrackingOverlay(
                    onPositionChange: { pos in mousePosition = pos },
                    onTap: { pt in
                        let scale = instance.window?.screen?.backingScaleFactor ?? NSScreen.main?.backingScaleFactor ?? 2.0
                        let text = "\(Int(pt.x * scale)), \(Int(pt.y * scale))"
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(text, forType: .string)
                        showToast("Copied \(text)")
                        isTargeting = false
                        mousePosition = nil
                    }
                )

                if let pos = mousePosition {
                    positionLabel(at: pos)
                }
            }

            if isCropping {
                CropTrackingOverlay(
                    onDragChange: { start, current in
                        cropStart = start
                        cropCurrent = current
                    },
                    onDragEnd: { rect in
                        let scale = instance.window?.screen?.backingScaleFactor ?? NSScreen.main?.backingScaleFactor ?? 2.0
                        let text = "\(Int(rect.minX * scale)), \(Int(rect.minY * scale)), \(Int(rect.width * scale)), \(Int(rect.height * scale))"
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(text, forType: .string)
                        showToast("Copied \(text)")
                        isCropping = false
                        cropStart = nil
                        cropCurrent = nil
                    }
                )

                if let start = cropStart, let current = cropCurrent {
                    cropSelectionView(start: start, current: current)
                }
            }

            // The virtual cursor is on screen from launch, parked at its home
            // corner: it is what every screenshot shows and what all the clicks
            // are aimed with, so where it sits is worth seeing before anything
            // starts driving it.
            let scale = NSScreen.main?.backingScaleFactor ?? 2.0
            let cursorImg = NSCursor.arrow.image
            let hot = NSCursor.arrow.hotSpot
            let viewX = animatedCursorPos.x / scale
            let viewY = animatedCursorPos.y / scale
            Image(nsImage: cursorImg)
                .resizable()
                .frame(width: cursorImg.size.width, height: cursorImg.size.height)
                .position(
                    x: viewX + cursorImg.size.width / 2 - hot.x,
                    y: viewY + cursorImg.size.height / 2 - hot.y
                )
                .allowsHitTesting(false)

            Color.white
                .opacity(screenshotFlashOpacity)
                .allowsHitTesting(false)

            if let toast = toastMessage {
                Text(toast)
                    .font(.system(size: 11, weight: .medium, design: .monospaced))
                    .foregroundColor(.white)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(Color.black.opacity(0.75))
                    .cornerRadius(6)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottom)
                    .padding(.bottom, 10)
                    .allowsHitTesting(false)
                    .transition(.opacity)
            }
        }
        .onChange(of: mouseService.displayPosition) { newPos in
            withAnimation(.easeOut(duration: 0.1)) {
                animatedCursorPos = newPos
            }
        }
        .onChange(of: bridge.isMCPDriving) { driving in
            // Start the overlay where the cursor actually is, so it does not
            // appear at a stale spot until the client's first move.
            if driving { animatedCursorPos = mouseService.virtualCursorPosition }
        }
        .onChange(of: bridge.isExecuting) { executing in
            if executing {
                animatedCursorPos = CGPoint(x: 20, y: 20)
                // During execution the Go core records the AI's actions itself.
                instance.recorder.pauseForExecution()
            } else if isRecording {
                // Resume user recording where the AI's cursor ended up.
                instance.recorder.start(from: mouseService.virtualCursorPosition)
            }
            updateWindowLock()
            updateClickThrough()
        }
        .onChange(of: bridge.flashTick) { _ in
            flashScreenshot()
        }
        .onChange(of: isLocked) { locked in
            updateWindowLock()
            instance.settings.saveWindowLocked(locked)
        }
        .onChange(of: isClickThrough) { enabled in
            updateClickThrough()
            instance.settings.saveClickThrough(enabled)
        }
        .onChange(of: isTargeting) { _ in
            updateClickThrough()
        }
        .onChange(of: isCropping) { _ in
            updateClickThrough()
        }
        .onAppear {
            AppLogger.event("Pob started")
            // The lock and the click-through the window was left with, so an
            // instance set up for a macro comes back the way it was rather than
            // loose and catching clicks again.
            isLocked = instance.settings.getWindowLocked()
            isClickThrough = instance.settings.getClickThrough()
            updateClickThrough()
            updateWindowLock()
        }
        .toolbar { toolbarContent }
        .onTapGesture {
            NSApplication.shared.activate(ignoringOtherApps: true)
            instance.window?.makeKeyAndOrderFront(nil)
        }
        .alert(bridge.coreAlertTitle, isPresented: $bridge.showCoreAlert) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(bridge.coreAlertMessage)
        }
        .alert("Warning", isPresented: $showRecordWarning) {
            Button("Clear") { startRecording(clearingMacro: true) }
            Button("Keep") { startRecording(clearingMacro: false) }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("main.macro.psl has recorded actions. Clear them before recording?")
        }
    }

    @ViewBuilder
    private func positionLabel(at pos: CGPoint) -> some View {
        let scale = instance.window?.screen?.backingScaleFactor ?? NSScreen.main?.backingScaleFactor ?? 2.0
        GeometryReader { geo in
            let estimatedWidth: CGFloat = 100
            let margin: CGFloat = 6
            let rawX = pos.x + 55
            let clampedX = min(rawX, geo.size.width - estimatedWidth / 2 - margin)
            let finalX = max(estimatedWidth / 2 + margin, clampedX)
            let finalY = max(14, pos.y - 14)
            Text("(\(Int(pos.x * scale)), \(Int(pos.y * scale)))")
                .font(.system(size: 11, weight: .medium, design: .monospaced))
                .foregroundColor(.white)
                .padding(.horizontal, 6)
                .padding(.vertical, 3)
                .background(Color.black.opacity(0.75))
                .cornerRadius(4)
                .position(x: finalX, y: finalY)
        }
    }

    @ViewBuilder
    private func cropSelectionView(start: CGPoint, current: CGPoint) -> some View {
        let scale = instance.window?.screen?.backingScaleFactor ?? NSScreen.main?.backingScaleFactor ?? 2.0
        let minX = min(start.x, current.x)
        let minY = min(start.y, current.y)
        let w = abs(current.x - start.x)
        let h = abs(current.y - start.y)

        ZStack {
            Rectangle()
                .fill(Color.blue.opacity(0.08))
                .frame(width: w, height: h)
                .position(x: minX + w / 2, y: minY + h / 2)

            Rectangle()
                .stroke(Color.blue, lineWidth: 1)
                .frame(width: w, height: h)
                .position(x: minX + w / 2, y: minY + h / 2)

            GeometryReader { geo in
                let labelW: CGFloat = 180
                let labelH: CGFloat = 22
                let margin: CGFloat = 6
                let rawX = minX + w / 2
                let clampedX = min(max(labelW / 2 + margin, rawX), geo.size.width - labelW / 2 - margin)
                let belowY = minY + h + 2 + labelH / 2
                let aboveY = minY - 2 - labelH / 2
                let minAllowed = margin + labelH / 2
                let maxAllowed = geo.size.height - margin - labelH / 2
                let finalY: CGFloat = {
                    if belowY <= maxAllowed { return belowY }
                    if aboveY >= minAllowed { return aboveY }
                    return max(minAllowed, min(maxAllowed, belowY))
                }()
                Text("(\(Int(minX * scale)), \(Int(minY * scale))) \(Int(w * scale))×\(Int(h * scale))")
                    .font(.system(size: 11, weight: .medium, design: .monospaced))
                    .foregroundColor(.white)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 3)
                    .background(Color.black.opacity(0.75))
                    .cornerRadius(4)
                    .position(x: clampedX, y: finalY)
            }
        }
        .allowsHitTesting(false)
    }

    // MARK: - Toolbar

    /// The instance id sits at the leading edge beside the window buttons,
    /// then a flexible space pushes every button to the trailing edge.
    /// (Placements like .navigation / .primaryAction are no-ops in a plain
    /// WindowGroup toolbar — a Spacer item is what NSToolbar honors.)
    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        // macOS 26 draws a light capsule behind every toolbar item; hidden for
        // the badge, which is a label rather than a control — the id reads as
        // the window's name, not as a button waiting to be pressed.
        if #available(macOS 26.0, *) {
            ToolbarItem(placement: .automatic) { instanceBadge }
                .sharedBackgroundVisibility(.hidden)
        } else {
            ToolbarItem(placement: .automatic) { instanceBadge }
        }
        ToolbarItem(placement: .automatic) { Spacer() }
        toolbarFileItems
        toolbarActionItems
    }

    /// The dashboard: the server's bare root, which answers with the index page
    /// — what is running here, and links on to the control and view pages. The
    /// instance address the server reports names the machine in its path, and a
    /// GET there is a picture of the screen rather than somewhere to land, so
    /// the path is dropped and the origin is what gets handed out.
    private var dashboardURL: String? {
        guard let url = bridge.serverURL, var parts = URLComponents(string: url) else { return bridge.serverURL }
        parts.path = ""
        parts.query = nil
        parts.fragment = nil
        return parts.string
    }

    /// What the badge puts on the pasteboard: the dashboard's address on the
    /// network, which is what a phone's browser has to be pointed at and the
    /// only part of it worth typing by hand. With the server off there is no
    /// address, and the id it shows is copied instead — the name of its
    /// ~/.pob/<instance>/ directory, and what `pob show <id>` takes.
    private var badgeCopyText: String {
        dashboardURL ?? instance.settings.instanceID
    }

    /// Names this window's ~/.pob/<instance>/ directory, so the id on screen is
    /// the directory to look in. Click to copy the dashboard it is reachable
    /// at, like the targeting and crop labels copy what they point at.
    private var instanceBadge: some View {
        Button(action: {
            let text = badgeCopyText
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(text, forType: .string)
            showToast("Copied \(text)")
        }) {
            Text(instance.settings.instanceID)
                .font(.system(size: 11, weight: .medium, design: .monospaced))
                .foregroundStyle(controlActiveState == .inactive ? Color.secondary : Color.primary)
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .contentShape(Rectangle()) // no background now, so the padding is still the click
        }
        .buttonStyle(.plain)
        .help(dashboardURL.map { "Instance \(instance.settings.instanceID) — click to copy \($0)" }
            ?? "Instance \(instance.settings.instanceID) — click to copy")
    }

    @ToolbarContentBuilder
    private var toolbarFileItems: some ToolbarContent {
        ToolbarItem(placement: .automatic) {
            Button(action: { instance.settings.openSettingsFile() }) {
                Image(systemName: "gearshape")
            }
            .help("Settings")
        }
        ToolbarItem(placement: .automatic) {
            Button(action: { instance.settings.openLogsFolder() }) {
                Image(systemName: "doc.text")
            }
            .help("Logs")
        }
        ToolbarItem(placement: .automatic) {
            InstanceLogButton { isOptionDown in
                if isOptionDown {
                    instance.settings.openAppLog()
                } else {
                    instance.settings.openInstanceLog()
                }
            }
        }
        ToolbarItem(placement: .automatic) {
            Button(action: { instance.settings.openMacroFile() }) {
                Image(systemName: "wand.and.rays")
            }
            .help("Macro PSL")
        }
        ToolbarItem(placement: .automatic) {
            Button(action: {
                if isRecording {
                    stopRecording()
                } else if instance.settings.getMacro().trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    startRecording(clearingMacro: false)
                } else {
                    // Recording appends, so whatever is in main.macro.psl already
                    // would replay in front of everything recorded next.
                    showRecordWarning = true
                }
            }) {
                Image(systemName: isRecording ? "record.circle.fill" : "record.circle")
                    .foregroundStyle(isRecording ? Color.red : (controlActiveState == .inactive ? Color.secondary : Color.primary))
            }
            .help(isRecording ? "Recording (click to stop)" : "Record Macro")
        }
        ToolbarItem(placement: .automatic) {
            Button(action: {
                if bridge.isExecuting {
                    bridge.stopExecution()
                } else {
                    lockWindow()
                    bridge.runMacro()
                }
            }) {
                Image(systemName: bridge.isExecuting ? "stop.fill" : "play.fill")
            }
            .help(bridge.isExecuting ? "Stop" : "Execute")
            .animation(nil, value: bridge.isExecuting)
        }
    }

    @ToolbarContentBuilder
    private var toolbarActionItems: some ToolbarContent {
        ToolbarItem(placement: .automatic) {
            Button(action: {
                isTargeting.toggle()
                if isTargeting { isCropping = false; cropStart = nil; cropCurrent = nil }
                if !isTargeting { mousePosition = nil }
                updateClickThrough()
            }) {
                Image(systemName: "scope")
                    .foregroundStyle(isTargeting ? Color.accentColor : (controlActiveState == .inactive ? Color.secondary : Color.primary))
            }
            .help(isTargeting ? "Stop Targeting" : "Target")
        }
        ToolbarItem(placement: .automatic) {
            Button(action: {
                isCropping.toggle()
                if isCropping { isTargeting = false; mousePosition = nil }
                if !isCropping { cropStart = nil; cropCurrent = nil }
                updateClickThrough()
            }) {
                Image(systemName: "crop")
                    .foregroundStyle(isCropping ? Color.accentColor : (controlActiveState == .inactive ? Color.secondary : Color.primary))
            }
            .help(isCropping ? "Stop Cropping" : "Crop")
        }
        ToolbarItem(placement: .automatic) {
            Button(action: {
                // Flush pending recorded input first so the takeScreenshot()
                // line the core appends lands after the user's actions.
                instance.recorder.flushPending()
                bridge.takeScreenshot()
            }) {
                Image(systemName: "camera")
            }
            .help("Screenshot")
        }
        ToolbarItem(placement: .automatic) {
            Button(action: { isClickThrough.toggle() }) {
                Image(systemName: isClickThrough ? "hand.raised" : "hand.raised.slash")
                    .foregroundStyle(controlActiveState == .inactive ? Color.secondary : Color.primary)
            }
            .help(isClickThrough ? "Click-Through On (click to disable)" : "Click-Through Off (click to enable)")
        }
        ToolbarItem(placement: .automatic) {
            Button(action: { isLocked.toggle() }) {
                Image(systemName: isLocked ? "lock.fill" : "lock.open")
                    .foregroundStyle(controlActiveState == .inactive ? Color.secondary : Color.primary)
            }
            .help(isLocked
                ? "Window Locked — fixed size, and dragging carries the windows below (click to unlock)"
                : "Window Unlocked (click to lock)")
        }
        ToolbarItem(placement: .automatic) {
            Button(action: {
                mouseService.resetCursor()
                showToast("Mouse position reset")
            }) {
                Image(systemName: "arrow.counterclockwise")
            }
            .help("Reset Mouse Position")
        }
    }

    // MARK: - Helpers

    /// Starts macro recording, either over an emptied main.macro.psl or appending to
    /// what is already in it.
    private func startRecording(clearingMacro: Bool) {
        if clearingMacro {
            instance.settings.clearMacro()
        } else if !instance.settings.getMacro().trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            // Everything recorded next is a relative move from (20, 20), where
            // a replay starts. The kept lines leave the cursor wherever they
            // leave it, so send it home between them and what follows.
            instance.settings.appendToMacro("resetCursor()")
        }
        isRecording = true
        bridge.recordingChanged(true)
        lockWindow()
        // Outside a session, capture the user's own actions; enable
        // click-through so those actions reach the app below.
        if !bridge.isExecuting {
            instance.recorder.start()
            isClickThrough = true
        }
        showToast("Recording started")
    }

    private func stopRecording() {
        isRecording = false
        bridge.recordingChanged(false)
        instance.recorder.stop()
        showToast("Recording stopped")
    }

    /// Shows a transient bottom-centered message (black pill, white text) that
    /// fades out after ~2 s — action feedback like "main.macro.psl reset".
    private func showToast(_ message: String) {
        toastMessage = message
        toastToken += 1
        let token = toastToken
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
            if toastToken == token {
                withAnimation(.easeOut(duration: 0.25)) {
                    toastMessage = nil
                }
            }
        }
    }

    /// Running and recording both make the window's frame part of the result:
    /// every coordinate written to main.macro.psl is relative to it, so a resize
    /// partway through aims the replay at the wrong pixels. Play and Record
    /// turn the lock on themselves; turning it back off stays the user's call,
    /// as it was before.
    private func lockWindow() {
        isLocked = true
    }

    /// The lock holds the frame's *size*, and holds it onto what it frames.
    ///
    /// Moving stays allowed, because with the lock on a move no longer costs
    /// anything: the windows under the frame travel with it, so a macro's
    /// coordinates still land where they landed when it was recorded. A resize
    /// is the one that cannot be made harmless that way — it changes where
    /// every pixel inside the frame sits, and no amount of moving the windows
    /// below puts them back.
    private func updateWindowLock() {
        guard let window = instance.window else { return }
        let shouldLock = isLocked || bridge.isExecuting
        instance.carry.setEnabled(shouldLock)
        if shouldLock {
            window.styleMask.remove(.resizable)
        } else {
            window.styleMask.insert(.resizable)
        }
    }

    private func flashScreenshot() {
        screenshotFlashOpacity = 0.5
        withAnimation(.easeOut(duration: 0.4)) {
            screenshotFlashOpacity = 0
        }
    }

    private func updateClickThrough() {
        instance.setClickThrough(isClickThrough && !bridge.isExecuting && !isTargeting && !isCropping)
    }
}

private struct InstanceLogButton: View {
    /// Called with whether Option was held: the same button opens the app log
    /// instead, so the wider record costs a modifier rather than a toolbar slot.
    let action: (Bool) -> Void
    @State private var isHovered = false

    var body: some View {
        Button(action: { action(NSEvent.modifierFlags.contains(.option)) }) {
            Text(".log")
                .font(.system(size: 9, design: .monospaced))
                .padding(.horizontal, 4)
                .padding(.vertical, 2)
        }
        .buttonStyle(.plain)
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(isHovered ? Color.primary.opacity(0.1) : Color.clear)
        )
        .overlay(HoverDetectorView(isHovered: $isHovered))
        .help("Instance Log — ⌥ for app.log")
    }
}

private struct HoverDetectorView: NSViewRepresentable {
    @Binding var isHovered: Bool

    func makeNSView(context _: Context) -> HoverNSView {
        let view = HoverNSView()
        view.onHoverChange = { isHovered = $0 }
        return view
    }

    func updateNSView(_ nsView: HoverNSView, context _: Context) {
        nsView.onHoverChange = { isHovered = $0 }
    }
}

private class HoverNSView: NSView {
    var onHoverChange: ((Bool) -> Void)?
    private var trackingArea: NSTrackingArea?

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let old = trackingArea { removeTrackingArea(old) }
        let area = NSTrackingArea(rect: bounds, options: [.mouseEnteredAndExited, .activeAlways], owner: self, userInfo: nil)
        addTrackingArea(area)
        trackingArea = area
    }

    override func hitTest(_: NSPoint) -> NSView? { nil }
    override func mouseEntered(with _: NSEvent) { onHoverChange?(true) }
    override func mouseExited(with _: NSEvent) { onHoverChange?(false) }
}

private struct MouseTrackingOverlay: NSViewRepresentable {
    var onPositionChange: (CGPoint?) -> Void
    var onTap: (CGPoint) -> Void

    func makeNSView(context _: Context) -> TrackingNSView {
        let view = TrackingNSView()
        view.onPositionChange = onPositionChange
        view.onTap = onTap
        return view
    }

    func updateNSView(_ nsView: TrackingNSView, context _: Context) {
        nsView.onPositionChange = onPositionChange
        nsView.onTap = onTap
    }
}

private struct CropTrackingOverlay: NSViewRepresentable {
    var onDragChange: (CGPoint, CGPoint) -> Void
    var onDragEnd: (CGRect) -> Void

    func makeNSView(context _: Context) -> CropNSView {
        let view = CropNSView()
        view.onDragChange = onDragChange
        view.onDragEnd = onDragEnd
        return view
    }

    func updateNSView(_ nsView: CropNSView, context _: Context) {
        nsView.onDragChange = onDragChange
        nsView.onDragEnd = onDragEnd
    }
}

private class CropNSView: NSView {
    var onDragChange: ((CGPoint, CGPoint) -> Void)?
    var onDragEnd: ((CGRect) -> Void)?
    private var startPoint: CGPoint?

    override func acceptsFirstMouse(for _: NSEvent?) -> Bool {
        true
    }

    override var isFlipped: Bool {
        true
    }

    override func resetCursorRects() {
        addCursorRect(bounds, cursor: .crosshair)
    }

    override func mouseDown(with event: NSEvent) {
        startPoint = convert(event.locationInWindow, from: nil)
    }

    override func mouseDragged(with event: NSEvent) {
        guard let start = startPoint else { return }
        let current = convert(event.locationInWindow, from: nil)
        onDragChange?(start, current)
    }

    override func mouseUp(with event: NSEvent) {
        guard let start = startPoint else { return }
        let end = convert(event.locationInWindow, from: nil)
        let rect = CGRect(
            x: min(start.x, end.x),
            y: min(start.y, end.y),
            width: abs(end.x - start.x),
            height: abs(end.y - start.y)
        )
        startPoint = nil
        if rect.width > 2, rect.height > 2 {
            onDragEnd?(rect)
        }
    }
}

private class TrackingNSView: NSView {
    var onPositionChange: ((CGPoint?) -> Void)?
    var onTap: ((CGPoint) -> Void)?
    private var trackingArea: NSTrackingArea?

    override func acceptsFirstMouse(for _: NSEvent?) -> Bool {
        true
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let old = trackingArea { removeTrackingArea(old) }
        let area = NSTrackingArea(
            rect: bounds,
            options: [.mouseMoved, .mouseEnteredAndExited, .activeAlways],
            owner: self,
            userInfo: nil
        )
        addTrackingArea(area)
        trackingArea = area
    }

    override var isFlipped: Bool {
        true
    }

    override func mouseMoved(with event: NSEvent) {
        let pt = convert(event.locationInWindow, from: nil)
        onPositionChange?(pt)
    }

    override func mouseExited(with _: NSEvent) {
        onPositionChange?(nil)
    }

    override func mouseDown(with event: NSEvent) {
        let pt = convert(event.locationInWindow, from: nil)
        onTap?(pt)
        super.mouseDown(with: event)
    }
}
