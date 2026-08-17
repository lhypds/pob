// The toolbar window — the compact titlebar of the Pob overlay, mirroring
// the macOS unified-compact toolbar / GTK headerbar: file group, action
// toggles, then window controls. It is the main window (owns the content
// overlay window glued below it) and stays interactive even while the
// content passes clicks through.
using System.Windows;
using System.Windows.Input;
using System.Windows.Interop;
using System.Windows.Media;
using Pob.Interop;
using Pob.Services;

namespace Pob.Views;

public partial class ToolbarWindow : Window
{
    public const double BarHeight = 32;

    // Segoe MDL2 Assets glyphs (Play, Stop, Lock, Unlock, ChromeMaximize,
    // ChromeRestore) — swapped at runtime on the matching buttons.
    private const string GlyphPlay = "\uE768";
    private const string GlyphStop = "\uE71A";
    private const string GlyphLocked = "\uE72E";
    private const string GlyphUnlocked = "\uE785";
    private const string GlyphMaximize = "\uE922";
    private const string GlyphRestore = "\uE923";

    private static readonly Brush DefaultIconBrush = Freeze(new SolidColorBrush(Color.FromRgb(0x40, 0x40, 0x40)));
    private static readonly Brush AccentBrush = Freeze(new SolidColorBrush(AppState.Accent));
    private static readonly Brush RecordBrush = Freeze(new SolidColorBrush(AppState.RecordRed));

    private static Brush Freeze(Brush b)
    {
        b.Freeze();
        return b;
    }

    public ToolbarWindow()
    {
        InitializeComponent();
        Title = $"Pob {AppState.Version}";
        InstanceIdText.Text = SettingsService.InstanceId;
        SyncInstanceBadge();
        SetClickThroughVisual(AppState.IsClickThrough);
        // The restored lock (App.OnStartup reads it) rather than a fixed
        // unlocked button: the state and the icon start out saying the same.
        SetLockVisual(AppState.IsLocked);
    }

    private ContentView? Content2 => AppState.Overlay?.ContentView;

    // ── extended-style flags ────────────────────────────────────────────────

    public void SetNoActivate(bool noActivate)
    {
        NativeMethods.SetExStyleFlag(new WindowInteropHelper(this).Handle,
                                     NativeMethods.WS_EX_NOACTIVATE, noActivate);
    }

    // ── toolbar visual state ────────────────────────────────────────────────

    public void SyncModeVisuals()
    {
        TargetBtn.Foreground = AppState.IsTargeting ? AccentBrush : DefaultIconBrush;
        TargetBtn.ToolTip = AppState.IsTargeting ? "Stop Targeting" : "Target";
        CropBtn.Foreground = AppState.IsCropping ? AccentBrush : DefaultIconBrush;
        CropBtn.ToolTip = AppState.IsCropping ? "Stop Cropping" : "Crop";
    }

    public void SetExecutingVisual(bool executing)
    {
        PlayIcon.Text = executing ? GlyphStop : GlyphPlay;
        PlayBtn.ToolTip = executing ? "Stop" : "Execute";
    }

    private void SetRecordingVisual(bool recording)
    {
        RecordBtn.Foreground = recording ? RecordBrush : DefaultIconBrush;
        RecordBtn.ToolTip = recording ? "Recording (click to stop)" : "Record Macro";
    }

    private void SetClickThroughVisual(bool on)
    {
        // Plain hand when ON, slashed hand when OFF — same pair as macOS.
        ClickThroughSlash.Visibility = on ? Visibility.Collapsed : Visibility.Visible;
        ClickThroughBtn.ToolTip = on
            ? "Click-Through On (click to disable)"
            : "Click-Through Off (click to enable)";
    }

    private void SetLockVisual(bool locked)
    {
        LockIcon.Text = locked ? GlyphLocked : GlyphUnlocked;
        LockBtn.ToolTip = locked
            ? "Window Locked — fixed size, and dragging carries the windows below (click to unlock)"
            : "Window Unlocked (click to lock)";
    }

