import Foundation

/// What this run of Pob was started as, read once at launch.
///
/// A flag given on the command line is a property of the run, so nothing on
/// disk remembers it — it would otherwise come back on the next launch without
/// having been asked for.
enum AppOptions {
    /// Whether the window comes up fullscreen: `--fullscreen` on the command
    /// line, or else however the instance was last left — see
    /// SettingsService.savedFullscreen. The window then covers the whole display with
    /// none of its own chrome on it — no titlebar, no toolbar, no window
    /// buttons — and the only thing left to press is the dot in the top-right
    /// corner, which brings the window back. The `pob` command drives it
    /// otherwise (`pob start`, `pob stop`, `pob screenshot`, `pob kill`).
    ///
    /// Only where the run starts: the toolbar's Fullscreen button and the dot
    /// switch it while Pob is up — see PobInstance.setFullscreen.
    ///
    /// The CLI passes the flag through `open --args` for the packaged app, and
    /// straight to the binary for a dev build.
    static let fullscreen = CommandLine.arguments.contains("--fullscreen") || SettingsService.savedFullscreen
}
