import AppKit
import SwiftUI

struct ContentView: View {
    @State private var isExecuting = false
    @State private var currentTask: Task<Void, Never>?
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
    @State private var maxStepWarning = false
    @State private var maxStepContinuation: CheckedContinuation<Bool, Never>?
    @State private var animatedCursorPos: CGPoint = .init(x: 20, y: 20)
    @State private var screenshotFlashOpacity: Double = 0
    @ObservedObject private var mouseService = MouseService.shared
    @Environment(\.controlActiveState) private var controlActiveState

    var body: some View {
        ZStack {
            Color.gray.opacity(0.2)

            if isTargeting {
                MouseTrackingOverlay(
                    onPositionChange: { pos in mousePosition = pos },
                    onTap: { pt in
                        let scale = NSApplication.shared.windows.first?.screen?.backingScaleFactor ?? NSScreen.main?.backingScaleFactor ?? 2.0
                        let text = "(\(Int(pt.x * scale)), \(Int(pt.y * scale)))"
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(text, forType: .string)
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
                        let scale = NSApplication.shared.windows.first?.screen?.backingScaleFactor ?? NSScreen.main?.backingScaleFactor ?? 2.0
                        let text = "(\(Int(rect.minX * scale)), \(Int(rect.minY * scale)), \(Int(rect.width * scale)), \(Int(rect.height * scale)))"
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(text, forType: .string)
                        isCropping = false
                        cropStart = nil
                        cropCurrent = nil
                    }
                )

                if let start = cropStart, let current = cropCurrent {
                    cropSelectionView(start: start, current: current)
                }
            }

            if isExecuting {
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
        }
        .onChange(of: mouseService.displayPosition) { newPos in
            withAnimation(.easeOut(duration: 0.1)) {
                animatedCursorPos = newPos
            }
        }
        .onChange(of: isExecuting) { _ in
            updateWindowLock()
            updateClickThrough()
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
        .toolbar {
            ToolbarItemGroup(placement: .automatic) {
                Button(action: { SettingsService.shared.openSettingsFile() }) {
                    Image(systemName: "gearshape")
                }
                .help("Settings")

                Button(action: { SettingsService.shared.openLogsFolder() }) {
                    Image(systemName: "doc.text")
                }
                .help("Logs")

                Button(action: { SettingsService.shared.openInstructionFile() }) {
                    Image(systemName: "text.alignleft")
                }
                .help("Instruction")

                Button(action: { SettingsService.shared.openMacroFile() }) {
                    Image(systemName: "wand.and.rays")
                }
                .help("Macro")

                Button(action: {
                    isRecording.toggle()
                    if isRecording { SettingsService.shared.clearMacro() }
                }) {
                    Image(systemName: isRecording ? "record.circle.fill" : "record.circle")
                        .foregroundStyle(isRecording ? Color.red : (controlActiveState == .inactive ? Color.secondary : Color.primary))
                }
                .help(isRecording ? "Recording (click to stop)" : "Record Macro")

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

                Button(action: {
                    if isExecuting {
                        stop()
                    } else {
                        let macro = SettingsService.shared.getMacro()
                        if macro.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                            isExecuting = true
                            executeMain()
                        } else {
                            showMacroChoice = true
                        }
                    }
                }) {
                    Image(systemName: isExecuting ? "stop.fill" : "play.fill")
                }
                .help(isExecuting ? "Stop" : "Execute")
                .animation(nil, value: isExecuting)

                Button(action: {
                    isClickThrough.toggle()
                    updateClickThrough()
                }) {
                    Image(systemName: isClickThrough ? "hand.raised" : "hand.raised.slash")
                        .foregroundStyle(controlActiveState == .inactive ? Color.secondary : Color.primary)
                }
                .help(isClickThrough ? "Click-Through On (click to disable)" : "Click-Through Off (click to enable)")

                Button(action: {
                    isLocked.toggle()
                }) {
                    Image(systemName: isLocked ? "lock.fill" : "lock.open")
                        .foregroundStyle(controlActiveState == .inactive ? Color.secondary : Color.primary)
                }
                .help(isLocked ? "Window Locked (click to unlock)" : "Window Unlocked (click to lock)")

                Button(action: { showClearChoice = true }) {
                    Image(systemName: "trash")
                }
                .help("Clear")
            }
        }
        .onTapGesture {
            NSApplication.shared.activate(ignoringOtherApps: true)
            NSApplication.shared.windows.first?.makeKeyAndOrderFront(nil)
        }
        .alert("Warning", isPresented: $maxStepWarning) {
            Button("Continue") {
                maxStepWarning = false
                maxStepContinuation?.resume(returning: true)
                maxStepContinuation = nil
            }
            Button("Stop", role: .cancel) {
                maxStepWarning = false
                maxStepContinuation?.resume(returning: false)
                maxStepContinuation = nil
            }
        } message: {
            Text("Max step exceed.")
        }
        .alert("What would you like to run?", isPresented: $showMacroChoice) {
            Button("Run Instruction") { isExecuting = true; executeMain() }
            Button("Run Macro") { isExecuting = true; executeMacro() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("macro.txt has recorded actions.")
        }
        .confirmationDialog("Clear", isPresented: $showClearChoice) {
            Button("Clear Instruction", role: .destructive) { SettingsService.shared.clearInstruction() }
            Button("Clear Macro", role: .destructive) { SettingsService.shared.clearMacro() }
            Button("Clear Logs", role: .destructive) { SettingsService.shared.clearLogs() }
            Button("Clear All", role: .destructive) {
                SettingsService.shared.clearInstruction()
                SettingsService.shared.clearMacro()
                SettingsService.shared.clearLogs()
            }
            Button("Cancel", role: .cancel) {}
        }
    }

    @ViewBuilder
    private func positionLabel(at pos: CGPoint) -> some View {
        let scale = NSApplication.shared.windows.first?.screen?.backingScaleFactor ?? NSScreen.main?.backingScaleFactor ?? 2.0
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
        let scale = NSApplication.shared.windows.first?.screen?.backingScaleFactor ?? NSScreen.main?.backingScaleFactor ?? 2.0
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

    private func stop() {
        maxStepContinuation?.resume(returning: false)
        maxStepContinuation = nil
        maxStepWarning = false
        currentTask?.cancel()
        currentTask = nil
        isExecuting = false
        AppLogger.log("Stopped")
    }

    private func executeMacro() {
        animatedCursorPos = CGPoint(x: 20, y: 20)
        currentTask = Task {
            let window = await MainActor.run { NSApplication.shared.windows.first }
            MouseService.shared.resetCursor()

            let lines = SettingsService.shared.getMacro().components(separatedBy: .newlines)
            AppLogger.log("Executing macro (\(lines.filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }.count) actions)")

            guard let (_, ctx) = captureWithCursor(window: window) else {
                AppLogger.log("Macro: failed to get screenshot context")
                await MainActor.run { isExecuting = false }
                return
            }

            let sessionId = StorageService.shared.createSession()
            StorageService.shared.saveMacro(sessionId: sessionId)
            AppLogger.log("[\(sessionId)] Macro session started")

            for line in lines {
                guard !Task.isCancelled else { break }
                let trimmed = line.trimmingCharacters(in: .whitespaces)
                guard !trimmed.isEmpty else { continue }
                guard let (name, args) = parseMacroLine(trimmed) else {
                    AppLogger.log("Macro: skipping line: \(trimmed)")
                    continue
                }

                switch name {
                case "move":
                    guard args.count >= 2, let dx = Double(args[0]).map({ CGFloat($0) }), let dy = Double(args[1]).map({ CGFloat($0) }) else { continue }
                    MouseService.shared.moveCursorBy(dx: dx, dy: dy)
                    let pos = MouseService.shared.virtualCursorPosition
                    AppLogger.log("[\(sessionId)] Macro move(\(Int(dx)), \(Int(dy))) -> (\(Int(pos.x)), \(Int(pos.y)))")
                    await MainActor.run { withAnimation(.easeOut(duration: 0.1)) { animatedCursorPos = pos } }

                case "click":
                    let pos = MouseService.shared.virtualCursorPosition
                    AppLogger.log("[\(sessionId)] Macro click at (\(Int(pos.x)), \(Int(pos.y)))")
                    await MouseService.shared.performClick(at: ctx.toCGEventPoint(pixelX: pos.x, pixelY: pos.y))

                case "rightClick":
                    let pos = MouseService.shared.virtualCursorPosition
                    AppLogger.log("[\(sessionId)] Macro rightClick at (\(Int(pos.x)), \(Int(pos.y)))")
                    await MouseService.shared.performRightClick(at: ctx.toCGEventPoint(pixelX: pos.x, pixelY: pos.y))

                case "doubleClick":
                    let pos = MouseService.shared.virtualCursorPosition
                    AppLogger.log("[\(sessionId)] Macro doubleClick at (\(Int(pos.x)), \(Int(pos.y)))")
                    await MouseService.shared.performDoubleClick(at: ctx.toCGEventPoint(pixelX: pos.x, pixelY: pos.y))

                case "drag":
                    guard args.count >= 2, let dx = Double(args[0]).map({ CGFloat($0) }), let dy = Double(args[1]).map({ CGFloat($0) }) else { continue }
                    let startPos = MouseService.shared.virtualCursorPosition
                    let endPos = CGPoint(x: startPos.x + dx, y: startPos.y + dy)
                    AppLogger.log("[\(sessionId)] Macro drag(\(Int(dx)), \(Int(dy))) -> (\(Int(endPos.x)), \(Int(endPos.y)))")
                    await MouseService.shared.performDrag(from: ctx.toCGEventPoint(pixelX: startPos.x, pixelY: startPos.y), to: ctx.toCGEventPoint(pixelX: endPos.x, pixelY: endPos.y))
                    MouseService.shared.moveCursor(to: endPos)
                    await MainActor.run { withAnimation(.easeOut(duration: 0.1)) { animatedCursorPos = endPos } }

                case "scroll":
                    guard args.count >= 2, let dx = Double(args[0]).map({ Int32($0) }), let dy = Double(args[1]).map({ Int32($0) }) else { continue }
                    let pos = MouseService.shared.virtualCursorPosition
                    AppLogger.log("[\(sessionId)] Macro scroll(\(dx), \(dy)) at (\(Int(pos.x)), \(Int(pos.y)))")
                    await MouseService.shared.performScroll(at: ctx.toCGEventPoint(pixelX: pos.x, pixelY: pos.y), dx: dx, dy: dy)

                case "typeText":
                    guard let text = args.first else { continue }
                    AppLogger.log("[\(sessionId)] Macro typeText(\"\(text.prefix(80))\")")
                    await MouseService.shared.performType(text: text)

                case "keyPress":
                    guard let key = args.first else { continue }
                    AppLogger.log("[\(sessionId)] Macro keyPress(\"\(key)\")")
                    await MouseService.shared.performKeyPress(key: key)

                case "sleep":
                    guard let ms = args.first.flatMap({ Double($0) }) else { continue }
                    AppLogger.log("[\(sessionId)] Macro sleep(\(Int(ms))ms)")
                    try? await Task.sleep(nanoseconds: UInt64(ms * 1_000_000))

                case "take_screenshot":
                    let cropRect: CGRect? = args.count >= 4
                        ? { guard let x = Double(args[0]), let y = Double(args[1]),
                                  let w = Double(args[2]), let h = Double(args[3]) else { return nil }
                            return CGRect(x: x, y: y, width: w, height: h)
                        }()
                        : nil
                    if let r = cropRect {
                        AppLogger.log("[\(sessionId)] Macro take_screenshot(crop: \(Int(r.origin.x)), \(Int(r.origin.y)), \(Int(r.width)), \(Int(r.height)))")
                    } else {
                        AppLogger.log("[\(sessionId)] Macro take_screenshot")
                    }
                    await MainActor.run { flashScreenshot() }
                    if let (shot, _) = captureWithCursor(window: window) {
                        let finalShot = cropRect.flatMap { ScreenshotService.shared.crop(shot, to: $0) } ?? shot
                        StorageService.shared.saveScreenshot(finalShot, sessionId: sessionId)
                    }

                default:
                    AppLogger.log("[\(sessionId)] Macro: unknown action: \(name)")
                }

                let delayMs = SettingsService.shared.getMacroDefaultDelay()
                if delayMs > 0 {
                    try? await Task.sleep(nanoseconds: UInt64(delayMs) * 1_000_000)
                }
            }

            AppLogger.log("Macro execution complete")
            await MainActor.run { isExecuting = false; currentTask = nil }
        }
    }

    private func parseMacroLine(_ line: String) -> (name: String, args: [String])? {
        guard let openParen = line.firstIndex(of: "("), line.hasSuffix(")") else { return nil }
        let name = String(line[line.startIndex ..< openParen]).trimmingCharacters(in: .whitespaces)
        guard !name.isEmpty else { return nil }

        let argsStart = line.index(after: openParen)
        let argsEnd = line.index(before: line.endIndex)
        let argsStr = String(line[argsStart ..< argsEnd]).trimmingCharacters(in: .whitespaces)
        if argsStr.isEmpty { return (name, []) }

        if argsStr.hasPrefix("\"") {
            var result = ""
            var i = argsStr.index(after: argsStr.startIndex)
            while i < argsStr.endIndex {
                let ch = argsStr[i]
                if ch == "\\" {
                    let next = argsStr.index(after: i)
                    if next < argsStr.endIndex { result.append(argsStr[next]); i = argsStr.index(after: next) }
                    else { i = next }
                } else if ch == "\"" {
                    break
                } else {
                    result.append(ch)
                    i = argsStr.index(after: i)
                }
            }
            return (name, [result])
        }

        let parts = argsStr.components(separatedBy: ",").map { $0.trimmingCharacters(in: .whitespaces) }
        return (name, parts)
    }

    private func executeMain() {
        isExecuting = true
        animatedCursorPos = CGPoint(x: 20, y: 20)

        currentTask = Task {
            let window = await MainActor.run { NSApplication.shared.windows.first }

            // Reset virtual cursor to (20, 20) — near top-left of the capture area.
            MouseService.shared.resetCursor()

            let sessionId = StorageService.shared.createSession()
            AppLogger.log("[\(sessionId)] Session started")

            guard let (initShot, _) = captureWithCursor(window: window) else {
                AppLogger.log("Failed to capture screenshot")
                await MainActor.run { isExecuting = false }
                return
            }

            guard let initBase64 = toBase64(initShot) else {
                AppLogger.log("Failed to encode screenshot")
                await MainActor.run { isExecuting = false }
                return
            }

            let instruction = SettingsService.shared.getInstruction()
            StorageService.shared.saveInstruction(sessionId: sessionId)

            // Generate plan and execute; loop on resumePlan, stop on stop/done/cancel.
            var currentShot: NSImage = initShot
            var currentScreenshotBase64 = initBase64
            var outcome: PlanOutcome = .resumePlan
            while outcome == .resumePlan && !Task.isCancelled {
                let planId = StorageService.shared.createPlan(sessionId: sessionId)
                AppLogger.log("[\(sessionId)/\(planId)] Generating plan...")
                let plan = await AgentService.shared.generatePlan(instruction: instruction, screenshotBase64: currentScreenshotBase64, screenshot: currentShot, sessionId: sessionId, planId: planId)
                if let plan {
                    AppLogger.log("[\(sessionId)/\(planId)] Plan: \(plan)")
                }

                guard !Task.isCancelled else {
                    await MainActor.run { isExecuting = false }
                    return
                }

                outcome = await executePlan(
                    sessionId: sessionId,
                    planId: planId,
                    plan: plan,
                    window: window
                )

                if outcome == .resumePlan,
                   let (freshShot, _) = captureWithCursor(window: window),
                   let freshBase64 = toBase64(freshShot) {
                    currentShot = freshShot
                    currentScreenshotBase64 = freshBase64
                }
            }

            StorageService.shared.saveSessionUsage(sessionId: sessionId)
            AppLogger.log("[\(sessionId)] Session usage saved")

            let wasCancelled = Task.isCancelled
            await MainActor.run {
                isExecuting = false
                currentTask = nil
                if !wasCancelled {
                    let soundCmd = SettingsService.shared.getStopHook()
                    if !soundCmd.isEmpty {
                        let p = Process()
                        p.launchPath = "/bin/sh"
                        p.arguments = ["-c", soundCmd]
                        try? p.run()
                    }
                }
            }
        }
    }

    private enum PlanOutcome {
        case done
        case resumePlan
        case stop
    }

    @discardableResult
    private func executePlan(
        sessionId: String,
        planId: String,
        plan: String?,
        window: NSWindow?
    ) async -> PlanOutcome {
        guard let plan,
              let data = plan.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let rawSteps = json["steps"] as? [[String: Any]], !rawSteps.isEmpty else {
            AppLogger.log("[\(sessionId)] No plan steps to execute.")
            return .done
        }

        var steps: [(sequence: Int, instruction: String, expectation: String)] = []
        for s in rawSteps {
            if let seq = s["sequence"] as? Int, let inst = s["instruction"] as? String {
                steps.append((seq, inst, s["expectation"] as? String ?? ""))
            }
        }
        steps.sort { $0.sequence < $1.sequence }

        var stepIndex = 0
        while stepIndex < steps.count && !Task.isCancelled {
            let step = steps[stepIndex]
            var stepDone = false
            var jumpToIndex: Int? = nil
            var isStepResume = false

            while !stepDone && jumpToIndex == nil && !Task.isCancelled {
                let statusFile = StorageService.shared.stepStatusFile(sessionId: sessionId, planId: planId, stepSeq: step.sequence)
                let stepDir = statusFile.deletingLastPathComponent()

                await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
                    let lock = NSLock()
                    var resumed = false
                    let resume = {
                        lock.lock()
                        defer { lock.unlock() }
                        guard !resumed else { return }
                        resumed = true
                        continuation.resume()
                    }

                    // Watch step directory; fire when STATUS = "DONE"
                    let fd = open(stepDir.path, O_EVTONLY)
                    if fd >= 0 {
                        let src = DispatchSource.makeFileSystemObjectSource(
                            fileDescriptor: fd, eventMask: .write, queue: .global()
                        )
                        src.setEventHandler {
                            if let txt = try? String(contentsOf: statusFile, encoding: .utf8),
                               txt.trimmingCharacters(in: .whitespacesAndNewlines) == "DONE" {
                                src.cancel()
                                resume()
                            }
                        }
                        src.setCancelHandler { close(fd) }
                        src.resume()
                    }

                    Task {
                        await self.executeStep(
                            sessionId: sessionId,
                            planId: planId,
                            stepSeq: step.sequence,
                            stepInstruction: step.instruction,
                            stepExpectation: step.expectation,
                            plan: plan,
                            window: window,
                            isResume: isStepResume
                        )
                        resume() // fallback if watcher missed the event
                    }
                }

                let verifyResult = await verifyStep(
                    instruction: step.instruction,
                    expectation: step.expectation,
                    sessionId: sessionId,
                    planId: planId,
                    stepSeq: step.sequence,
                    window: window
                )
                switch verifyResult {
                case .verified:
                    stepDone = true
                case .resumeStep(let targetSeq):
                    if let targetSeq, let targetIndex = steps.firstIndex(where: { $0.sequence == targetSeq }), targetIndex != stepIndex {
                        AppLogger.log("[plan:\(sessionId)/step:\(step.sequence)] Jumping to step \(targetSeq)...")
                        jumpToIndex = targetIndex
                    } else {
                        AppLogger.log("[plan:\(sessionId)/step:\(step.sequence)] Retrying step \(step.sequence)...")
                        isStepResume = true
                    }
                case .resumePlan:
                    AppLogger.log("[plan:\(sessionId)] Resume All — regenerating plan...")
                    return .resumePlan
                case .stop:
                    AppLogger.log("[plan:\(sessionId)/step:\(step.sequence)] Stop — halting execution.")
                    return .stop
                }
            }

            stepIndex = jumpToIndex ?? (stepIndex + 1)
        }
        return .done
    }

    private func executeStep(
        sessionId: String,
        planId: String,
        stepSeq: Int,
        stepInstruction: String,
        stepExpectation: String,
        plan: String,
        window: NSWindow?,
        isResume: Bool = false
    ) async {
        StorageService.shared.writeStepStatus("RUNNING", sessionId: sessionId, planId: planId, stepSeq: stepSeq)
        AppLogger.log("[plan:\(sessionId)/step:\(stepSeq)] \(isResume ? "Resuming" : "Starting") step \(stepSeq): \(stepInstruction)")

        guard let (initShot, initCtx) = captureWithCursor(window: window),
              let initBase64 = toBase64(initShot) else {
            StorageService.shared.writeStepStatus("ERROR", sessionId: sessionId, planId: planId, stepSeq: stepSeq)
            return
        }

        var messages: [[String: Any]] = []
        var lastContext: ScreenshotContext? = initCtx
        var lastScreenshot: NSImage? = initShot
        var logId = 1
        var emptyResponseCount = 0
        var stepCount = 0
        let maxSteps = SettingsService.shared.getMaxSteps()

        let systemMsg: [String: Any] = [
            "role": "system",
            "content": """
            You are a desktop automation assistant. All coordinates are screenshot pixel coordinates \
            (origin = top-left of the screenshot, x right, y down). The app converts them to real \
            screen positions — you never deal with screen or OS coordinates.

            Available actions:
            • move(dx, dy) — nudge the cursor by a relative pixel offset; you receive a new screenshot showing the updated cursor arrow tip.
            • click() — left-click at the current cursor position (executes immediately).
            • rightClick() — right-click at the current cursor position (executes immediately).
            • doubleClick() — double-click at the current cursor position (executes immediately).
            • drag(dx, dy) — drag from the current cursor position to current+(dx,dy); cursor ends at the new position.
            • scroll(dx, dy) — scroll at the current cursor position; dy>0 = down, dy<0 = up, dx>0 = right.
            • typeText(text) — type text at the current keyboard focus.
            • keyPress(key) — press a special key: return, tab, space, delete, escape, left/right/up/down, \
            home, end, pageup, pagedown, f1–f12, cmd+a/c/v/x/z/w/s/t/r.
            • sleep(milliseconds) — pause execution for the given number of milliseconds.
            • take_screenshot(crop_x, crop_y, crop_width, crop_height) — capture a fresh screenshot; all crop parameters are optional. When provided, the image is cropped to that pixel region before being returned.

            Workflow:
            1. Use move(dx, dy) repeatedly to position the cursor arrow tip precisely on the target.
            2. Call the appropriate action (click, rightClick, doubleClick, drag, scroll, type, keyPress).

            The cursor starts at (20, 20).

            Execute this step: \(stepInstruction)
            Expectation: \(stepExpectation)

            Full execution plan for reference:
            \(plan)
            """,
        ]
        messages.append(systemMsg)

        let userMsg: [String: Any] = [
            "role": "user",
            "content": [
                ["type": "text", "text": "Step \(stepSeq): \(stepInstruction)"],
                ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(initBase64)"]],
            ] as [[String: Any]],
        ]
        messages.append(userMsg)

        let tools = AgentService.shared.makeTools()

        while !Task.isCancelled {
            if stepCount >= maxSteps {
                AppLogger.log("[plan:\(sessionId)/step:\(stepSeq)] Max step exceeded.")
                let shouldContinue = await withCheckedContinuation { continuation in
                    maxStepContinuation = continuation
                    maxStepWarning = true
                }
                if !shouldContinue { break }
                stepCount = 0
            }
            stepCount += 1

            AppLogger.log("[plan:\(sessionId)/step:\(stepSeq)/log:\(logId)] Analyzing...")

            let result = await OpenAIClient.shared.chat(messages: messages, tools: tools)

            var responseToSave: [String: Any] = result.success
                ? result.rawAssistantMessage
                : ["error": result.error ?? "Unknown error"]
            if let usage = result.usage { responseToSave["usage"] = usage }
            StorageService.shared.saveStepLog(sessionId: sessionId, planId: planId, stepSeq: stepSeq, logId: logId,
                                              messages: messages,
                                              response: responseToSave,
                                              screenshot: lastScreenshot)
            logId += 1

            if !result.success {
                AppLogger.log("[plan:\(sessionId)/step:\(stepSeq)] Error: \(result.error ?? "Unknown")")
                break
            }

            messages.append(result.rawAssistantMessage)

            if result.toolCalls.isEmpty {
                let text = result.contentText ?? ""
                if !text.isEmpty {
                    AppLogger.log("[plan:\(sessionId)/step:\(stepSeq)] Done: \(text.prefix(100))")
                    break
                }
                emptyResponseCount += 1
                if emptyResponseCount >= 3 {
                    AppLogger.log("[plan:\(sessionId)/step:\(stepSeq)] Too many empty responses, stopping.")
                    break
                }
                AppLogger.log("[plan:\(sessionId)/step:\(stepSeq)] Empty response, prompting to continue...")
                messages.append([
                    "role": "user",
                    "content": "Continue the task. Use move(dx, dy) to position the cursor, then call the appropriate action.",
                ])
                continue
            }
            emptyResponseCount = 0

            for toolCall in result.toolCalls {
                guard !Task.isCancelled else { break }

                switch toolCall.name {
                case "move":
                    let dx: CGFloat = (toolCall.arguments["dx"] as? Double).map { CGFloat($0) } ?? 0
                    let dy: CGFloat = (toolCall.arguments["dy"] as? Double).map { CGFloat($0) } ?? 0
                    MouseService.shared.moveCursorBy(dx: dx, dy: dy)
                    let newPos = MouseService.shared.virtualCursorPosition
                    AppLogger.log("[plan:\(sessionId)/step:\(stepSeq)] move(dx:\(Int(dx)), dy:\(Int(dy))) -> (\(Int(newPos.x)), \(Int(newPos.y)))")
                    if isRecording { SettingsService.shared.appendToMacro("move(\(Int(dx)), \(Int(dy)))") }

                    messages.append([
                        "role": "tool",
                        "tool_call_id": toolCall.id,
                        "content": "Cursor moved by (\(Int(dx)), \(Int(dy))). New position: (\(Int(newPos.x)), \(Int(newPos.y))).",
                    ])

                    if let (newShot, newCtx) = captureWithCursor(window: window) {
                        lastContext = newCtx
                        lastScreenshot = newShot
                        if let b64 = toBase64(newShot) {
                            messages.append([
                                "role": "user",
                                "content": [
                                    ["type": "text", "text": "Cursor at (\(Int(newPos.x)), \(Int(newPos.y))). The arrow tip is the click point. Move again or call click()."],
                                    ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]],
                                ] as [[String: Any]],
                            ])
                        }
                    }

                case "click":
                    let curPos = MouseService.shared.virtualCursorPosition
                    AppLogger.log("[plan:\(sessionId)/step:\(stepSeq)] click at (\(Int(curPos.x)), \(Int(curPos.y)))")
                    if isRecording { SettingsService.shared.appendToMacro("click()") }
                    if let ctx = lastContext {
                        let cgPt = ctx.toCGEventPoint(pixelX: curPos.x, pixelY: curPos.y)
                        await MouseService.shared.performClick(at: cgPt)
                    }
                    messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                     "content": "Clicked at (\(Int(curPos.x)), \(Int(curPos.y)))."])
                    if let (shot, ctx) = captureWithCursor(window: window), let b64 = toBase64(shot) {
                        lastContext = ctx; lastScreenshot = shot
                        messages.append(["role": "user", "content": [
                            ["type": "text", "text": "Clicked at (\(Int(curPos.x)), \(Int(curPos.y))). Screenshot after click:"],
                            ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]],
                        ] as [[String: Any]]])
                    }

