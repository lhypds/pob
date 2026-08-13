// Which of Pob's two logs a shell message belongs in, matching the
// macOS/Linux AppLogger.
//
// <project root>\app.log is the machine's record across instances and is kept
// short on purpose: the app starting and stopping, an instance starting and
// stopping, and errors. Read on its own it should answer "did it come up, and
// did anything break" without scrolling.
//
// Everything else is detail, and detail belongs to the instance —
// <project root>\<instance>\instance.log, the file the toolbar's .log
// button opens and the one pob-core writes its own steps to. Every message
// logged here lands there whatever its level, so the shell's side of a run
// reads in order beside the core's.
//
// Lines are "[ISO8601] message" in app.log and "[ISO8601] LEVEL message" in
// instance.log, appended one at a time so two processes writing at once
// interleave without corrupting each other.
using System.IO;
using System.Globalization;
using Pob.Services;

namespace Pob;

public static class AppLogger
{
    private static readonly object LogLock = new();

    /// <summary>Detail: the instance log alone.</summary>
    public static void Log(string message) => Write("INFO", toAppLog: false, message);

    /// <summary>
    /// A line app.log is kept for — the app or an instance starting or
    /// stopping. Goes to both logs.
    /// </summary>
    public static void Event(string message) => Write("INFO", toAppLog: true, message);

    /// <summary>
    /// A failure. Goes to both logs, marked ERROR, so app.log answers what went
    /// wrong and instance.log keeps it beside the detail that led there.
    /// </summary>
    public static void Error(string message) => Write("ERROR", toAppLog: true, message);

    private static void Write(string level, bool toAppLog, string message)
    {
        string timestamp = DateTime.UtcNow.ToString("yyyy-MM-dd'T'HH:mm:ss.ffffff'Z'", CultureInfo.InvariantCulture);
        string marked = level == "INFO" ? message : $"{level} {message}";
        lock (LogLock)
        {
            if (toAppLog) Append(Path.Combine(SettingsService.ProjectRoot, "app.log"), $"[{timestamp}] {marked}\n");
            // The instance log names its level in a column of its own, the way
            // pob-core writes INSTANCE START and the rest of its events.
            Append(Path.Combine(SettingsService.InstanceDir, "instance.log"), $"[{timestamp}] {level} {message}\n");
        }
    }

    private static void Append(string path, string line)
    {
        try
        {
            File.AppendAllText(path, line);
        }
        catch (IOException)
        {
            // Never let logging take the app down.
        }
        catch (UnauthorizedAccessException)
        {
        }
    }
}
