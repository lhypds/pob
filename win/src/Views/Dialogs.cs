// Modal dialogs mirroring the macOS/Linux shells: the record-over-a-macro
// warning and the About box.
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using Pob.Services;

namespace Pob.Views;

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

    // ── "macro.psl has recorded actions." Clear/Keep/Cancel ─────────────────

    public static RecordChoice ShowRecordWarning(Window? owner)
    {
        Window dialog = MakeDialog(owner, "Warning");
        RecordChoice choice = RecordChoice.Cancel;

        var message = new TextBlock
        {
            Text = "macro.psl has recorded actions. Clear them before recording?",
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