                case "rightClick":
                    let curPos = MouseService.shared.virtualCursorPosition
                    AppLogger.log("[\(sessionId)] rightClick at (\(Int(curPos.x)), \(Int(curPos.y)))")
                    if isRecording { SettingsService.shared.appendToMacro("rightClick()") }
                    if let ctx = lastContext {
                        await MouseService.shared.performRightClick(at: ctx.toCGEventPoint(pixelX: curPos.x, pixelY: curPos.y))
                    }
                    messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                     "content": "Right-clicked at (\(Int(curPos.x)), \(Int(curPos.y)))."])
                    if let (shot, ctx) = captureWithCursor(window: window), let b64 = toBase64(shot) {
                        lastContext = ctx; lastScreenshot = shot
                        messages.append(["role": "user", "content": [
                            ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]],
                        ] as [[String: Any]]])
                    }

                case "doubleClick":
                    let curPos = MouseService.shared.virtualCursorPosition
                    AppLogger.log("[\(sessionId)] doubleClick at (\(Int(curPos.x)), \(Int(curPos.y)))")
                    if isRecording { SettingsService.shared.appendToMacro("doubleClick()") }
                    if let ctx = lastContext {
                        await MouseService.shared.performDoubleClick(at: ctx.toCGEventPoint(pixelX: curPos.x, pixelY: curPos.y))
                    }
                    messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                     "content": "Double-clicked at (\(Int(curPos.x)), \(Int(curPos.y)))."])
                    if let (shot, ctx) = captureWithCursor(window: window), let b64 = toBase64(shot) {
                        lastContext = ctx; lastScreenshot = shot
                        messages.append(["role": "user", "content": [
                            ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]],
                        ] as [[String: Any]]])
                    }

                case "drag":
                    let dx: CGFloat = (toolCall.arguments["dx"] as? Double).map { CGFloat($0) } ?? 0
                    let dy: CGFloat = (toolCall.arguments["dy"] as? Double).map { CGFloat($0) } ?? 0
                    let startPos = MouseService.shared.virtualCursorPosition
                    let endPos = CGPoint(x: startPos.x + dx, y: startPos.y + dy)
                    AppLogger.log("[\(sessionId)] drag(\(Int(dx)), \(Int(dy))) -> (\(Int(endPos.x)), \(Int(endPos.y)))")
                    if isRecording { SettingsService.shared.appendToMacro("drag(\(Int(dx)), \(Int(dy)))") }
                    if let ctx = lastContext {
                        let from = ctx.toCGEventPoint(pixelX: startPos.x, pixelY: startPos.y)
                        let to = ctx.toCGEventPoint(pixelX: endPos.x, pixelY: endPos.y)
                        await MouseService.shared.performDrag(from: from, to: to)
                    }
                    MouseService.shared.moveCursor(to: endPos)
                    messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                     "content": "Dragged to (\(Int(endPos.x)), \(Int(endPos.y)))."])
                    if let (shot, ctx) = captureWithCursor(window: window), let b64 = toBase64(shot) {
                        lastContext = ctx; lastScreenshot = shot
                        messages.append(["role": "user", "content": [
                            ["type": "text", "text": "Cursor at (\(Int(endPos.x)), \(Int(endPos.y)))."],
                            ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]],
                        ] as [[String: Any]]])
                    }

                case "scroll":
                    let dx = (toolCall.arguments["dx"] as? Double).map { Int32($0) } ?? 0
                    let dy = (toolCall.arguments["dy"] as? Double).map { Int32($0) } ?? 0
                    let curPos = MouseService.shared.virtualCursorPosition
                    AppLogger.log("[\(sessionId)] scroll(dx:\(dx), dy:\(dy)) at (\(Int(curPos.x)), \(Int(curPos.y)))")
                    if isRecording { SettingsService.shared.appendToMacro("scroll(\(dx), \(dy))") }
                    if let ctx = lastContext {
                        await MouseService.shared.performScroll(at: ctx.toCGEventPoint(pixelX: curPos.x, pixelY: curPos.y), dx: dx, dy: dy)
                    }
                    messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                     "content": "Scrolled dx:\(dx) dy:\(dy) at (\(Int(curPos.x)), \(Int(curPos.y)))."])
                    if let (shot, ctx) = captureWithCursor(window: window), let b64 = toBase64(shot) {
                        lastContext = ctx; lastScreenshot = shot
                        messages.append(["role": "user", "content": [
                            ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]],
                        ] as [[String: Any]]])
                    }

                case "typeText":
                    let text = toolCall.arguments["text"] as? String ?? ""
                    AppLogger.log("[\(sessionId)] typeText(\"\(text.prefix(80))\")")
                    if isRecording { SettingsService.shared.appendToMacro("typeText(\"\(text.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\""))\")") }
                    await MouseService.shared.performType(text: text)
                    messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                     "content": "Typed \"\(text)\"."])
                    if let (shot, ctx) = captureWithCursor(window: window), let b64 = toBase64(shot) {
                        lastContext = ctx; lastScreenshot = shot
                        messages.append(["role": "user", "content": [
                            ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]],
                        ] as [[String: Any]]])
                    }

                case "keyPress":
                    let key = toolCall.arguments["key"] as? String ?? ""
                    AppLogger.log("[\(sessionId)] keyPress(\"\(key)\")")
                    if isRecording { SettingsService.shared.appendToMacro("keyPress(\"\(key)\")") }
                    await MouseService.shared.performKeyPress(key: key)
                    messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                     "content": "Pressed \"\(key)\"."])
                    if let (shot, ctx) = captureWithCursor(window: window), let b64 = toBase64(shot) {
                        lastContext = ctx; lastScreenshot = shot
                        messages.append(["role": "user", "content": [
                            ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]],
                        ] as [[String: Any]]])
                    }

                case "sleep":
                    let ms = toolCall.arguments["milliseconds"] as? Double ?? 0
                    AppLogger.log("[\(sessionId)] sleep(\(Int(ms))ms)")
                    if isRecording { SettingsService.shared.appendToMacro("sleep(\(Int(ms)))") }
                    try? await Task.sleep(nanoseconds: UInt64(ms * 1_000_000))
                    messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                     "content": "Slept for \(Int(ms))ms."])

                case "take_screenshot":
                    let cropX = (toolCall.arguments["crop_x"] as? Double).map { CGFloat($0) }
                    let cropY = (toolCall.arguments["crop_y"] as? Double).map { CGFloat($0) }
                    let cropW = (toolCall.arguments["crop_width"] as? Double).map { CGFloat($0) }
                    let cropH = (toolCall.arguments["crop_height"] as? Double).map { CGFloat($0) }
                    let cropRect: CGRect? = (cropX != nil && cropY != nil && cropW != nil && cropH != nil)
                        ? CGRect(x: cropX!, y: cropY!, width: cropW!, height: cropH!) : nil
                    if let r = cropRect {
                        AppLogger.log("[\(sessionId)] take_screenshot(crop: \(Int(r.origin.x)), \(Int(r.origin.y)), \(Int(r.width)), \(Int(r.height)))")
                        if isRecording { SettingsService.shared.appendToMacro("take_screenshot(\(Int(r.origin.x)), \(Int(r.origin.y)), \(Int(r.width)), \(Int(r.height)))") }
                    } else {
                        AppLogger.log("[\(sessionId)] take_screenshot")
                        if isRecording { SettingsService.shared.appendToMacro("take_screenshot()") }
                    }
                    await MainActor.run { flashScreenshot() }
                    messages.append(["role": "tool", "tool_call_id": toolCall.id,
                                     "content": "Screenshot captured."])
                    if let (shot, ctx) = captureWithCursor(window: window) {
                        lastContext = ctx
                        let finalShot = cropRect.flatMap { ScreenshotService.shared.crop(shot, to: $0) } ?? shot
                        lastScreenshot = finalShot
                        StorageService.shared.saveScreenshot(finalShot, sessionId: sessionId)
                        if let b64 = toBase64(finalShot) {
                            messages.append(["role": "user", "content": [
                                ["type": "text", "text": "Current screenshot:"],
                                ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]],
                            ] as [[String: Any]]])
                        }
                    }

                default:
                    AppLogger.log("[\(sessionId)] Unknown tool: \(toolCall.name)")
                }
            }

            if Task.isCancelled { break }
        }

        StorageService.shared.writeStepStatus("DONE", sessionId: sessionId, planId: planId, stepSeq: stepSeq)
    }

    private enum VerifyResult {
        case verified
        case resumeStep(targetSeq: Int?)
        case resumePlan
        case stop
    }

    private func verifyStep(
        instruction: String,
        expectation: String,
        sessionId: String,
        planId: String,
        stepSeq: Int,
        window: NSWindow?
    ) async -> VerifyResult {
        guard !expectation.isEmpty else { return .verified }
        guard let (shot, _) = captureWithCursor(window: window),
              let b64 = toBase64(shot) else { return .verified }

        AppLogger.log("[plan:\(sessionId)/step:\(stepSeq)] Verifying: \(expectation)")

        let messages: [[String: Any]] = [
            [
                "role": "system",
                "content": """
                You are a verification assistant. Given a screenshot and step details, determine the outcome.
                Respond with JSON using one of three results:
                  {"result": "verified"} — expectation is met, proceed to next step.
                  {"result": "resumeStep", "reason": "..."} — this step failed and should be retried from the beginning.
                  {"result": "resumeStep", "stepSeq": N, "reason": "..."} — resume from step N (a specific step sequence number from the plan).
                  {"result": "resumePlan", "reason": "..."} — a critical error occurred; the entire plan must be recreated and restarted.
                  {"result": "stop", "reason": "..."} — execution should stop entirely.
                """,
            ],
            [
                "role": "user",
                "content": [
                    ["type": "text", "text": "Step instruction: \(instruction)\nExpectation: \(expectation)\n\nDoes the current screenshot match this expectation?"],
                    ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(b64)"]],
                ] as [[String: Any]],
            ],
        ]

        let result = await OpenAIClient.shared.chat(messages: messages, jsonMode: true)
        StorageService.shared.saveVerification(
            sessionId: sessionId,
            planId: planId,
            stepSeq: stepSeq,
            messages: messages + [result.rawAssistantMessage],
            response: result.rawAssistantMessage,
            screenshot: shot
        )
        guard result.success, let text = result.contentText,
              let data = text.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { return .verified }

        let resultStr = json["result"] as? String ?? "verified"
        let reason = json["reason"] as? String ?? ""
        let targetSeq = json["stepSeq"] as? Int

        switch resultStr {
        case "resumeStep":
            let seqDesc = targetSeq.map { " → step\($0)" } ?? ""
            AppLogger.log("[plan:\(sessionId)/step:\(stepSeq)] Verification RESUME STEP\(seqDesc)\(reason.isEmpty ? "" : ": \(reason)")")
            return .resumeStep(targetSeq: targetSeq)
        case "resumePlan":
            AppLogger.log("[plan:\(sessionId)/step:\(stepSeq)] Verification RESUME PLAN\(reason.isEmpty ? "" : ": \(reason)")")
            return .resumePlan
        case "stop":
            AppLogger.log("[plan:\(sessionId)/step:\(stepSeq)] Verification STOP\(reason.isEmpty ? "" : ": \(reason)")")
            return .stop
        default:
            AppLogger.log("[plan:\(sessionId)/step:\(stepSeq)] Verification PASS")
            return .verified
        }
    }

    // MARK: - Helpers

    private func captureWithCursor(window: NSWindow?) -> (NSImage, ScreenshotContext)? {
        guard let w = window else { return nil }
        guard let (shot, ctx) = ScreenshotService.shared.captureWindowContentAreaWithContext(window: w) else { return nil }
        let pos = MouseService.shared.virtualCursorPosition
        let withCursor = ScreenshotService.shared.imageWithCursor(shot, at: pos)
        return (withCursor, ctx)
    }

    private func toBase64(_ image: NSImage) -> String? {
        guard let tiff = image.tiffRepresentation,
              let rep = NSBitmapImageRep(data: tiff),
              let png = rep.representation(using: .png, properties: [:]) else { return nil }
        return png.base64EncodedString()
    }

    private func updateWindowLock() {
        guard let window = NSApplication.shared.windows.first else { return }
        let shouldLock = isLocked || isExecuting
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
        AppDelegate.shared?.setClickThrough(isClickThrough && !isExecuting && !isTargeting && !isCropping)
    }
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
