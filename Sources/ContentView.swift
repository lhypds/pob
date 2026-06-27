import SwiftUI
import AppKit

struct ContentView: View {
    @State private var showSettings = false
    @State private var showLog = false
    
    var body: some View {
        ZStack {
            Color.gray.opacity(0.08)
            
            VStack {
                Spacer()
            }
        }
        .toolbar {
            ToolbarItemGroup(placement: .automatic) {
                Button(action: { showSettings.toggle() }) {
                    Image(systemName: "gearshape")
                }
                .help("Settings")

                Button(action: { showLog.toggle() }) {
                    Image(systemName: "doc.text")
                }
                .help("Log")
            }
        }
        .sheet(isPresented: $showSettings) {
            SettingsPanel(isPresented: $showSettings)
        }
        .sheet(isPresented: $showLog) {
            LogPanel(isPresented: $showLog)
        }
        .onTapGesture {
            NSApplication.shared.activate(ignoringOtherApps: true)
            NSApplication.shared.windows.first?.makeKeyAndOrderFront(nil)
        }
    }
}

// #Preview disabled - requires Xcode preview compilation support