    public void SetMaximizedVisual(bool maximized)
    {
        MaximizeIcon.Text = maximized ? GlyphRestore : GlyphMaximize;
        MaximizeBtn.ToolTip = maximized ? "Restore" : "Maximize";
    }

    // ── toolbar actions ─────────────────────────────────────────────────────

    // The tooltip says what a click would put on the clipboard, so it follows
    // the server up and down.
    public void SyncInstanceBadge()
    {
        string? dashboard = AppState.DashboardUrl;
        InstanceIdBadge.ToolTip = dashboard == null
            ? $"Instance {SettingsService.InstanceId} — click to copy"
            : $"Instance {SettingsService.InstanceId} — click to copy {dashboard}";
    }

    // Copies the dashboard this instance answers at, like the coordinate labels
    // copy what they point at. With the server off there is no address, and the
    // id shown is copied instead — the name of its ~/.pob/<instance>/
    // directory, and what `pob show <id>` takes. Handled so the click doesn't
    // also start a titlebar drag.
    private void OnInstanceIdClicked(object sender, MouseButtonEventArgs e)
    {
        string text = AppState.DashboardUrl ?? SettingsService.InstanceId;
        ContentView.CopyToClipboard(text);
        Content2?.ShowMessage($"Copied {text}");
        e.Handled = true;
    }

    // The app name stands in for the macOS app menu, so clicking it opens what
    // that menu's "About Pob" opens. Handled so the click doesn't also start a
    // titlebar drag.
    private void OnAppNameClicked(object sender, MouseButtonEventArgs e)
    {
        e.Handled = true;
        Dialogs.ShowAbout(this);
    }

    private void OnSettingsClicked(object sender, RoutedEventArgs e) => SettingsService.OpenSettingsFile();

    private void OnLogsClicked(object sender, RoutedEventArgs e) => SettingsService.OpenLogsFolder();

    // Alt-click asks for the app log instead — same button, the wider record.
    private void OnInsLogClicked(object sender, RoutedEventArgs e)
    {
        if ((Keyboard.Modifiers & ModifierKeys.Alt) != 0) SettingsService.OpenAppLog();
        else SettingsService.OpenInstanceLog();
    }

    private void OnMacroClicked(object sender, RoutedEventArgs e) => SettingsService.OpenMacroFile();

    private void OnRecordClicked(object sender, RoutedEventArgs e)
    {
        if (AppState.IsRecording)
        {
            StopRecording();
            return;
        }
        if (SettingsService.GetMacro().Trim().Length == 0)
        {
            StartRecording(clearingMacro: false);
            return;
        }
        // Recording appends, so whatever is in main.macro.psl already would replay
        // in front of everything recorded next.
        switch (Dialogs.ShowRecordWarning(this))
        {
            case RecordChoice.ClearMacro:
                StartRecording(clearingMacro: true);
                break;
            case RecordChoice.KeepMacro:
                StartRecording(clearingMacro: false);
                break;
        }
    }

    // Starts macro recording, either over an emptied main.macro.psl or appending to
    // what is already in it.
    private void StartRecording(bool clearingMacro)
    {
        if (clearingMacro)
        {
            SettingsService.ClearMacro();
        }
        else if (SettingsService.GetMacro().Trim().Length > 0)
        {
            // Everything recorded next is a relative move from (20, 20), where
            // a replay starts. The kept lines leave the cursor wherever they
            // leave it, so send it home between them and what follows.
            SettingsService.AppendToMacro("resetCursor()");
        }
        AppState.IsRecording = true;
        CoreBridge.RecordingChanged(true);
        SetLocked(true);
        // Same behavior as macOS: starting to record outside a session enables
        // click-through so interactions reach the app below the overlay.
        if (!AppState.IsExecuting) SetClickThrough(true);
        Content2?.ShowMessage("Recording started");
        SetRecordingVisual(true);
    }

    private void StopRecording()
    {
        AppState.IsRecording = false;
        CoreBridge.RecordingChanged(false);
        Content2?.ShowMessage("Recording stopped");
        SetRecordingVisual(false);
    }

