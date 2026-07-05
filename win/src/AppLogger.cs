// Timestamped append-only logging to <project root>/app.log, matching the
// macOS/Linux AppLogger format: "[ISO8601] message\n".
using System.IO;
using Pob.Services;

namespace Pob;

public static class AppLogger
{
    private static readonly object LogLock = new();

    public static void Log(string message)
    {
        string timestamp = DateTime.UtcNow.ToString("yyyy-MM-dd'T'HH:mm:ss'Z'");
        string path = Path.Combine(SettingsService.ProjectRoot, "app.log");
        lock (LogLock)
        {
            try
            {
                File.AppendAllText(path, $"[{timestamp}] {message}\n");
            }
            catch (IOException)
            {
                // Never let logging take the app down.
            }
        }
    }
}
