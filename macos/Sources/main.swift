import SwiftUI

@main
struct PobApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) var appDelegate

    var body: some Scene {
        // Every window in the group is a full instance (own settings copy,
        // own pob-core); "New Instance" opens another window via this id.
        WindowGroup(id: "instance") {
            ContentView()
                .frame(minWidth: 400, minHeight: 300)
        }
    }
}