    // The record button pressed from a terminal — `pob record start|stop`, over
    // AppState.SetRecording. No Clear/Keep question: there is nobody at the
    // window to answer it, so the answer is the one that cannot lose work and
    // what is in the macro already is kept.
    public void SetRecording(bool recording)
    {
        if (recording == AppState.IsRecording) return;
        if (recording) StartRecording(clearingMacro: false);
        else StopRecording();
    }

    private void OnPlayClicked(object sender, RoutedEventArgs e)
    {
        if (AppState.IsExecuting)
        {
            CoreBridge.StopExecution();
            return;
        }
        SetLocked(true);
        CoreBridge.RunMacro();
    }

    private void OnTargetClicked(object sender, RoutedEventArgs e) =>
        AppState.SetTargeting(!AppState.IsTargeting);

    private void OnCropClicked(object sender, RoutedEventArgs e) =>
        AppState.SetCropping(!AppState.IsCropping);

    private void OnScreenshotClicked(object sender, RoutedEventArgs e) =>
        CoreBridge.TakeScreenshot();

    private void OnClickThroughClicked(object sender, RoutedEventArgs e) =>
        SetClickThrough(!AppState.IsClickThrough);

    // Sets click-through and syncs the toolbar button. Written to instance.json
    // like the lock, so the next run starts the way this one was left: an
    // overlay left sitting over the app it drives would otherwise come back
    // swallowing the clicks meant for what is underneath.
    public void SetClickThrough(bool on)
    {
        if (AppState.IsClickThrough == on) return;
        AppState.IsClickThrough = on;
        SetClickThroughVisual(on);
        AppState.UpdateClickThrough();
        // Not from a fullscreen run: the overlay is the whole display there and
        // goes on passing clicks through whatever this says, so what would be
        // written down is a state the run never actually had.
        if (!AppState.IsFullscreen) SettingsService.SaveClickThrough(on);
    }

    private void OnLockClicked(object sender, RoutedEventArgs e) => SetLocked(!AppState.IsLocked);

    // Sets the window lock and syncs the toolbar button. Play and Record turn
    // it on themselves: recorded coordinates are relative to the window, so a
    // resize partway through aims the replay at the wrong pixels. Unlocking
    // again is the user's call.
    //
    // The lock is written to instance.json so the next run starts the way this
    // one was left: a window locked to hold a macro's coordinates would
    // otherwise come back loose and have to be locked again.
    public void SetLocked(bool locked)
    {
        if (AppState.IsLocked == locked) return;
        AppState.IsLocked = locked;
        SetLockVisual(locked);
        AppState.UpdateWindowLock();
        SettingsService.SaveWindowLocked(locked);
    }

    private void OnResetClicked(object sender, RoutedEventArgs e)
    {
        MouseService.ResetCursor();
        Content2?.ShowMessage("Mouse position reset");
    }

    // ── window controls ─────────────────────────────────────────────────────

    private void OnMinimizeClicked(object sender, RoutedEventArgs e) =>
        WindowState = WindowState.Minimized;

    private void OnMaximizeClicked(object sender, RoutedEventArgs e) =>
        ((App)Application.Current).ToggleMaximize();

    private void OnCloseClicked(object sender, RoutedEventArgs e) => Close();

    // ── titlebar drag + context menu ────────────────────────────────────────

    private void OnBarLeftButtonDown(object sender, MouseButtonEventArgs e)
    {
        // A locked window still drags: the lock holds its size, and what it
        // frames comes along with it. Only the edge resize is taken away.
        if (e.ButtonState == MouseButtonState.Pressed) DragMove();
    }

    private void OnAboutClicked(object sender, RoutedEventArgs e) => Dialogs.ShowAbout(this);

    private void OnQuitClicked(object sender, RoutedEventArgs e) => Application.Current.Shutdown();

    protected override void OnPreviewKeyDown(KeyEventArgs e)
    {
        base.OnPreviewKeyDown(e);
        // Quit Pob: Ctrl+Q (the stand-in for the macOS app menu item).
        if (e.Key == Key.Q && Keyboard.Modifiers == ModifierKeys.Control)
        {
            Application.Current.Shutdown();
            e.Handled = true;
        }
    }
}
