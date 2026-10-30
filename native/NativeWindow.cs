using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using System.IO;
using System.Linq;
using System.Net.Http;
using System.Text.Json.Nodes;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using Microsoft.Win32;

namespace Cmnet.Native;

// WPF draws genuine Windows controls, not HTML. In particular the Scout
// roster and scrolling lists are retained between SSE updates.
public sealed partial class NativeWindow : Window
{
    private readonly AdvisorCore core = new();
    private readonly HeroArtwork artwork;
    private readonly Brush background = Hex("#0B1117");
    private readonly Brush panel = Hex("#121A23");
    private readonly Brush soft = Hex("#203044");
    private readonly Brush border = Hex("#263849");
    private readonly Brush foreground = Hex("#F1F5F9");
    private readonly Brush muted = Hex("#A0B0C3");
    private readonly Brush accent = Hex("#30D9C4");
    private readonly Brush gold = Hex("#F3B562");
    private readonly Brush good = Hex("#7BD9A5");

    private readonly ContentControl body = new();
    private readonly TextBlock status = new();
    private readonly TextBlock heading = new();
    private readonly TextBlock subtitle = new();
    private readonly TextBlock connectionBadge = new();
    // Each view build must own fresh WPF elements. Reusing a TextBlock across
    // navigation causes "already the logical child of another element", leaving
    // the old page on screen while the header/sidebar switch to the new one.
    private TextBlock allyMetric = new();
    private TextBlock enemyMetric = new();
    private readonly Dictionary<string, Button> navigation = new();
    private readonly Dictionary<string, Border> navMarkers = new();
    private string page = "Разведка";
    private JsonObject? state;
    private readonly CoreSnapshotGate snapshotGate = new();
    private JsonObject? stats;
    private JsonObject? players;
    private string scoutVersion = "";
    private string playersVersion = "";
    private DateTime lastScoutLoad = DateTime.MinValue;
    private bool loadingScout;

    // Native WPF view references. Event ticks update text rather than rebuilding
    // ScrollViewer and 10 seat cards, preserving focus and scroll position.
    private readonly Dictionary<string, TextBlock> rosterLabels = new();
    private readonly Dictionary<string, Image> rosterAvatars = new();
    private readonly Dictionary<string, TextBlock> rosterBadges = new();
    private readonly Dictionary<string, TextBlock> rosterHeroes = new();
    private TextBlock? scoutLive;
    private ScrollViewer? scoutViewport;
    private TextBlock? draftSummary;
    private ListBox? draftRecommendations;
    private readonly ObservableCollection<HeroRow> heroRows = new();
    private ListBox? heroList;
    private WrapPanel? heroGallery;
    private TextBlock? selectedHeroHint;
    private int selectedHeroId;
    private int heroPageSize = 48;
    private TextBox? heroSearchBox;
    private TextBlock? heroResultsCount;
    private Button? heroShowMore;
    private Button? applyDraftButton;
    private string heroSearchText = "";
    private StackPanel? scoutDetails;
    private WrapPanel? scoutedPlayers;
    private long selectedScoutId;
    private string selectedScoutSide = "enemy";
    private ListBox? knownPlayersList;
    private readonly List<PlayerRow> knownPlayerRows = new();
    private string knownPlayerFilter = "";
    private TextBlock? bookEmptyHint;
    private Grid? teamGrid;
    private string heroCatalogKey = "";
    private string recommendationKey = "";
    private TextBlock? dataActivity;
    private TextBlock corpusMatches = new();
    private TextBlock corpusDrafts = new();
    private TextBlock corpusCoverage = new();
    private TextBlock corpusHeroes = new();
    private TextBlock? shareStatus;
    private StackPanel? configHost;
    private readonly List<ConfigField> configEditors = new();
    private readonly TextBlock toast = new();

