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
        InstanceIdBadge.ToolTip = $"Instance {SettingsService.InstanceId} — click to copy";
        SetClickThroughVisual(AppState.IsClickThrough);
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
            ? "Window Locked (click to unlock)"
            : "Window Unlocked (click to lock)";
    }

    public void SetMaximizedVisual(bool maximized)
    {
        MaximizeIcon.Text = maximized ? GlyphRestore : GlyphMaximize;
        MaximizeBtn.ToolTip = maximized ? "Restore" : "Maximize";
    }

    // ── toolbar actions ─────────────────────────────────────────────────────

    // Copies the instance id, like the coordinate labels do. Handled so the
    // click doesn't also start a titlebar drag.
    private void OnInstanceIdClicked(object sender, MouseButtonEventArgs e)
    {
        ContentView.CopyToClipboard(SettingsService.InstanceId);
        Content2?.ShowMessage($"Copied {SettingsService.InstanceId}");
        e.Handled = true;
    }

    private void OnSettingsClicked(object sender, RoutedEventArgs e) => SettingsService.OpenSettingsFile();

    private void OnLogsClicked(object sender, RoutedEventArgs e) => SettingsService.OpenLogsFolder();

    private void OnAppLogClicked(object sender, RoutedEventArgs e) => SettingsService.OpenAppLog();

    private void OnInstructionClicked(object sender, RoutedEventArgs e) => SettingsService.OpenInstructionFile();

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
        // Recording appends, so whatever is in macro.txt already would replay
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

    // Starts macro recording, either over an emptied macro.txt or appending to
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
        if (!AppState.IsExecuting && !AppState.IsClickThrough)
        {
            AppState.IsClickThrough = true;
            SetClickThroughVisual(true);
            AppState.UpdateClickThrough();
        }
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

    private void OnPlayClicked(object sender, RoutedEventArgs e)
    {
        if (AppState.IsExecuting)
        {
            CoreBridge.StopExecution();
            return;
        }
        string macro = SettingsService.GetMacro().Trim();
        if (macro.Length == 0)
        {
            SetLocked(true);
            CoreBridge.RunInstruction(AppState.IsRecording);
        }
        else
            switch (Dialogs.ShowMacroChoice(this))
            {
                case MacroChoice.RunInstruction:
                    SetLocked(true);
                    CoreBridge.RunInstruction(AppState.IsRecording);
                    break;
                case MacroChoice.RunMacro:
                    SetLocked(true);
                    CoreBridge.RunMacro();
                    break;
            }
    }

    private void OnTargetClicked(object sender, RoutedEventArgs e) =>
        AppState.SetTargeting(!AppState.IsTargeting);

    private void OnCropClicked(object sender, RoutedEventArgs e) =>
        AppState.SetCropping(!AppState.IsCropping);

    private void OnScreenshotClicked(object sender, RoutedEventArgs e) =>
        CoreBridge.TakeScreenshot();

    private void OnClickThroughClicked(object sender, RoutedEventArgs e)
    {
        AppState.IsClickThrough = !AppState.IsClickThrough;
        SetClickThroughVisual(AppState.IsClickThrough);
        AppState.UpdateClickThrough();
    }

    private void OnLockClicked(object sender, RoutedEventArgs e) => SetLocked(!AppState.IsLocked);

    // Sets the window lock and syncs the toolbar button. Play and Record turn
    // it on themselves: recorded coordinates are relative to the window, so a
    // nudge or a resize partway through aims the replay at the wrong pixels.
    // Unlocking again is the user's call.
    private void SetLocked(bool locked)
    {
        if (AppState.IsLocked == locked) return;
        AppState.IsLocked = locked;
        SetLockVisual(locked);
        AppState.UpdateWindowLock();
    }

    private void OnResetClicked(object sender, RoutedEventArgs e) => Dialogs.ShowReset(this);

    // ── window controls ─────────────────────────────────────────────────────

    private void OnMinimizeClicked(object sender, RoutedEventArgs e) =>
        WindowState = WindowState.Minimized;

    private void OnMaximizeClicked(object sender, RoutedEventArgs e) =>
        ((App)Application.Current).ToggleMaximize();

    private void OnCloseClicked(object sender, RoutedEventArgs e) => Close();

    // ── titlebar drag + context menu ────────────────────────────────────────

    private void OnBarLeftButtonDown(object sender, MouseButtonEventArgs e)
    {
        // Window locked (or executing): swallow the press so the drag never starts.
        if (AppState.IsMoveResizeLocked) return;
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
