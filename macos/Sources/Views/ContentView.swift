import AppKit
import SwiftUI

/// Root view of every window in the WindowGroup. Owns one PobInstance for
/// the window's lifetime (the VSCode model: one process, one instance per
/// window) and attaches it to the hosting NSWindow once that exists.
struct ContentView: View {
    @StateObject private var instance = PobInstance()
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        InstanceContentView(instance: instance, bridge: instance.bridge, mouseService: instance.mouse)
            .background(WindowAccessor { window in
                instance.attach(window: window)
            })
            .onAppear {
                // Give the AppKit menu a way to open WindowGroup windows.
                AppDelegate.openNewInstanceWindow = { openWindow(id: "instance") }
            }
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
    @State private var isClickThrough = false
    @State private var isLocked = false
    @State private var isRecording = false
    @State private var showMacroChoice = false
    @State private var showClearChoice = false
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
                        let text = "(\(Int(pt.x * scale)), \(Int(pt.y * scale)))"
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
                        let text = "(\(Int(rect.minX * scale)), \(Int(rect.minY * scale)), \(Int(rect.width * scale)), \(Int(rect.height * scale)))"
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

            if bridge.isExecuting {
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
            }

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
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
                    .padding(.top, 10)
                    .allowsHitTesting(false)
                    .transition(.opacity)
            }
        }
        .onChange(of: mouseService.displayPosition) { newPos in
            withAnimation(.easeOut(duration: 0.1)) {
                animatedCursorPos = newPos
            }
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
        .onChange(of: isLocked) { _ in
            updateWindowLock()
        }
        .onChange(of: isTargeting) { _ in
            updateClickThrough()
        }
        .onChange(of: isCropping) { _ in
            updateClickThrough()
        }
        .onAppear {
            AppLogger.log("Pob started")
            updateClickThrough()
            updateWindowLock()
        }
        .toolbar { toolbarContent }
        .onTapGesture {
            NSApplication.shared.activate(ignoringOtherApps: true)
            instance.window?.makeKeyAndOrderFront(nil)
        }
        .alert("Warning", isPresented: $bridge.showMaxStepWarning) {
            Button("Continue") {
                bridge.resolveMaxStep(true)
            }
            Button("Stop", role: .cancel) {
                bridge.resolveMaxStep(false)
            }
        } message: {
            Text("Max step exceed.")
        }
        .alert("What would you like to run?", isPresented: $showMacroChoice) {
            Button("Run Instruction") { bridge.runInstruction(recording: isRecording) }
            Button("Run Macro") { bridge.runMacro() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("macro.txt has recorded actions.")
        }
        .confirmationDialog("Clear", isPresented: $showClearChoice) {
            Button("Clear Instruction", role: .destructive) {
                instance.settings.clearInstruction()
                showToast("Instruction cleared")
            }
            Button("Clear Macro", role: .destructive) {
                instance.settings.clearMacro()
                showToast("Macro cleared")
            }
            Button("Clear Logs", role: .destructive) {
                instance.settings.clearLogs()
                showToast("Logs cleared")
            }
            Button("Clear All", role: .destructive) {
                instance.settings.clearInstruction()
                instance.settings.clearMacro()
                instance.settings.clearLogs()
                showToast("Instruction, macro and logs cleared")
            }
            Button("Cancel", role: .cancel) {}
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

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        toolbarFileItems
        toolbarActionItems
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
            AppLogButton { instance.settings.openAppLog() }
        }
        ToolbarItem(placement: .automatic) {
            Button(action: { instance.settings.openInstructionFile() }) {
                Image(systemName: "text.alignleft")
            }
            .help("Instruction")
        }
        ToolbarItem(placement: .automatic) {
            Button(action: { instance.settings.openMacroFile() }) {
                Image(systemName: "wand.and.rays")
            }
            .help("Macro")
        }
        ToolbarItem(placement: .automatic) {
            Button(action: {
                isRecording.toggle()
                if isRecording { instance.settings.clearMacro() }
                bridge.recordingChanged(isRecording)
                if isRecording {
                    // Outside a session, capture the user's own actions; enable
                    // click-through so those actions reach the app below.
                    if !bridge.isExecuting {
                        instance.recorder.start()
                        if !isClickThrough {
                            isClickThrough = true
                            updateClickThrough()
                        }
                    }
                } else {
                    instance.recorder.stop()
                }
                showToast(isRecording ? "Recording started" : "Recording stopped")
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
                    let macro = instance.settings.getMacro()
                    if macro.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        bridge.runInstruction(recording: isRecording)
                    } else {
                        showMacroChoice = true
                    }
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
                // Flush pending recorded input first so the take_screenshot()
                // line the core appends lands after the user's actions.
                instance.recorder.flushPending()
                bridge.takeScreenshot()
            }) {
                Image(systemName: "camera")
            }
            .help("Screenshot")
        }
        ToolbarItem(placement: .automatic) {
            Button(action: {
                isClickThrough.toggle()
                updateClickThrough()
            }) {
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
            .help(isLocked ? "Window Locked (click to unlock)" : "Window Unlocked (click to lock)")
        }
        ToolbarItem(placement: .automatic) {
            Button(action: { showClearChoice = true }) {
                Image(systemName: "trash")
            }
            .help("Clear")
        }
    }

    // MARK: - Helpers

    /// Shows a transient top-centered message (black pill, white text) that
    /// fades out after ~2 s — action feedback like "Logs cleared".
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

    private func updateWindowLock() {
        guard let window = instance.window else { return }
        let shouldLock = isLocked || bridge.isExecuting
        window.isMovable = !shouldLock
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

private struct AppLogButton: View {
    let action: () -> Void
    @State private var isHovered = false

    var body: some View {
        Button(action: action) {
            VStack(spacing: 0) {
                Text("app")
                Text(".log")
            }
            .font(.system(size: 6, design: .monospaced))
            .padding(.horizontal, 4)
            .padding(.vertical, 2)
        }
        .buttonStyle(.plain)
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(isHovered ? Color.primary.opacity(0.1) : Color.clear)
        )
        .overlay(HoverDetectorView(isHovered: $isHovered))
        .help("App Log")
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
