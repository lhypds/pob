// Modal dialogs mirroring the macOS/Linux shells: the max-step warning,
// the run-macro-or-instruction choice, the clear menu (stacked destructive
// buttons) and the About box.
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using Pob.Services;

namespace Pob.Views;

public enum MacroChoice
{
    Cancel,
    RunMacro,
    RunInstruction,
}

public static class Dialogs
{
    private static readonly Brush DestructiveBrush = new SolidColorBrush(AppState.RecordRed);

    private static Window MakeDialog(Window? owner, string title)
    {
        var dialog = new Window
        {
            Title = title,
            WindowStartupLocation = owner != null
                ? WindowStartupLocation.CenterOwner
                : WindowStartupLocation.CenterScreen,
            SizeToContent = SizeToContent.WidthAndHeight,
            ResizeMode = ResizeMode.NoResize,
            WindowStyle = WindowStyle.SingleBorderWindow,
            ShowInTaskbar = false,
            Topmost = true,
        };
        if (owner != null) dialog.Owner = owner;
        return dialog;
    }

    private static Button MakeButton(string label, Action onClick, bool destructive = false)
    {
        var button = new Button
        {
            Content = label,
            Padding = new Thickness(14, 4, 14, 4),
            Margin = new Thickness(4),
            MinWidth = 80,
        };
        if (destructive) button.Foreground = DestructiveBrush;
        button.Click += (_, _) => onClick();
        return button;
    }

    // ── "Max step exceed." Continue/Stop ────────────────────────────────────

    public static bool ShowMaxStep(Window? owner)
    {
        Window dialog = MakeDialog(owner, "Warning");
        bool shouldContinue = false;

        var message = new TextBlock
        {
            Text = "Max step exceed.",
            Margin = new Thickness(0, 0, 0, 12),
            HorizontalAlignment = HorizontalAlignment.Center,
        };

        var buttons = new StackPanel
        {
            Orientation = Orientation.Horizontal,
            HorizontalAlignment = HorizontalAlignment.Center,
        };
        buttons.Children.Add(MakeButton("Stop", () => dialog.Close()));
        buttons.Children.Add(MakeButton("Continue", () =>
        {
            shouldContinue = true;
            dialog.Close();
        }));

        var panel = new StackPanel { Margin = new Thickness(20) };
        panel.Children.Add(message);
        panel.Children.Add(buttons);
        dialog.Content = panel;
        dialog.ShowDialog();
        return shouldContinue;
    }

    // ── "What would you like to run?" ───────────────────────────────────────

    public static MacroChoice ShowMacroChoice(Window? owner)
    {
        Window dialog = MakeDialog(owner, "What would you like to run?");
        MacroChoice choice = MacroChoice.Cancel;

        var message = new TextBlock
        {
            Text = "macro.txt has recorded actions.",
            Margin = new Thickness(0, 0, 0, 12),
            HorizontalAlignment = HorizontalAlignment.Center,
        };

        var buttons = new StackPanel
        {
            Orientation = Orientation.Horizontal,
            HorizontalAlignment = HorizontalAlignment.Center,
        };
        buttons.Children.Add(MakeButton("Cancel", () => dialog.Close()));
        buttons.Children.Add(MakeButton("Run Macro", () =>
        {
            choice = MacroChoice.RunMacro;
            dialog.Close();
        }));
        buttons.Children.Add(MakeButton("Run Instruction", () =>
        {
            choice = MacroChoice.RunInstruction;
            dialog.Close();
        }));

        var panel = new StackPanel { Margin = new Thickness(20) };
        panel.Children.Add(message);
        panel.Children.Add(buttons);
        dialog.Content = panel;
        dialog.ShowDialog();
        return choice;
    }

    // ── Clear (stacked destructive buttons, like the macOS confirmation) ────

    public static void ShowClear(Window? owner)
    {
        Window dialog = MakeDialog(owner, "Clear");
        ContentView? content = AppState.Overlay?.ContentView;

        var panel = new StackPanel { Margin = new Thickness(20), MinWidth = 220 };

        void AddAction(string label, Action action, bool destructive = true)
        {
            Button button = MakeButton(label, () =>
            {
                dialog.Close();
                action();
            }, destructive);
            button.HorizontalAlignment = HorizontalAlignment.Stretch;
            panel.Children.Add(button);
        }

        AddAction("Clear Instruction", () =>
        {
            SettingsService.ClearInstruction();
            content?.ShowMessage("Instruction cleared");
        });
        AddAction("Clear Macro", () =>
        {
            SettingsService.ClearMacro();
            content?.ShowMessage("Macro cleared");
        });
        AddAction("Clear Logs", () =>
        {
            SettingsService.ClearLogs();
            content?.ShowMessage("Logs cleared");
        });
        AddAction("Clear All", () =>
        {
            SettingsService.ClearInstruction();
            SettingsService.ClearMacro();
            SettingsService.ClearLogs();
            content?.ShowMessage("Instruction, macro and logs cleared");
        });
        AddAction("Cancel", () => { }, destructive: false);

        dialog.Content = panel;
        dialog.ShowDialog();
    }

    // ── About ───────────────────────────────────────────────────────────────

    public static void ShowAbout(Window? owner)
    {
        Window dialog = MakeDialog(owner, "");

        var dim = new SolidColorBrush(Color.FromRgb(0x80, 0x80, 0x80));
        var panel = new StackPanel { Margin = new Thickness(20) };
        panel.Children.Add(new TextBlock
        {
            Text = "Pob",
            FontWeight = FontWeights.Bold,
            FontSize = 16,
            Margin = new Thickness(0, 0, 0, 4),
        });
        panel.Children.Add(new TextBlock
        {
            Text = "Perception & Operation Bridge",
            Foreground = dim,
            Margin = new Thickness(0, 0, 0, 4),
        });
        panel.Children.Add(new TextBlock
        {
            Text = $"Version {AppState.Version}",
            Foreground = dim,
            Margin = new Thickness(0, 0, 0, 12),
        });
        Button ok = MakeButton("OK", () => dialog.Close());
        ok.HorizontalAlignment = HorizontalAlignment.Right;
        panel.Children.Add(ok);

        dialog.Content = panel;
        dialog.ShowDialog();
    }
}
