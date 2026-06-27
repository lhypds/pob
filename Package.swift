// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "Pob",
    platforms: [
        .macOS(.v12),
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
