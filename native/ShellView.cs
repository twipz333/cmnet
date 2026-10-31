using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
namespace Cmnet.Native;
public sealed partial class NativeWindow
{
    private void BuildShell()
    {
        // Purpose-built match cockpit: restrained navigation, generous
        // content viewport, one semantic accent and contextual actions.
        var layout = new Grid();
        layout.RowDefinitions.Add(new RowDefinition { Height = new GridLength(48) });
        layout.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        layout.RowDefinitions.Add(new RowDefinition { Height = new GridLength(27) });
        Content = layout;

        var top = new Grid { Background = ThemeBrush("SidebarBrush") };
        top.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(132) });
        top.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        top.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });

        var brand = new StackPanel {
            Orientation = Orientation.Horizontal,
            VerticalAlignment = VerticalAlignment.Center,
            Margin = new Thickness(13, 0, 0, 0)
        };
        var symbol = new Border {
            Width = 31, Height = 31,
            Background = ThemeBrush("SelectedBrush"),
            BorderBrush = ThemeBrush("PrimaryBrush"),
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(7),
            Child = Text("◈", 20, FontWeights.Bold, accent)
        };
        ((TextBlock)symbol.Child).HorizontalAlignment = HorizontalAlignment.Center;
        brand.Children.Add(symbol);
        var wordmark = new StackPanel { Margin = new Thickness(10, 3, 0, 0) };
        var logo = Text("CMNET", 16, FontWeights.Bold, foreground);
        logo.FontFamily = new FontFamily("Segoe UI Variable Display");
        wordmark.Children.Add(logo);
        
        brand.Children.Add(wordmark);
        top.Children.Add(brand);

        var pageHeading = new StackPanel {
            VerticalAlignment = VerticalAlignment.Center,
            Margin = new Thickness(17, 0, 0, 0)
        };
        heading.FontSize = 15;
        heading.FontWeight = FontWeights.SemiBold;
        heading.Foreground = foreground;
        subtitle.FontSize = 10;
        subtitle.Foreground = muted;
        subtitle.Margin = new Thickness(0, 4, 0, 0);
        pageHeading.Children.Add(heading);
        pageHeading.Children.Add(subtitle);
        Grid.SetColumn(pageHeading, 1);
        top.Children.Add(pageHeading);

        var connection = new Border {
            Background = ThemeBrush("RaisedBrush"),
            BorderBrush = ThemeBrush("PrimaryBrush"),
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(99),
            Padding = new Thickness(14, 8, 14, 8),
            Margin = new Thickness(10, 0, 25, 0),
            VerticalAlignment = VerticalAlignment.Center
        };
        connectionBadge.Text = "●  Соединение…";
        connectionBadge.Foreground = accent;
        connectionBadge.FontSize = 11;
        connectionBadge.FontWeight = FontWeights.SemiBold;
        connection.Child = connectionBadge;
        Grid.SetColumn(connection, 2);
        top.Children.Add(connection);
        layout.Children.Add(top);

        var content = new Grid();
        content.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(132) });
        content.ColumnDefinitions.Add(new ColumnDefinition {
            Width = new GridLength(1, GridUnitType.Star) });
        Grid.SetRow(content, 1);
        layout.Children.Add(content);

        var aside = new Border {
            Background = ThemeBrush("SidebarBrush"),
            BorderBrush = ThemeBrush("BorderBrush"),
            BorderThickness = new Thickness(0, 1, 1, 0)
        };
        // D7: flat 3B rail — same five destinations, no MATCH/SYSTEM inventing IA.
        var sidebar = new StackPanel { Margin = new Thickness(6, 12, 6, 10) };

        void AddNavigation(string name, string glyph, string shortcut)
        {
            var slot = new Grid { Margin = new Thickness(0, 0, 0, 3) };
            slot.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(3) });
            slot.ColumnDefinitions.Add(new ColumnDefinition {
                Width = new GridLength(1, GridUnitType.Star) });
            var marker = new Border {
                Width = 3,
                CornerRadius = new CornerRadius(2),
                Background = accent,
                Visibility = Visibility.Collapsed,
                Margin = new Thickness(0, 8, 0, 8)
            };
            navMarkers[name] = marker;
            slot.Children.Add(marker);
            var item = Btn($"{glyph}    {name}", async () =>
                await OpenPageAsync(name), true);
            item.HorizontalContentAlignment = HorizontalAlignment.Left;
            item.FontSize = 12;
            item.FontWeight = FontWeights.Medium;
            item.MinHeight = 36;
            item.Padding = new Thickness(10, 7, 6, 7);
            item.BorderThickness = new Thickness(1);
            item.ToolTip = $"Перейти: {name}  ({shortcut})";
            Grid.SetColumn(item, 1);
            slot.Children.Add(item);
            navigation[name] = item;
            sidebar.Children.Add(slot);
        }

        // Flat 3B rail: Scout/Draft (match) → Data/History/Settings/Friends (maintain).
        AddNavigation("Разведка", "◎", "Ctrl+1");
        AddNavigation("Драфт", "◇", "Ctrl+2");
        sidebar.Children.Add(new Border {
            Height = 1, Background = border,
            Margin = new Thickness(10, 10, 10, 10)
        });
        AddNavigation("Данные", "▤", "Ctrl+3");
        AddNavigation("История", "☰", "Ctrl+4");
        AddNavigation("Настройки", "⚙", "Ctrl+5");
        AddNavigation("Друзья", "♧", "Ctrl+6");

        sidebar.Children.Add(new Border {
            Height = 1, Background = border,
            Margin = new Thickness(10, 13, 10, 11)
        });
        var tip = new StackPanel();
        tip.Children.Add(Text("GO CORE · ЛОКАЛЬНО", 9, FontWeights.Bold, accent));
        var tipText = Text("Без облака\nCtrl+1…6", 10, color: muted);
        tipText.Margin = new Thickness(0, 8, 0, 0);
        tip.Children.Add(tipText);
        var tipCard = new Border {
            Background = ThemeBrush("RaisedBrush"),
            BorderBrush = ThemeBrush("BorderBrush"),
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(10),
            Padding = new Thickness(9),
            Margin = new Thickness(2, 0, 2, 0),
            Child = tip
        };
        sidebar.Children.Add(tipCard);
        aside.Child = new ScrollViewer {
            VerticalScrollBarVisibility = ScrollBarVisibility.Auto,
            HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled,
            Content = sidebar
        };
        content.Children.Add(aside);

        var main = new Border {
            Background = background,
            Padding = new Thickness(12, 10, 12, 10),
            Child = body
        };
        Grid.SetColumn(main, 1);
        content.Children.Add(main);

        var footer = new Grid {
            Background = ThemeBrush("SidebarBrush")
        };
        footer.ColumnDefinitions.Add(new ColumnDefinition {
            Width = new GridLength(1, GridUnitType.Star) });
        footer.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        status.Foreground = muted;
        status.FontSize = 11;
        status.Margin = new Thickness(23, 0, 0, 0);
        status.VerticalAlignment = VerticalAlignment.Center;
        status.TextTrimming = TextTrimming.CharacterEllipsis;
        status.Text = "Запуск локального ядра…";
        footer.Children.Add(status);
        toast.Foreground = gold;
        toast.FontSize = 11;
        toast.Margin = new Thickness(12, 0, 25, 0);
        toast.VerticalAlignment = VerticalAlignment.Center;
        Grid.SetColumn(toast, 1);
        footer.Children.Add(toast);
        Grid.SetRow(footer, 2);
        layout.Children.Add(footer);

        layout.SizeChanged += (_, _) =>
        {
            bool compact = layout.ActualWidth < 1000;
            if (content.ColumnDefinitions.Count > 0)
                content.ColumnDefinitions[0].Width = new GridLength(compact ? 64 : 132);
            if (top.ColumnDefinitions.Count > 0)
                top.ColumnDefinitions[0].Width = new GridLength(compact ? 64 : 132);
            wordmark.Visibility = compact ? Visibility.Collapsed : Visibility.Visible;
            tipCard.Visibility = compact || layout.ActualHeight < 650 ? Visibility.Collapsed : Visibility.Visible;
            var glyphByPage = new Dictionary<string, string>
            {
                ["Разведка"] = "◎",
                ["Драфт"] = "◇",
                ["Данные"] = "▤",
                ["История"] = "☰",
                ["Настройки"] = "⚙",
                ["Друзья"] = "♧"
            };
            foreach (var pair in navigation)
            {
                if (!glyphByPage.TryGetValue(pair.Key, out var glyph)) glyph = "·";
                pair.Value.Content = compact ? glyph : glyph + "    " + pair.Key;
            }
            subtitle.Visibility = layout.ActualHeight < 600 ? Visibility.Collapsed : Visibility.Visible;
        };
    }
}