    public NativeWindow(string? initialDataDirectory = null, bool visualPreview = false)
    {
        artwork = new HeroArtwork(() => core.DataDirectory);
        Title = "CMNET · Match Intelligence";
        Width = 1460;
        Height = 860;
        MinWidth = 940;
        MinHeight = 600;
        WindowStartupLocation = WindowStartupLocation.CenterScreen;
        Background = background;
        Foreground = foreground;
        FontFamily = new FontFamily("Segoe UI Variable Text");
        UseLayoutRounding = true;
        SnapsToDevicePixels = true;

        // Purpose-built match cockpit: restrained navigation, generous
        // content viewport, one semantic accent and contextual actions.
        var layout = new Grid();
        layout.RowDefinitions.Add(new RowDefinition { Height = new GridLength(58) });
        layout.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        layout.RowDefinitions.Add(new RowDefinition { Height = new GridLength(27) });
        Content = layout;

        var top = new Grid { Background = Hex("#101927") };
        top.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(148) });
        top.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        top.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });

        var brand = new StackPanel {
            Orientation = Orientation.Horizontal,
            VerticalAlignment = VerticalAlignment.Center,
            Margin = new Thickness(13, 0, 0, 0)
        };
        var symbol = new Border {
            Width = 31, Height = 31,
            Background = Hex("#193B3E"),
            BorderBrush = Hex("#327F7A"),
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
        wordmark.Children.Add(Text("MATCH INTELLIGENCE", 9, FontWeights.Bold, muted));
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
            Background = Hex("#152A37"),
            BorderBrush = Hex("#305363"),
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
        content.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(148) });
        content.ColumnDefinitions.Add(new ColumnDefinition {
            Width = new GridLength(1, GridUnitType.Star) });
        Grid.SetRow(content, 1);
        layout.Children.Add(content);

        var aside = new Border {
            Background = Hex("#101927"),
            BorderBrush = Hex("#26364A"),
            BorderThickness = new Thickness(0, 1, 1, 0)
        };
        var sidebar = new StackPanel { Margin = new Thickness(6, 15, 6, 10) };
        var section = Text("МАТЧ", 9, FontWeights.Bold, muted);
        section.Margin = new Thickness(13, 0, 0, 13);
        sidebar.Children.Add(section);

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

        AddNavigation("Разведка", "◎", "Ctrl+1");
        AddNavigation("Драфт", "◇", "Ctrl+2");
        sidebar.Children.Add(new Border {
            Height = 1, Background = border,
            Margin = new Thickness(10, 12, 10, 11)
        });
        var sectionOther = Text("СИСТЕМА", 9, FontWeights.Bold, muted);
        sectionOther.Margin = new Thickness(13, 0, 0, 13);
        sidebar.Children.Add(sectionOther);
        AddNavigation("Данные", "▤", "Ctrl+3");
        AddNavigation("Настройки", "⚙", "Ctrl+4");
        AddNavigation("Друзья", "♧", "Ctrl+5");

        sidebar.Children.Add(new Border {
            Height = 1, Background = border,
            Margin = new Thickness(10, 13, 10, 11)
        });
        var tip = new StackPanel();
        tip.Children.Add(Text("ЛОКАЛЬНЫЙ КЛИЕНТ", 9, FontWeights.Bold, accent));
        var tipText = Text("Без облачного сервера\nCtrl + 1…5 · разделы", 10, color: muted);
        tipText.Margin = new Thickness(0, 10, 0, 0);
        tip.Children.Add(tipText);
        sidebar.Children.Add(new Border {
            Background = Hex("#162535"),
            BorderBrush = Hex("#293F54"),
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(10),
            Padding = new Thickness(9),
            Margin = new Thickness(2, 0, 2, 0),
            Child = tip
        });
        aside.Child = new ScrollViewer {
            VerticalScrollBarVisibility = ScrollBarVisibility.Auto,
            HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled,
            Content = sidebar
        };
        content.Children.Add(aside);

        var main = new Border {
            Background = background,
            Padding = new Thickness(16, 12, 17, 13),
            Child = body
        };
        Grid.SetColumn(main, 1);
        content.Children.Add(main);

        var footer = new Grid {
            Background = Hex("#101927")
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

        PreviewKeyDown += async (_, e) =>
        {
            if ((Keyboard.Modifiers & ModifierKeys.Control) == 0) return;
            var n = e.Key switch
            {
                Key.D1 or Key.NumPad1 => 0, Key.D2 or Key.NumPad2 => 1,
                Key.D3 or Key.NumPad3 => 2, Key.D4 or Key.NumPad4 => 3,
                Key.D5 or Key.NumPad5 => 4, _ => -1
            };
            var pages = new[] { "Разведка", "Драфт", "Данные", "Настройки", "Друзья" };
            if (n >= 0) { e.Handled = true; await OpenPageAsync(pages[n]); }
            else if (e.Key == Key.F && page == "Драфт" && heroSearchBox != null)
            {
                e.Handled = true;
                heroSearchBox.Focus();
                heroSearchBox.SelectAll();
            }
            else if (e.Key == Key.R)
            {
                e.Handled = true;
                if (page == "Разведка") await LoadScoutAsync();
                else if (page == "Данные") await LoadDataAsync();
                else if (page == "Настройки") await LoadSettingsAsync();
            }
        };

        core.StateReceived += snapshot => Dispatcher.BeginInvoke(() => ApplyState(snapshot));
        core.ConnectionChanged += message => Dispatcher.BeginInvoke(() =>
        {
            connectionBadge.Text = message == "Go-ядро подключено" ? "●  Локально · онлайн" : "●  " + message;
            connectionBadge.Foreground = message == "Go-ядро подключено" ? good : gold;
        });
        if (!visualPreview)
            Loaded += async (_, _) => await BootAsync(initialDataDirectory);
        Closed += (_, _) => core.Stop();
        RenderPage();
    }

    private static Brush Hex(string code) => (Brush)new BrushConverter().ConvertFromString(code)!;
    private static Color HexColor(string code) => (Color)ColorConverter.ConvertFromString(code)!;
    private TextBlock Text(string value, double size = 14, FontWeight? weight = null, Brush? color = null)
        => new() { Text = value, FontSize = size, FontWeight = weight ?? FontWeights.Normal,
            Foreground = color ?? foreground, TextWrapping = TextWrapping.Wrap, VerticalAlignment = VerticalAlignment.Center };
    private Button Btn(string label, Func<Task> action, bool subtle = false)
    {
        var b = new Button
        {
            Content = label, Cursor = Cursors.Hand, Padding = new Thickness(11, 7, 11, 7),
            Background = subtle ? soft : Hex("#24555D"), Foreground = foreground,
            BorderBrush = subtle ? border : Hex("#397D81"),
            BorderThickness = new Thickness(1), MinHeight = 32
        };
        b.Click += async (_, _) =>
        {
            b.IsEnabled = false;
            try { await action(); }
            catch (Exception ex) { MessageBox.Show(this, ex.Message, "cmnet", MessageBoxButton.OK, MessageBoxImage.Warning); }
            finally { b.IsEnabled = true; }
        };
        return b;
    }
    private Border Surface(UIElement child, Thickness margin)
        => new() { Background = panel, CornerRadius = new CornerRadius(14),
            BorderBrush = border, BorderThickness = new Thickness(1),
            Margin = margin, Padding = new Thickness(20), Child = child };
    private StackPanel Stack(params UIElement[] children)
    {
        var s = new StackPanel();
        foreach (var child in children) s.Children.Add(child);
        return s;
    }
    private static ScrollViewer Scroll(UIElement child) => new()
    {
        Content = child, VerticalScrollBarVisibility = ScrollBarVisibility.Auto,
        HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled,
        PanningMode = PanningMode.VerticalOnly
    };
    private void Header(StackPanel host, string label)
    {
        var h = Text(label, 18, FontWeights.SemiBold);
        h.Margin = new Thickness(0, 8, 0, 15);
        host.Children.Add(h);
    }
    private static JsonNode? Obj(JsonNode? node, params string[] keys) => node.At(keys);
    private async Task BootAsync(string? newFolder = null)
    {
        try
        {
            await core.StartAsync(newFolder);
            status.Text = $"Подключено · {core.DataDirectory}";
            await OpenPageAsync(page);
        }
        catch (Exception ex)
        {
            status.Text = "Ошибка запуска: " + ex.Message;
            var stack = Stack(Text("Не удалось подключить Go-ядро", 22, FontWeights.Bold),
                Text(ex.Message, 14, color: muted),
                Btn("Выбрать существующую папку данных", async () =>
                {
                    var dialog = new OpenFolderDialog { Title = "Папка с cmnet.db и bundle.json" };
                    if (dialog.ShowDialog(this) == true)
                    {
                        var executable = Environment.ProcessPath ?? throw new InvalidOperationException("Путь приложения неизвестен");
                        var psi = new System.Diagnostics.ProcessStartInfo(executable) { UseShellExecute = true };
                        psi.ArgumentList.Add("--data-dir");
                        psi.ArgumentList.Add(dialog.FolderName);
                        System.Diagnostics.Process.Start(psi);
                        Close();
                    }
                }));
            body.Content = stack;
        }
    }
    private async Task OpenPageAsync(string name)
    {
        // Build the view before changing the navigation state. A failed view
        // construction must not leave "Разведка" highlighted over a draft page.
        if (name != page || body.Content == null)
        {
            RenderPage(name);
        }
        if (page != name) return; // construction failed; keep current view
        if (name == "Разведка") await LoadScoutAsync();
        else if (name == "Данные") await LoadDataAsync();
        else if (name == "Настройки") await LoadSettingsAsync();
    }
    private void RenderPage()
    {
        RenderPage(page);
    }
    private void RenderPage(string target)
    {
        UIElement view;
        try
        {
            view = target switch
            {
                "Разведка" => BuildScoutView(),
                "Драфт" => BuildDraftView(),
                "Данные" => BuildDataView(),
                "Настройки" => BuildSettingsView(),
                "Друзья" => BuildShareView(),
                _ => Text(target)
            };
        }
        catch (Exception ex)
        {
            toast.Text = $"Ошибка экрана «{target}»: {ex.Message}";
            return; // preserve previous page + selection
        }
        page = target;
        body.Content = view;
        heading.Text = page;
        subtitle.Text = page switch
        {
            "Разведка" => "Команды, игроки и информация о противниках",
            "Драфт" => "Предложения модели и управление выбором героев",
            "Данные" => "Корпус матчей и обучение модели",
            "Настройки" => "Параметры движка и интеграции",
            _ => "Совместная работа и приглашения"
        };
        foreach (var item in navigation)
        {
            bool active = item.Key == page;
            item.Value.Background = active ? Hex("#1D4247") : Hex("#101927");
            item.Value.BorderBrush = active ? Hex("#3D7773") : Hex("#101927");
            item.Value.Foreground = active ? accent : foreground;
            item.Value.FontWeight = active ? FontWeights.Bold : FontWeights.Medium;
            if (navMarkers.TryGetValue(item.Key, out var marker))
                marker.Visibility = active ? Visibility.Visible : Visibility.Collapsed;
        }
        if (state != null) UpdateLiveState();
    }
    private void ApplyState(JsonObject next)
    {
        // Ignore SSE frames older than the latest mutation response.
        if (!snapshotGate.TryAccept(next)) return;
        state = next;
        UpdateLiveState();
        if (page == "Разведка" && DateTime.UtcNow - lastScoutLoad > TimeSpan.FromSeconds(9))
            _ = LoadScoutAsync();
    }
    private void UpdateLiveState()
    {
        if (state == null) return;
        status.Text = state.At("status").S("Подключено");
        if (page == "Разведка")
        {
            UpdateScoutRoster();
            var active = state.At("ops","active").Items()
                .Where(j => j.At("kind").S() is "scout" or "scout-allies")
                .ToList();
            if (scoutLive != null)
                scoutLive.Text = active.Count == 0 ? "Разведка не выполняется" :
                    string.Join("    ·    ", active.Select(j =>
                    $"{(j.At("kind").S() == "scout" ? "Враги" : "Союзники")}: {j.At("lastLine").S("ожидание")}"));
        }
        if (page == "Драфт") UpdateDraft();
        if (page == "Данные" && dataActivity != null)
        {
            var jobs = state.At("ops","active").Items().ToList();
            dataActivity.Text = jobs.Count == 0 ? "Активных заданий нет" :
                string.Join("\n", jobs.Select(j => $"{j.At("kind").S()} · {j.At("lastLine").S()}"));
        }
        if (page == "Друзья" && shareStatus != null)
            shareStatus.Text = $"Режим: {state.At("share","live").S("—")}  ·  Подключений: {state.At("share","followers").I()}  ·  Код: {state.At("share","host","code").S("—")}";
    }

    private async Task RunAction(string endpoint, object? payload = null)
    {
        if (core.BaseUri == null) return;
        var response = await core.PostAsync(endpoint, payload ?? new { });
        var message = System.Text.Json.Nodes.JsonNode.Parse(response).At("status").S();
        toast.Text = string.IsNullOrWhiteSpace(message) ? "Операция отправлена" : message;
        if (page == "Разведка") await LoadScoutAsync();
    }

    private Border Pill(string label, Brush backgroundColor, Brush textColor)
    {
        return new Border
        {
            Background = backgroundColor, CornerRadius = new CornerRadius(999),
            Padding = new Thickness(11, 6, 11, 6), Child = Text(label, 11, FontWeights.SemiBold, textColor),
            HorizontalAlignment = HorizontalAlignment.Left, VerticalAlignment = VerticalAlignment.Center
        };
    }

    private Border MetricTile(string label, TextBlock counter, Brush dotColor)
    {
        var column = new StackPanel();
        var line = new StackPanel { Orientation = Orientation.Horizontal };
        line.Children.Add(new Border { Background = dotColor, CornerRadius = new CornerRadius(4),
            Width = 7, Height = 7, Margin = new Thickness(0, 0, 8, 0), VerticalAlignment = VerticalAlignment.Center });
        line.Children.Add(Text(label.ToUpperInvariant(), 10, FontWeights.Bold, muted));
        column.Children.Add(line);
        counter.FontSize = 24;
        counter.FontWeight = FontWeights.Bold;
        counter.Foreground = foreground;
        counter.Margin = new Thickness(0, 8, 0, 0);
        counter.Text = "0 / 5";
        column.Children.Add(counter);
        return new Border { Child = column, Background = Hex("#1D2C3B"), BorderBrush = border,
            BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(12),
            Padding = new Thickness(18, 14, 22, 14), MinWidth = 150, Margin = new Thickness(0, 0, 10, 10) };
    }

    private StackPanel BuildSide(string side)
    {
        var host = new StackPanel();
        var isAlly = side == "our";
        var title = new Grid { Margin = new Thickness(0, 0, 0, 20) };
        title.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        title.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        var caption = new StackPanel();
        caption.Children.Add(Text(isAlly ? "СВОЙ СОСТАВ" : "СОСТАВ ВРАГА", 10, FontWeights.Bold, muted));
        var head = Text(isAlly ? "Союзники" : "Противники", 20, FontWeights.Bold);
        head.Margin = new Thickness(0, 3, 0, 0);
        caption.Children.Add(head);
        title.Children.Add(caption);
        var mark = Pill(isAlly ? "RADIANT / DIRE" : "АНАЛИЗ", Hex(isAlly ? "#203F40" : "#3F302F"),
            isAlly ? accent : gold);
        Grid.SetColumn(mark, 1);
        title.Children.Add(mark);
        host.Children.Add(title);

        for (int pos = 1; pos <= 5; pos++)
        {
            var key = $"{side}:{pos}";
            int position = pos;
            var row = new Grid();
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(34) });
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(48) });
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
            var number = new Border
            {
                Background = Hex(isAlly ? "#254D4B" : "#4B3B31"),
                BorderBrush = Hex(isAlly ? "#416F69" : "#6C5441"),
                BorderThickness = new Thickness(1),
                CornerRadius = new CornerRadius(8), Height = 27, Width = 27,
                VerticalAlignment = VerticalAlignment.Center,
                Child = Text(pos.ToString("D2"), 11, FontWeights.Bold, isAlly ? accent : gold)
            };
            ((TextBlock)number.Child).HorizontalAlignment = HorizontalAlignment.Center;
            row.Children.Add(number);

            var avatarFrame = new Grid { Height = 39, Width = 39, Margin = new Thickness(0, 0, 6, 0) };
            avatarFrame.Children.Add(new Border { Background = Hex("#273B4B"), CornerRadius = new CornerRadius(9),
                Child = Text("?", 16, FontWeights.Bold, muted) });
            ((TextBlock)((Border)avatarFrame.Children[0]).Child).HorizontalAlignment = HorizontalAlignment.Center;
            var avatar = new Image { Height = 37, Width = 37, Stretch = Stretch.UniformToFill };
            rosterAvatars[key] = avatar;
            avatarFrame.Children.Add(avatar);
            Grid.SetColumn(avatarFrame, 1);
            row.Children.Add(avatarFrame);

            var identity = new StackPanel { Margin = new Thickness(8, 0, 6, 0),
                VerticalAlignment = VerticalAlignment.Center };
            var label = Text("Место свободно", 13, FontWeights.SemiBold);
            label.TextTrimming = TextTrimming.CharacterEllipsis;
            label.TextWrapping = TextWrapping.NoWrap;
            var heroes = Text("Позиция " + pos, 11, color: muted);
            heroes.TextTrimming = TextTrimming.CharacterEllipsis;
            heroes.TextWrapping = TextWrapping.NoWrap;
            var badge = Text("", 10, FontWeights.SemiBold, gold);
            badge.TextTrimming = TextTrimming.CharacterEllipsis;
            badge.TextWrapping = TextWrapping.NoWrap;
            rosterLabels[key] = label;
            rosterHeroes[key] = heroes;
            rosterBadges[key] = badge;
            identity.Children.Add(label);
            identity.Children.Add(heroes);
            identity.Children.Add(badge);
            Grid.SetColumn(identity, 2);
            row.Children.Add(identity);

            var actions = new StackPanel { Orientation = Orientation.Vertical, Margin = new Thickness(7, 0, 0, 0) };
            var details = Btn("Статистика", async () =>
            {
                var ids = state.At("team", side).Items().ToArray();
                var id = position <= ids.Length ? ids[position - 1]?.L() ?? 0 : 0;
                if (id > 0) ShowScoutDetails(side, id);
                await Task.CompletedTask;
            }, true);
            details.FontSize = 11;
            details.Padding = new Thickness(7, 4, 7, 4);
            details.Margin = new Thickness(0, 0, 0, 4);
            actions.Children.Add(details);
            var choose = Btn("Заменить", async () => await EditSeatAsync(side, position), true);
            choose.FontSize = 11;
            choose.Padding = new Thickness(7, 4, 7, 4);
            actions.Children.Add(choose);
            Grid.SetColumn(actions, 3);
            row.Children.Add(actions);

            host.Children.Add(new Border { Child = row, Background = Hex("#1D2B3A"),
                BorderBrush = Hex("#324457"), BorderThickness = new Thickness(1),
                CornerRadius = new CornerRadius(11), Padding = new Thickness(12, 11, 12, 11),
                Margin = new Thickness(0, 0, 0, 8) });
        }
        var controls = new StackPanel { Margin = new Thickness(0, 16, 0, 0) };
        controls.Children.Add(Text("ДОБАВИТЬ ИГРОКА", 10, FontWeights.Bold, muted));
        var input = new TextBox { Margin = new Thickness(0, 9, 0, 10), MinWidth = 0,
            ToolTip = "Steam account ID или ссылка на профиль" };
        controls.Children.Add(input);
        var buttons = new WrapPanel();
        var scout = Btn("◎  Разведать", async () =>
        {
            var value = input.Text.Trim();
            if (value.Length > 0)
                await RunAction(isAlly ? "/api/ops/scout-allies" : "/api/ops/scout", new { players = value });
        });
        scout.Margin = new Thickness(0, 0, 8, 8);
        buttons.Children.Add(scout);
        var rescout = Btn("Обновить состав", async () =>
        {
            var ids = state.At("team", side).Items().Select(j => j.L()).Where(id => id > 0).Distinct().ToArray();
            if (ids.Length > 0)
                await RunAction(isAlly ? "/api/ops/scout-allies" : "/api/ops/scout", new { players = string.Join(",", ids) });
        }, true);
        buttons.Children.Add(rescout);
        controls.Children.Add(buttons);
        host.Children.Add(controls);
        return host;
    }

    private UIElement BuildScoutView() => BuildTacticalScoutView();
    private async Task LoadScoutAsync()
    {
        if (loadingScout || core.BaseUri == null) return;
        loadingScout = true;
        try
        {
            var tasks = new[] { core.GetAsync("/api/stats"), core.GetAsync("/api/players") };
            var result = await Task.WhenAll(tasks);
            stats = result[0];
            players = result[1];
            lastScoutLoad = DateTime.UtcNow;
            if (page == "Разведка")
            {
                UpdateScoutRoster();
                UpdateKnownPlayers();
                RenderScoutedPlayers();
                RenderScoutDetails();
            }
        }
        catch (Exception ex)
        {
            if (page == "Разведка")
                toast.Text = ex is TimeoutException ? "Steam-статистика временно занята." : "Список: " + ex.Message;
        }
        finally { loadingScout = false; }
    }

    private string PlayerName(long id)
    {
        if (id <= 0) return "Не назначен";
        var p = players.At("players").Items().FirstOrDefault(p => p.At("accountId").L() == id);
        return string.IsNullOrWhiteSpace(p.At("persona").S()) ? $"ID {id}" : p.At("persona").S();
    }

    private void UpdateScoutRoster()
    {
        if (page != "Разведка" || state == null) return;
        var version = string.Join("|", new[] { "our", "enemy" }.SelectMany(side =>
            state.At("team",side).Items().Select(id => side + ":" + id.L()))) +
            "|" + stats?.ToJsonString()?.GetHashCode() +
            "|" + players?.ToJsonString()?.GetHashCode();
        if (version == scoutVersion) return;
        scoutVersion = version;
        var ourIds = state.At("team","our").Items().Count(id => id.L() > 0);
        var enemyIds = state.At("team","enemy").Items().Count(id => id.L() > 0);
        allyMetric.Text = $"{ourIds} / 5";
        enemyMetric.Text = $"{enemyIds} / 5";
        foreach (var side in new[] { "our", "enemy" })
        {
            var ids = state.At("team",side).Items().ToArray();
            var scouted = stats.At("stats", side == "our" ? "allyPlayers" : "enemyPlayers").Items().ToArray();
            for (int pos = 1; pos <= 5; pos++)
            {
                var key = $"{side}:{pos}";
                var id = pos <= ids.Length ? ids[pos - 1]?.L() ?? 0 : 0;
                if (!rosterLabels.TryGetValue(key, out var label)) continue;
                label.Text = $"{pos}. {PlayerName(id)}";
                var p = scouted.FirstOrDefault(a => a.At("accountId").L() == id);
                rosterBadges[key].Text = id == 0 ? "" :
                    p == null ? "Ещё не разведан" : p.At("scoutStatus").S("Данные загружены");
                rosterHeroes[key].Text = p == null ? "" :
                    string.Join(" · ", p.At("topHeroes").Items().Take(2).Select(h => $"{h.At("heroId").S()} · {h.At("games").I()} игр"));
                var img = rosterAvatars[key];
                img.Source = null;
                if (id > 0 && core.BaseUri != null)
                {
                    try
                    {
                        var uri = new Uri(core.BaseUri, $"assets/avatars/{id}.jpg");
                        var bmp = new BitmapImage();
                        bmp.BeginInit();
                        bmp.UriSource = uri;
                        bmp.CacheOption = BitmapCacheOption.OnLoad;
                        bmp.EndInit();
                        img.Source = bmp;
                    }
                    catch (Exception) { /* Text fallback remains. */ }
                }
            }
        }
        RefreshScoutSignals();
    }

    private void ShowScoutDetails(string side, long id)
    {
        if (id <= 0) return;
        selectedScoutSide = side;
        selectedScoutId = id;
        RenderScoutedPlayers();
        RenderScoutDetails();
        scoutSignalKey = "";
        RefreshScoutSignals();
        if (scoutDetails != null)
            Dispatcher.BeginInvoke(() => scoutDetails.BringIntoView());
    }

    private void RenderScoutedPlayers()
    {
        if (page != "Разведка" || scoutedPlayers == null) return;
        scoutedPlayers.Children.Clear();
        foreach (var side in new[] { "our", "enemy" })
        {
            var rows = stats.At("stats", side == "our" ? "allyPlayers" : "enemyPlayers").Items().ToArray();
            foreach (var p in rows.Take(20))
            {
                var id = p.At("accountId").L();
                if (id <= 0) continue;
                var resolvedSide = side;
                var caption = p.At("nickname").S();
                if (string.IsNullOrWhiteSpace(caption)) caption = PlayerName(id);
                var label = (side == "our" ? "● " : "◆ ") + caption;
                var btn = Btn(label, async () =>
                {
                    ShowScoutDetails(resolvedSide, id);
                    await Task.CompletedTask;
                }, true);
                btn.Margin = new Thickness(0, 0, 8, 8);
                btn.Background = selectedScoutSide == side && selectedScoutId == id
                    ? Hex("#31565C") : soft;
                scoutedPlayers.Children.Add(btn);
                if (selectedScoutId == 0)
                {
                    selectedScoutSide = side;
                    selectedScoutId = id;
                }
            }
        }
        if (scoutedPlayers.Children.Count == 0)
            scoutedPlayers.Children.Add(Text("Пока никого не разведали. Добавь Steam ID или обнови состав выше.",
                13, color: muted));
    }

    private void RenderScoutDetails()
    {
        if (page != "Разведка" || scoutDetails == null) return;
        scoutDetails.Children.Clear();
        if (selectedScoutId <= 0)
        {
            scoutDetails.Children.Add(Text("Нет выбранного игрока", 17, FontWeights.SemiBold));
            scoutDetails.Children.Add(Text("Здесь появятся герои, количество игр, винрейт и статус источника.",
                12, color: muted));
            return;
        }
        var side = selectedScoutSide;
        var entry = stats.At("stats", side == "our" ? "allyPlayers" : "enemyPlayers")
            .Items().FirstOrDefault(p => p.At("accountId").L() == selectedScoutId);
        var head = new DockPanel { LastChildFill = true };
        var rescout = Btn("↻  Повторить разведку", async () =>
        {
            await RunAction(side == "our" ? "/api/ops/scout-allies" : "/api/ops/scout",
                new { players = selectedScoutId.ToString() });
        });
        DockPanel.SetDock(rescout, Dock.Right);
        head.Children.Add(rescout);
        var title = new StackPanel();
        title.Children.Add(Text(side == "our" ? "НАШ ИГРОК" : "ПРОТИВНИК", 10, FontWeights.Bold,
            side == "our" ? accent : gold));
        title.Children.Add(Text(PlayerName(selectedScoutId), 20, FontWeights.Bold));
        title.Children.Add(Text("Steam account: " + selectedScoutId, 11, color: muted));
        head.Children.Add(title);
        scoutDetails.Children.Add(head);

        if (entry == null)
        {
            var missing = Text("Данные героя ещё не загружены. Проверь публичность профиля или запусти разведку.",
                13, color: muted);
            missing.Margin = new Thickness(0, 17, 0, 0);
            scoutDetails.Children.Add(missing);
            return;
        }
        var current = entry.At("scoutStatus").S("unknown");
        var friendly = current switch
        {
            "ok" => "Данные получены",
            "no_stats" => "Нет публичной статистики",
            "error" => "Ошибка запроса — сохранены старые данные",
            "stale" => "Данные могут быть устаревшими",
            "unresolved" => "Steam ID не определён",
            _ => "Статус: " + current
        };
        var status = Text(friendly, 12, FontWeights.SemiBold, current == "ok" ? good : gold);
        status.Margin = new Thickness(0, 14, 0, 14);
        scoutDetails.Children.Add(status);

        var heroes = entry.At("topHeroes").Items().Take(8).ToArray();
        if (heroes.Length == 0)
        {
            scoutDetails.Children.Add(Text("Нет статистики по героям для этого аккаунта.", 13, color: muted));
            return;
        }
        var gallery = new WrapPanel();
        foreach (var hero in heroes)
        {
            var id = hero.At("id").I();
            if (id <= 0) continue;
            var item = new StackPanel();
            item.Children.Add(HeroPortrait(id, 130, 75));
            var name = Text(HeroName(id), 12, FontWeights.SemiBold);
            name.TextTrimming = TextTrimming.CharacterEllipsis;
            name.TextWrapping = TextWrapping.NoWrap;
            name.Width = 130;
            name.Margin = new Thickness(0, 7, 0, 0);
            item.Children.Add(name);
            item.Children.Add(Text($"{hero.At("games").I()} игр · {hero.At("winRate").D():P0} побед",
                11, color: muted));
            var affinity = hero.At("aff").D();
            if (affinity != 0) item.Children.Add(Text($"Комфорт: {affinity:0.00}", 11, color: accent));
            gallery.Children.Add(new Border { Child = item, Background = Hex("#203044"),
                BorderBrush = border, BorderThickness = new Thickness(1),
                CornerRadius = new CornerRadius(10), Padding = new Thickness(10),
                Margin = new Thickness(0, 0, 10, 10) });
        }
        scoutDetails.Children.Add(gallery);
    }

    private void UpdateKnownPlayers()
    {
        if (page != "Разведка") return;
        var list = knownPlayersList;
        if (list == null) return;
        // The API is polled every few seconds while scouting. Avoid resetting
        // ItemsSource unless a Steam persona or player list actually changed.
        var version = players?.ToJsonString() ?? "";
        if (version == playersVersion) return;
        playersVersion = version;
        knownPlayerRows.Clear();
        knownPlayerRows.AddRange(players.At("players").Items()
            .Select(x => new PlayerRow { Id = x.At("accountId").L(), Role = x.At("role").S(),
                Name = x.At("persona").S("Steam ID"), Pinned = x.At("pinned").B() })
            .Where(x => x.Id > 0).OrderByDescending(x => x.Pinned).ThenBy(x => x.Name));
        FilterKnownPlayers();
    }

    private void FilterKnownPlayers()
    {
        if (knownPlayersList == null) return;
        var selected = (knownPlayersList.SelectedItem as PlayerRow)?.Id;
        var phrase = knownPlayerFilter.Trim();
        var rows = knownPlayerRows.Where(p => phrase.Length == 0 ||
            p.Name.Contains(phrase, StringComparison.CurrentCultureIgnoreCase) ||
            p.Role.Contains(phrase, StringComparison.CurrentCultureIgnoreCase) ||
            p.Id.ToString().Contains(phrase, StringComparison.Ordinal)).ToList();
        knownPlayersList.ItemsSource = rows;
        knownPlayersList.DisplayMemberPath = "Label";
        if (selected.HasValue)
            knownPlayersList.SelectedItem = rows.FirstOrDefault(x => x.Id == selected.Value);
        if (bookEmptyHint != null)
        {
            bookEmptyHint.Text = knownPlayerRows.Count == 0
                ? "Пока нет игроков. Добавь Steam ID в состав выше."
                : "По этому запросу никто не найден.";
            bookEmptyHint.Visibility = rows.Count == 0 ? Visibility.Visible : Visibility.Collapsed;
        }
    }

    private static ListBox? FindListBox(DependencyObject? node)
    {
        if (node == null) return null;
        if (node is ListBox list && list.Name == "KnownPlayers") return list;
        for (var i = 0; i < System.Windows.Media.VisualTreeHelper.GetChildrenCount(node); i++)
            if (FindListBox(System.Windows.Media.VisualTreeHelper.GetChild(node, i)) is { } found) return found;
        return null;
    }

    private async Task EditSeatAsync(string side, int position)
    {
        // WPF modal dialog, not an HTML prompt. Can enter a Steam ID or choose
        // an already known player from the address book.
        var dialog = new Window
        {
            Owner = this, Title = $"Позиция {position} — {side}", Width = 450, Height = 370,
            WindowStartupLocation = WindowStartupLocation.CenterOwner,
            Background = background, Foreground = foreground, ResizeMode = ResizeMode.NoResize
        };
        var host = Stack(Text("Выбери игрока из адресной книги", 17, FontWeights.Bold));
        host.Margin = new Thickness(18);
        var options = players.At("players").Items().Select(p => new PlayerRow {
            Id = p.At("accountId").L(), Name = p.At("persona").S("Steam ID"), Role = p.At("role").S()
        }).Where(p => p.Id > 0).ToList();
        var combo = new ComboBox { ItemsSource = options, DisplayMemberPath = "Label", Margin = new Thickness(0, 14, 0, 12), MinHeight = 35 };
        host.Children.Add(combo);
        host.Children.Add(Text("Или вставь account ID", 13, color: muted));
        var input = new TextBox { Margin = new Thickness(0, 6, 0, 12), Padding = new Thickness(8) };
        host.Children.Add(input);
        var place = Btn("Назначить", async () =>
        {
            var id = long.TryParse(input.Text.Trim(), out var parsed) ? parsed : (combo.SelectedItem as PlayerRow)?.Id ?? 0;
            if (id <= 0) return;
            await RunAction("/api/team/seat", new { side, position, accountId = id });
            dialog.Close();
        });
        host.Children.Add(place);
        var clear = Btn("Освободить позицию", async () =>
        {
            await RunAction("/api/team/seat", new { side, position, accountId = 0 });
            dialog.Close();
        }, true);
        clear.Margin = new Thickness(0, 10, 0, 0);
        host.Children.Add(clear);
        dialog.Content = host;
        dialog.ShowDialog();
    }

    private string HeroInternalName(int id)
        => state.At("heroes").Items().FirstOrDefault(h => h.At("id").I() == id).At("name").S();

    private FrameworkElement HeroPortrait(int id, double width, double height)
    {
        var host = new Grid { Width = width, Height = height, ClipToBounds = true };
        var name = HeroName(id);
        var words = name.Split(' ', StringSplitOptions.RemoveEmptyEntries);
        var monogram = words.Length > 1
            ? string.Concat(words.Take(2).Select(w => w.Substring(0, 1)))
            : name.Substring(0, Math.Min(2, name.Length));
        var fallback = new Border
        {
            Background = new LinearGradientBrush(HexColor("#29465A"),
                HexColor("#142534"), 35),
            Child = Text(monogram.ToUpperInvariant(), Math.Min(19, height * 0.35),
                FontWeights.Bold, Hex("#9CBBC8"))
        };
        ((TextBlock)fallback.Child).HorizontalAlignment = HorizontalAlignment.Center;
        host.Children.Add(fallback);
        var picture = new Image { Stretch = Stretch.UniformToFill, Width = width, Height = height };
        host.Children.Add(picture);
        artwork.Bind(picture, id, HeroInternalName(id));
        return host;
    }

    private UIElement DraftHeroTile(int id)
    {
        var tile = new StackPanel();
        tile.Children.Add(HeroPortrait(id, 126, 74));
        var label = Text(HeroName(id), 11, FontWeights.SemiBold);
        label.TextTrimming = TextTrimming.CharacterEllipsis;
        label.TextWrapping = TextWrapping.NoWrap;
        label.Width = 126;
        label.Margin = new Thickness(0, 6, 0, 0);
        tile.Children.Add(label);
        var choose = Btn("", async () =>
        {
            selectedHeroId = id;
            if (selectedHeroHint != null) selectedHeroHint.Text = "Выбран: " + HeroName(id);
            HighlightDraftSelection();
            if (applyDraftButton != null) applyDraftButton.IsEnabled = true;
            await Task.CompletedTask;
        }, true);
        choose.Content = tile;
        choose.Padding = new Thickness(4);
        choose.Width = 138;
        choose.Height = 116;
        choose.Margin = new Thickness(0, 0, 8, 8);
        choose.Tag = id;
        choose.ToolTip = HeroName(id);
        choose.Background = id == selectedHeroId ? Hex("#2B5A5E") : soft;
        choose.BorderBrush = id == selectedHeroId ? accent : border;
        return choose;
    }

    private void HighlightDraftSelection()
    {
        if (heroGallery == null) return;
        foreach (var button in heroGallery.Children.OfType<Button>())
        {
            bool selected = button.Tag is int id && id == selectedHeroId;
            button.Background = selected ? Hex("#2B5A5E") : soft;
            button.BorderBrush = selected ? accent : border;
        }
    }

    private UIElement RecommendationRow(int id, double score, int rank)
    {
        var row = new Grid { MinHeight = 44 };
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(24) });
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(49) });
        row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        var order = Text(rank.ToString("D2"), 11, FontWeights.Bold,
            rank == 1 ? accent : muted);
        row.Children.Add(order);
        var portrait = HeroPortrait(id, 44, 30);
        Grid.SetColumn(portrait, 1);
        row.Children.Add(portrait);
        var content = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
        var name = Text(HeroName(id), 12, FontWeights.SemiBold);
        name.TextWrapping = TextWrapping.NoWrap;
        name.TextTrimming = TextTrimming.CharacterEllipsis;
        content.Children.Add(name);
        content.Children.Add(Text("Оценка " + score.ToString("0.000"), 10,
            FontWeights.SemiBold, accent));
        Grid.SetColumn(content, 2);
        row.Children.Add(content);
        var button = Btn("", async () =>
        {
            SelectTacticalHero(id);
            await Task.CompletedTask;
        }, true);
        button.Content = row;
        button.Padding = new Thickness(5, 8, 5, 8);
        button.Margin = new Thickness(0, 0, 0, 7);
        button.ToolTip = "Выбрать " + HeroName(id) +
            " · оценка модели " + score.ToString("0.000");
        button.HorizontalContentAlignment = HorizontalAlignment.Stretch;
        return button;
    }

    private UIElement BuildDraftView() => BuildTacticalDraftView();
    private void UpdateDraft()
    {
        if (state == null || page != "Драфт") return;
        var m = state.At("meta");
        var step = state.At("step");
        if (draftSummary != null)
        {
            draftSummary.Text = !m.At("draftReady").B()
                ? m.At("draftError").S("Модель ещё не обучена. Открой раздел «Данные».")
                : $"Сторона: {m.At("ourTeam").S()}\nХод: {step.At("team").S("—")} · {step.At("action").S("—")}\nПатч: {m.At("patchTag").S("—")}" +
                  (state.At("modelUpdatePending").B() ? "\nНовая модель активируется в следующем драфте" : "");
        }
        RefreshDraftBoard();
        var recs = state.At("result","recs").Items().Take(6)
            .Select(x => (id: x.At("heroId").I(), score: x.At("score").D()))
            .Where(x => x.id > 0).ToList();
        var recKey = string.Join("|", recs.Select(x => x.id + ":" + x.score));
        if (draftRecommendations != null && recKey != recommendationKey)
        {
            draftRecommendations.Items.Clear();
            for (int i = 0; i < recs.Count; i++)
                draftRecommendations.Items.Add(RecommendationRow(recs[i].id, recs[i].score, i + 1));
            recommendationKey = recKey;
        }
        RefreshSelectedHeroDetail();
        var key = state.At("heroes")?.ToJsonString() + "|" + state.At("board")?.ToJsonString();
        if (key != heroCatalogKey)
        {
            heroCatalogKey = key;
            UpdateHeroes(heroSearchText);
        }
    }

    private string HeroName(int id) => state.At("names",id.ToString()).S(id.ToString());
    private void UpdateHeroes(string filter)
    {
        if (state == null || page != "Драфт") return;
        var taken = new HashSet<int>(new[] { "radiantPicks","direPicks","radiantBans","direBans" }
            .SelectMany(key => state.At("board",key).Items().Select(x=>x.I())));
        var all = state.At("heroes").Items()
            .Select(x=>x.At("id").I()).Where(x=>x>0 && !taken.Contains(x))
            .Distinct().Select(id => new HeroRow { Id = id, Name = HeroName(id) })
            .Where(h => h.Name.Contains(filter, StringComparison.CurrentCultureIgnoreCase))
            .OrderBy(h=>h.Name).ToList();
        if (heroResultsCount != null)
            heroResultsCount.Text = $"{all.Count} доступно";
        if (heroShowMore != null)
            heroShowMore.Visibility = all.Count > heroPageSize
                ? Visibility.Visible : Visibility.Collapsed;
        if (selectedHeroId > 0 && taken.Contains(selectedHeroId))
        {
            selectedHeroId = 0;
            if (applyDraftButton != null) applyDraftButton.IsEnabled = false;
            if (selectedHeroHint != null) selectedHeroHint.Text = "Выбери героя для следующего хода";
        }
        heroRows.Clear();
        foreach (var x in all) heroRows.Add(x);
        if (heroGallery == null) return;
        heroGallery.Children.Clear();
        if (all.Count == 0)
        {
            heroGallery.Children.Add(Text("Нет подходящих героев", 13, color: muted));
            return;
        }
        foreach (var h in all.Take(heroPageSize))
            heroGallery.Children.Add(TacticalHeroTile(h.Id));
    }

    private UIElement BuildDataView()
    {
        corpusMatches = new TextBlock();
        corpusDrafts = new TextBlock();
        corpusCoverage = new TextBlock();
        corpusHeroes = new TextBlock();
        var host = new StackPanel();
        host.Children.Add(Text("Данные и обучение модели", 24, FontWeights.Bold));
        var note = Text("Следи за корпусом матчей, запускай синхронизацию и обучение без ожидания на главном экране.", 13, color: muted);
        note.Margin = new Thickness(0, 7, 0, 20);
        host.Children.Add(note);

        var actions = new WrapPanel { Margin = new Thickness(0, 0, 0, 22) };
        var sync = Btn("↻  Собрать матчи", async () => await RunAction("/api/ops/sync"));
        sync.Margin = new Thickness(0, 0, 8, 8);
        actions.Children.Add(sync);
        var train = Btn("◇  Обучить модель", async () => await RunAction("/api/ops/train"));
        train.Margin = new Thickness(0, 0, 8, 8);
        actions.Children.Add(train);
        actions.Children.Add(Btn("Обновить метрики", LoadDataAsync, true));
        host.Children.Add(actions);

        var tiles = new WrapPanel();
        tiles.Children.Add(MetricTile("Всего матчей", corpusMatches, accent));
        tiles.Children.Add(MetricTile("Драфтов CM", corpusDrafts, gold));
        tiles.Children.Add(MetricTile("С позициями", corpusCoverage, good));
        tiles.Children.Add(MetricTile("Героев", corpusHeroes, muted));
        host.Children.Add(tiles);

        Header(host, "Очередь задач");
        var job = Surface(Stack(
            Text("АКТИВНОСТЬ СБОРЩИКА", 10, FontWeights.Bold, muted),
            Text("Процесс выполняется локально, в Go-ядре", 12, color: muted)
        ), new Thickness(0, 0, 0, 12));
        host.Children.Add(job);
        dataActivity = Text("Активных заданий нет", 13, color: accent);
        var activityBox = new Border { Child = dataActivity, Background = Hex("#132434"),
            BorderBrush = Hex("#365061"), BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(10), Padding = new Thickness(18, 15, 18, 15) };
        host.Children.Add(activityBox);

        Header(host, "Популярные герои в корпусе");
        var heading = Text("Топ пиков по сохранённым матчам", 12, color: muted);
        heading.Margin = new Thickness(0, 0, 0, 10);
        host.Children.Add(heading);
        var top = new ListBox { Name = "CorpusPicks", MaxHeight = 370, MinHeight = 120 };
        VirtualizingStackPanel.SetIsVirtualizing(top, true);
        host.Children.Add(top);
        return Scroll(host);
    }

    private async Task LoadDataAsync()
    {
        if (page != "Данные" || core.BaseUri == null) return;
        try
        {
            var result = await core.GetAsync("/api/corpus");
            var corpus = result.At("corpus");
            if (corpus == null)
            {
                toast.Text = result.At("error").S("Корпус пока недоступен");
                return;
            }
            if (page != "Данные") return; // stale response after navigation
            corpusMatches.Text = corpus.At("matches").L().ToString("N0");
            corpusDrafts.Text = corpus.At("cmDrafts").L().ToString("N0");
            corpusCoverage.Text = corpus.At("withPositions").L().ToString("N0");
            corpusHeroes.Text = corpus.At("heroes").L().ToString("N0");
            var top = FindNamedList(body.Content as DependencyObject, "CorpusPicks");
            if (top != null)
                top.ItemsSource = corpus.At("topPicks").Items().Take(12)
                    .Select((h, i) => $"{i + 1:D2}    {h.At("name").S("Герой")}    ·    {h.At("count").L():N0} пиков")
                    .ToList();
        }
        catch (Exception ex)
        {
            // A slow corpus refresh from a previous page must not leave an
            // unrelated error in the permanent footer of another view.
            if (page == "Данные")
            {
                toast.Text = ex is TimeoutException
                    ? "Данные временно заняты. Повтори обновление метрик."
                    : "Корпус: " + ex.Message;
            }
        }
    }

    private static ListBox? FindNamedList(DependencyObject? node, string name)
    {
        if (node == null) return null;
        if (node is ListBox list && list.Name == name) return list;
        for (int i = 0; i < VisualTreeHelper.GetChildrenCount(node); i++)
            if (FindNamedList(VisualTreeHelper.GetChild(node, i), name) is { } found)
                return found;
        return null;
    }

    private static TextBlock? FindNamedText(DependencyObject? node, string name)
    {
        if (node == null) return null;
        if (node is TextBlock block && block.Name == name) return block;
        for (var i=0;i<VisualTreeHelper.GetChildrenCount(node);i++)
            if (FindNamedText(VisualTreeHelper.GetChild(node,i),name) is { } found) return found;
        return null;
    }
    private UIElement BuildSettingsView()
    {
        configEditors.Clear();
        var host = new StackPanel();
        Header(host, "Настройки Go-ядра");
        host.Children.Add(Text("Редактирование config.yaml через API Go-ядра. Изменённые поля автоматически включаются.", 13, color: muted));
        var dir = Text(core.DataDirectory, 13, color: accent);
        dir.Margin = new Thickness(0, 12, 0, 14);
        host.Children.Add(dir);
        var buttons = new WrapPanel();
        buttons.Children.Add(Btn("Открыть папку данных", () =>
        {
            System.Diagnostics.Process.Start(new System.Diagnostics.ProcessStartInfo("explorer.exe", $"\"{core.DataDirectory}\"") { UseShellExecute = true });
            return Task.CompletedTask;
        }, true));
        buttons.Children.Add(Btn("Сохранить настройки", SaveSettingsAsync));
        buttons.Children.Add(Btn("Отменить изменения", LoadSettingsAsync, true));
        host.Children.Add(buttons);
        configHost = new StackPanel { Margin = new Thickness(0, 22, 0, 0) };
        host.Children.Add(configHost);
        return Scroll(host);
    }

    private async Task LoadSettingsAsync()
    {
        if (core.BaseUri == null || page != "Настройки" || configHost == null) return;
        try
        {
            var cfg = await core.GetAsync("/api/config");
            configEditors.Clear();
            configHost.Children.Clear();
            var value = cfg.At("config");
            if (value == null) { configHost.Children.Add(Text(cfg.At("error").S("Конфигурация недоступна"), 15, color: accent)); return; }
            foreach (var section in value.At("sections").Items())
            {
                var name = section.At("name").S();
                var box = new StackPanel { Margin = new Thickness(0, 0, 0, 20) };
                Header(box, section.At("title").S(name));
                foreach (var field in section.At("fields").Items())
                {
                    var key = field.At("key").S();
                    if (key.StartsWith("alert-", StringComparison.Ordinal)) continue;
                    var type = field.At("type").S();
                    var editor = new ConfigField { Section = name, Key = key, Type = type };
                    var row = new Grid { Margin = new Thickness(0, 6, 0, 6) };
                    row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(32) });
                    row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(240) });
                    row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
                    var enabled = new CheckBox { IsChecked = field.At("set").B(), VerticalAlignment = VerticalAlignment.Center,
                        ToolTip = "Записать значение в config.yaml" };
                    editor.Enabled = enabled;
                    row.Children.Add(enabled);
                    var label = Text(key, 13);
                    Grid.SetColumn(label, 1); row.Children.Add(label);
                    FrameworkElement control;
                    if (type == "bool")
                    {
                        var checkbox = new CheckBox { IsChecked = field.At("value").B(), VerticalAlignment = VerticalAlignment.Center };
                        checkbox.Checked += (_, _) => enabled.IsChecked = true;
                        checkbox.Unchecked += (_, _) => enabled.IsChecked = true;
                        editor.Control = checkbox;
                        control = checkbox;
                    }
                    else
                    {
                        var input = new TextBox { Text = field.At("value").S(), Padding = new Thickness(7),
                            ToolTip = field.At("help").S(), MinWidth = 130 };
                        input.TextChanged += (_, _) => enabled.IsChecked = true;
                        editor.Control = input;
                        control = input;
                    }
                    Grid.SetColumn(control, 2); row.Children.Add(control);
                    box.Children.Add(row);
                    configEditors.Add(editor);
                }
                if (box.Children.Count > 1) configHost.Children.Add(Surface(box, new Thickness(0, 0, 0, 12)));
            }
        }
        catch (Exception ex) { toast.Text = "Настройки: " + ex.Message; }
    }

    private async Task SaveSettingsAsync()
    {
        var sections = configEditors.GroupBy(x => x.Section).Select(g => new
        {
            name = g.Key,
            fields = g.Select(f => new
            {
                key = f.Key,
                set = f.Enabled?.IsChecked == true,
                value = f.Value()
            }).ToArray()
        }).ToArray();
        await RunAction("/api/config/save", new { sections });
        await LoadSettingsAsync();
        toast.Text = "Настройки сохранены. Для части изменений требуется перезапуск.";
    }

    private sealed class ConfigField
    {
        public string Section { get; init; } = "";
        public string Key { get; init; } = "";
        public string Type { get; init; } = "";
        public CheckBox? Enabled { get; set; }
        public FrameworkElement? Control { get; set; }
        public object Value()
        {
            if (Control is CheckBox c) return c.IsChecked == true;
            var input = (Control as TextBox)?.Text ?? "";
            if (Type == "int") return int.TryParse(input, out var n) ? n : 0;
            if (Type == "float") return double.TryParse(input, System.Globalization.NumberStyles.Float,
                System.Globalization.CultureInfo.InvariantCulture, out var d) ? d : 0d;
            return input;
        }
    }

    private UIElement BuildShareView()
    {
        var host = new StackPanel();
        host.Children.Add(Text("COLLABORATION  /  05", 10, FontWeights.Bold, accent));
        var heading = Text("Драфт вместе с командой", 25, FontWeights.Bold);
        heading.Margin = new Thickness(0, 9, 0, 7);
        host.Children.Add(heading);
        var description = Text("Открой сессию для друзей или подключись к чужому драфту по коду.", 13, color: muted);
        description.Margin = new Thickness(0, 0, 0, 22);
        host.Children.Add(description);

        var stateBox = new StackPanel();
        stateBox.Children.Add(Text("ТЕКУЩАЯ СЕССИЯ", 10, FontWeights.Bold, muted));
        shareStatus = Text("Проверяем подключение…", 15, FontWeights.SemiBold, accent);
        shareStatus.Margin = new Thickness(0, 10, 0, 0);
        stateBox.Children.Add(shareStatus);
        host.Children.Add(Surface(stateBox, new Thickness(0, 0, 0, 18)));

        var cards = new Grid();
        cards.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        cards.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        cards.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        cards.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });

        var publish = new StackPanel();
        publish.Children.Add(Text("01  ·  ХОСТ", 10, FontWeights.Bold, accent));
        var hostTitle = Text("Поделиться драфтом", 19, FontWeights.SemiBold);
        hostTitle.Margin = new Thickness(0, 9, 0, 8);
        publish.Children.Add(hostTitle);
        publish.Children.Add(Text("Сгенерируй код комнаты и покажи союзникам актуальный драфт.", 13, color: muted));
        var publishActions = new WrapPanel { Margin = new Thickness(0, 18, 0, 0) };
        var open = Btn("Открыть сессию", async () => await RunAction("/api/share/start"));
        open.IsEnabled = false;
        open.ToolTip = "Публичные relay отключены в локальной версии. LAN sharing пока не реализован.";
        open.Margin = new Thickness(0, 0, 8, 8);
        publishActions.Children.Add(open);
        publishActions.Children.Add(Btn("Завершить", async () => await RunAction("/api/share/stop"), true));
        publish.Children.Add(publishActions);
        var publishCard = Surface(publish, new Thickness(0, 0, 9, 0));
        cards.Children.Add(publishCard);

        var join = new StackPanel();
        join.Children.Add(Text("02  ·  ГОСТЬ", 10, FontWeights.Bold, gold));
        var joinTitle = Text("Присоединиться", 19, FontWeights.SemiBold);
        joinTitle.Margin = new Thickness(0, 9, 0, 8);
        join.Children.Add(joinTitle);
        join.Children.Add(Text("Вставь код комнаты, который прислал лидер команды.", 13, color: muted));
        var codeInput = new TextBox { Margin = new Thickness(0, 16, 0, 9),
            ToolTip = "Код комнаты", MaxLength = 24 };
        join.Children.Add(codeInput);
        var receive = Btn("Получить драфт", async () =>
            await RunAction("/api/share/receive", new { code = codeInput.Text.Trim() }));
        receive.IsEnabled = false;
        receive.ToolTip = "Публичные relay отключены в локальной версии.";
        join.Children.Add(receive);
        var joinCard = Surface(join, new Thickness(9, 0, 0, 0));
        Grid.SetColumn(joinCard, 1);
        cards.Children.Add(joinCard);

        bool compactLayout = false;
        cards.SizeChanged += (_, _) =>
        {
            bool compact = cards.ActualWidth < 700;
            if (compact == compactLayout) return;
            compactLayout = compact;
            cards.ColumnDefinitions[1].Width = compact ? new GridLength(0) : new GridLength(1, GridUnitType.Star);
            Grid.SetColumn(joinCard, compact ? 0 : 1);
            Grid.SetRow(joinCard, compact ? 1 : 0);
            publishCard.Margin = compact ? new Thickness(0, 0, 0, 12) : new Thickness(0, 0, 9, 0);
            joinCard.Margin = compact ? new Thickness(0) : new Thickness(9, 0, 0, 0);
        };
        host.Children.Add(cards);

        var footnote = Text("Локальный режим: публичные relay отключены. Для обмена файлами используйте защищённый ручной канал; live LAN пока недоступен.", 12, color: muted);
        footnote.Margin = new Thickness(0, 19, 0, 0);
        host.Children.Add(footnote);
        return Scroll(host);
    }

    // Lightweight Windows UI smoke gate used by the native CI job. The backend
    // never starts because the window is not shown. It catches WPF logical
    // parent reuse, page/header mismatch and broken view construction.
    internal void AssertNavigationWorks()
    {
        foreach (var target in new[] { "Драфт", "Разведка", "Данные", "Разведка", "Драфт", "Настройки", "Друзья" })
        {
            var previous = body.Content;
            RenderPage(target);
            if (page != target || heading.Text != target || ReferenceEquals(previous, body.Content))
                throw new InvalidOperationException($"Native navigation smoke failed on {target}: visual content did not change.");
            if (!navMarkers.TryGetValue(target, out var nav) ||
                nav.Visibility != Visibility.Visible)
                throw new InvalidOperationException($"Navigation marker did not activate for {target}.");
            if (target == "Драфт" &&
                (draftBoardHost == null || heroGallery == null ||
                 tacticalSelection == null ||
                 heroSearchBox == null || draftRecommendations == null ||
                 applyDraftButton == null || applyDraftButton.IsEnabled))
                throw new InvalidOperationException("Draft cockpit controls missing or action incorrectly enabled.");
            if (target == "Разведка" &&
                (scoutSignalHost == null || scoutViewport == null ||
                 tacticalInsights == null ||
                 scoutedPlayers == null || scoutDetails == null))
                throw new InvalidOperationException("Scouting overview or detail drill-down missing.");
        }
    }

    private sealed class PlayerRow
    {
        public long Id { get; init; }
        public string Name { get; init; } = "";
        public string Role { get; init; } = "";
        public bool Pinned { get; init; }
        public string Label => $"{(Pinned ? "★ " : "")}{Name}  ·  {Role}  ·  {Id}";
    }
    private sealed class HeroRow
    {
        public int Id { get; init; }
        public string Name { get; init; } = "";
        public string Label => $"{Name}  ({Id})";
    }
}
