import Foundation

/// What this run of Pob was started as, read once from the command line.
///
/// These are properties of a run rather than of the instance, so nothing on
/// disk remembers them: `~/.pob/<instance>/instance.json` keeps where the
/// window was and how it was left, and a mode given on the command line would
/// otherwise come back on the next launch without having been asked for.
enum AppOptions {
    /// `--fullscreen`: the window covers the whole display with none of its
    /// own chrome on it — no titlebar, no toolbar, no window buttons, and
    /// nothing anywhere to click. There is then no way to drive this Pob from
    /// the screen, which is the point of it: the `pob` command is the way in
    /// (`pob start`, `pob stop`, `pob screenshot`, `pob kill`).
    ///
    /// The CLI passes it through `open --args` for the packaged app, and
    /// straight to the binary for a dev build.
    static let fullscreen = CommandLine.arguments.contains("--fullscreen")
}
