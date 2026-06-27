import Cocoa
import SwiftUI

class AppDelegate: NSObject, NSApplicationDelegate {
    var window: NSWindow?
    
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApplication.shared.setActivationPolicy(.regular)

        // Configure the main window
        if let window = NSApplication.shared.windows.first {
            self.window = window
            
            // Keep the capture area transparent while retaining normal title bar chrome
            window.isOpaque = false
            window.backgroundColor = NSColor.clear
            window.titlebarAppearsTransparent = false
            window.titleVisibility = .hidden
            window.title = "Pob \(loadVersion())"
            window.toolbarStyle = .unifiedCompact
            
            // Make window resizable
            window.styleMask.insert(.resizable)
            window.styleMask.insert(.miniaturizable)
            window.styleMask.insert(.closable)
            
            // Use standard window level so focus/traffic-light behavior matches normal macOS windows
            window.level = .normal
            
            // Allow window to accept mouse events
            window.ignoresMouseEvents = false
            
            // Restore saved position/size, or use default centered position
            if let savedFrame = SettingsService.shared.getWindowFrame() {
                window.setFrame(savedFrame, display: true)
            } else {
                window.setFrame(NSRect(x: 100, y: 100, width: 600, height: 400), display: true)
                window.center()
            }

            window.delegate = self

            // Make sure the window becomes key/main so traffic-light buttons are active on focus
            window.makeKeyAndOrderFront(nil)
            NSApplication.shared.activate(ignoringOtherApps: true)

            window.standardWindowButton(.closeButton)?.isEnabled = true
            window.standardWindowButton(.miniaturizeButton)?.isEnabled = true
            window.standardWindowButton(.zoomButton)?.isEnabled = true
        }
        
        // Create application menu
        createMenu()
    }

    private func loadVersion() -> String {
        let fallback = "0.0.0"

        let fileManager = FileManager.default
        let currentDirectoryPath = fileManager.currentDirectoryPath
        let currentDirectoryVersionPath = URL(fileURLWithPath: currentDirectoryPath).appendingPathComponent("VERSION")

        if let value = try? String(contentsOf: currentDirectoryVersionPath, encoding: .utf8)
            .trimmingCharacters(in: .whitespacesAndNewlines),
           !value.isEmpty {
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
           !value.isEmpty {
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
        
        // App menu items
        let quitMenuItem = NSMenuItem(title: "Quit Pob", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appMenu.addItem(quitMenuItem)
        
        let appMenuItem = NSMenuItem(title: "Pob", action: nil, keyEquivalent: "")
        appMenuItem.submenu = appMenu
        mainMenu.addItem(appMenuItem)
        
        NSApplication.shared.mainMenu = mainMenu
    }
}

extension AppDelegate: NSWindowDelegate {
    func windowDidMove(_ notification: Notification) {
        saveWindowFrame()
    }

    func windowDidEndLiveResize(_ notification: Notification) {
        saveWindowFrame()
    }
}
