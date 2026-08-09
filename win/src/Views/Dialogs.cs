// Modal dialogs mirroring the macOS/Linux shells: the max-step warning,
// the run-macro-or-instruction choice, the record-over-a-macro warning, the
// reset menu (stacked buttons) and the About box.
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

public enum RecordChoice
{
    Cancel,
    ClearMacro,
    KeepMacro,
}

public static class Dialogs
{
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

    private static Button MakeButton(string label, Action onClick)
    {
        var button = new Button
        {
            Content = label,
            Padding = new Thickness(14, 4, 14, 4),
            Margin = new Thickness(4),
            MinWidth = 80,
        };
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

    // ── Whatever the core has to say, with an OK ────────────────────────────

    public static void ShowAlert(Window? owner, string title, string message)
    {
        Window dialog = MakeDialog(owner, string.IsNullOrEmpty(title) ? "Pob" : title);

        var text = new TextBlock
        {
            Text = message,
            // The core writes these, and a sentence of settings advice is
            // longer than a dialog is wide.
            TextWrapping = TextWrapping.Wrap,
            MaxWidth = 360,
            Margin = new Thickness(0, 0, 0, 12),
        };

        Button ok = MakeButton("OK", () => dialog.Close());
        ok.HorizontalAlignment = HorizontalAlignment.Center;

        var panel = new StackPanel { Margin = new Thickness(20) };
        panel.Children.Add(text);
        panel.Children.Add(ok);
        dialog.Content = panel;
        dialog.ShowDialog();
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

    // ── "macro.txt has recorded actions." Clear/Keep/Cancel ─────────────────

    public static RecordChoice ShowRecordWarning(Window? owner)
    {
        Window dialog = MakeDialog(owner, "Warning");
        RecordChoice choice = RecordChoice.Cancel;

        var message = new TextBlock
        {
            Text = "macro.txt has recorded actions. Clear them before recording?",
            Margin = new Thickness(0, 0, 0, 12),
            HorizontalAlignment = HorizontalAlignment.Center,
        };

        var buttons = new StackPanel
        {
            Orientation = Orientation.Horizontal,
            HorizontalAlignment = HorizontalAlignment.Center,
        };
        buttons.Children.Add(MakeButton("Cancel", () => dialog.Close()));
        buttons.Children.Add(MakeButton("Keep", () =>
        {
            choice = RecordChoice.KeepMacro;
            dialog.Close();
        }));
        buttons.Children.Add(MakeButton("Clear", () =>
        {
            choice = RecordChoice.ClearMacro;
            dialog.Close();
        }));

        var panel = new StackPanel { Margin = new Thickness(20) };
        panel.Children.Add(message);
        panel.Children.Add(buttons);
        dialog.Content = panel;
        dialog.ShowDialog();
        return choice;
    }

    // ── Reset (stacked buttons, like the macOS confirmation) ────────────────

    public static void ShowReset(Window? owner)
    {
        Window dialog = MakeDialog(owner, "Reset");
        ContentView? content = AppState.Overlay?.ContentView;

        var panel = new StackPanel { Margin = new Thickness(20), MinWidth = 220 };

        void AddAction(string label, Action action)
        {
            Button button = MakeButton(label, () =>
            {
                dialog.Close();
                action();
            });
            button.HorizontalAlignment = HorizontalAlignment.Stretch;
            panel.Children.Add(button);
        }

        AddAction("Reset mouse position", () =>
        {
            MouseService.ResetCursor();
            content?.ShowMessage("Mouse position reset");
        });
        AddAction("Reset instruction.txt", () =>
        {
            SettingsService.ClearInstruction();
            content?.ShowMessage("instruction.txt reset");
        });
        AddAction("Reset macro.txt", () =>
        {
            SettingsService.ClearMacro();
            content?.ShowMessage("macro.txt reset");
        });
        AddAction("Close", () => { });

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
