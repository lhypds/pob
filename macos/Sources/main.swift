import SwiftUI

@main
struct PobApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) var appDelegate

    var body: some Scene {
        // One window, for the machine's one instance.
        WindowGroup(id: "instance") {
            ContentView()
                .frame(minWidth: 400, minHeight: 300)
        }
    }
}
