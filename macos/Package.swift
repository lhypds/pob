// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "Pob",
    platforms: [
        // v13 for SwiftUI's openWindow (New Instance opens a window in-process).
        .macOS(.v13),
    ],
    dependencies: [],
    targets: [
        .executableTarget(
            name: "Pob",
            dependencies: [],
            path: "Sources",
            swiftSettings: [
                .unsafeFlags(["-parse-as-library"]),
            ]
        ),
    ]
)
