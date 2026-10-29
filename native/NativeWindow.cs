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
public sealed class NativeWindow : Window
{
    private readonly AdvisorCore core = new();
    private readonly Brush background = Hex("#101925");
    private readonly Brush panel = Hex("#1C2939");
    private readonly Brush soft = Hex("#26364A");
    private readonly Brush border = Hex("#34465E");
    private readonly Brush foreground = Hex("#E7E8EA");
    private readonly Brush muted = Hex("#A0B1C9");
    private readonly Brush accent = Hex("#E7B65A");
    private readonly Brush good = Hex("#76C9A6");

    private readonly ContentControl body = new();
    private readonly TextBlock status = new();
    private readonly TextBlock heading = new();
    private readonly Dictionary<string, Button> navigation = new();
    private string page = "Разведка";
    private JsonObject? state;
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
    private TextBlock? draftSummary;
    private ListBox? draftRecommendations;
    private readonly ObservableCollection<HeroRow> heroRows = new();
    private ListBox? heroList;
    private ListBox? knownPlayersList;
    private string heroCatalogKey = "";
    private TextBlock? dataActivity;
    private TextBlock? shareStatus;
    private StackPanel? configHost;
    private readonly List<ConfigField> configEditors = new();
    private readonly TextBlock toast = new();

    public NativeWindow(string? initialDataDirectory = null)
    {
        Title = "cmnet · Native Preview";
        Width = 1370;
        Height = 870;
        MinWidth = 890;
        MinHeight = 600;
        WindowStartupLocation = WindowStartupLocation.CenterScreen;
        Background = background;
        Foreground = foreground;
        FontFamily = new FontFamily("Segoe UI");
        UseLayoutRounding = true;
        SnapsToDevicePixels = true;

        var layout = new Grid();
        layout.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        layout.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        layout.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        Content = layout;

        var top = new DockPanel { Margin = new Thickness(20, 16, 20, 14), LastChildFill = true };
        var brand = Text("cmnet", 28, FontWeights.Bold, accent);
        brand.Margin = new Thickness(0, 0, 20, 0);
        DockPanel.SetDock(brand, Dock.Left);
        top.Children.Add(brand);
        var badge = Text("NATIVE  ·  Windows", 12, FontWeights.SemiBold, muted);
        badge.VerticalAlignment = VerticalAlignment.Center;
        DockPanel.SetDock(badge, Dock.Left);
        top.Children.Add(badge);
        heading.FontSize = 24;
        heading.FontWeight = FontWeights.SemiBold;
        heading.HorizontalAlignment = HorizontalAlignment.Right;
        heading.VerticalAlignment = VerticalAlignment.Center;
        top.Children.Add(heading);
        layout.Children.Add(top);

        var inner = new Grid();
        inner.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(192) });
        inner.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        Grid.SetRow(inner, 1);
        layout.Children.Add(inner);

        var sidebar = new StackPanel { Margin = new Thickness(12, 14, 8, 12) };
        foreach (var name in new[] { "Разведка", "Драфт", "Данные", "Настройки", "Друзья" })
        {
            var item = Btn(name, async () => await OpenPageAsync(name));
            item.HorizontalContentAlignment = HorizontalAlignment.Left;
            item.Margin = new Thickness(0, 0, 0, 9);
            item.Padding = new Thickness(15, 14, 12, 14);
            navigation[name] = item;
            sidebar.Children.Add(item);
        }
        var sideScroll = new ScrollViewer { VerticalScrollBarVisibility = ScrollBarVisibility.Auto, Content = sidebar };
        inner.Children.Add(sideScroll);

        var main = Surface(body, new Thickness(0, 0, 14, 10));
        Grid.SetColumn(main, 1);
        inner.Children.Add(main);

        status.Foreground = muted;
        status.FontSize = 12;
        status.Text = "Запускаем Go-ядро";
        toast.Foreground = accent;
        toast.FontSize = 12;
        var footer = new DockPanel { Margin = new Thickness(24, 2, 24, 12) };
        DockPanel.SetDock(status, Dock.Left);
        footer.Children.Add(status);
        toast.HorizontalAlignment = HorizontalAlignment.Right;
        footer.Children.Add(toast);
        Grid.SetRow(footer, 2);
        layout.Children.Add(footer);

        core.StateReceived += snapshot => Dispatcher.BeginInvoke(() => ApplyState(snapshot));
        core.ConnectionChanged += message => Dispatcher.BeginInvoke(() => status.Text = message);
        Loaded += async (_, _) => await BootAsync(initialDataDirectory);
        Closed += (_, _) => core.Stop();
        RenderPage();
    }

    private static Brush Hex(string code) => (Brush)new BrushConverter().ConvertFromString(code)!;
    private TextBlock Text(string value, double size = 14, FontWeight? weight = null, Brush? color = null)
        => new() { Text = value, FontSize = size, FontWeight = weight ?? FontWeights.Normal,
            Foreground = color ?? foreground, TextWrapping = TextWrapping.Wrap, VerticalAlignment = VerticalAlignment.Center };
    private Button Btn(string label, Func<Task> action, bool subtle = false)
    {
        var b = new Button
        {
            Content = label, Cursor = Cursors.Hand, Padding = new Thickness(12, 9, 12, 9),
            Background = subtle ? soft : panel, Foreground = foreground,
            BorderBrush = border, BorderThickness = new Thickness(1), MinHeight = 36
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
            Margin = margin, Padding = new Thickness(18), Child = child };
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
        var h = Text(label, 20, FontWeights.SemiBold);
        h.Margin = new Thickness(0, 5, 0, 13);
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
        page = name;
        RenderPage();
        if (page == "Разведка") await LoadScoutAsync();
        if (page == "Данные") await LoadDataAsync();
        if (page == "Настройки") await LoadSettingsAsync();
    }
    private void RenderPage()
    {
        heading.Text = page;
        foreach (var item in navigation)
        {
            item.Value.Background = item.Key == page ? Hex("#364862") : panel;
            item.Value.BorderBrush = item.Key == page ? accent : border;
        }
        body.Content = page switch
        {
            "Разведка" => BuildScoutView(),
            "Драфт" => BuildDraftView(),
            "Данные" => BuildDataView(),
            "Настройки" => BuildSettingsView(),
            "Друзья" => BuildShareView(),
            _ => Text(page)
        };
        if (state != null) UpdateLiveState();
    }
    private void ApplyState(JsonObject next)
    {
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
        await core.PostAsync(endpoint, payload ?? new { });
        toast.Text = "Команда отправлена";
        if (page == "Разведка") await LoadScoutAsync();
    }

    private StackPanel BuildSide(string side)
    {
        var host = new StackPanel();
        Header(host, side == "our" ? "Союзники" : "Соперники");
        for (int pos = 1; pos <= 5; pos++)
        {
            var key = $"{side}:{pos}";
            int position = pos;
            var row = new Grid { Margin = new Thickness(0, 0, 0, 9) };
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(48) });
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });

            var avatar = new Image { Height = 40, Width = 40, Stretch = Stretch.UniformToFill, Margin = new Thickness(0, 0, 8, 0) };
            rosterAvatars[key] = avatar;
            row.Children.Add(avatar);

            var names = new StackPanel();
            var label = Text($"Позиция {pos} — пусто", 15, FontWeights.SemiBold);
            var heroes = Text("", 11, color: muted);
            var badge = Text("", 11, color: accent);
            rosterLabels[key] = label;
            rosterHeroes[key] = heroes;
            rosterBadges[key] = badge;
            names.Children.Add(label);
            names.Children.Add(heroes);
            names.Children.Add(badge);
            Grid.SetColumn(names, 1);
            row.Children.Add(names);

            var choose = Btn("Изменить", async () => await EditSeatAsync(side, position), true);
            choose.FontSize = 12;
            choose.Padding = new Thickness(9, 5, 9, 5);
            choose.Margin = new Thickness(8, 0, 0, 0);
            Grid.SetColumn(choose, 2);
            row.Children.Add(choose);
            host.Children.Add(row);
        }
        var controls = new WrapPanel { Margin = new Thickness(0, 12, 0, 2) };
        var input = new TextBox { MinWidth = 210, Margin = new Thickness(0, 0, 8, 6),
            Padding = new Thickness(8), ToolTip = "Steam account ID, OpenDota URL или профиль" };
        controls.Children.Add(input);
        controls.Children.Add(Btn("Разведать ID", async () =>
        {
            var value = input.Text.Trim();
            if (value.Length > 0)
                await RunAction(side == "our" ? "/api/ops/scout-allies" : "/api/ops/scout", new { players = value });
        }));
        host.Children.Add(controls);
        var rescout = Btn("Обновить весь состав", async () =>
        {
            var ids = state.At("team", side).Items().Select(j => j.L()).Where(id => id > 0).Distinct().ToArray();
            if (ids.Length > 0)
                await RunAction(side == "our" ? "/api/ops/scout-allies" : "/api/ops/scout", new { players = string.Join(",", ids) });
        }, true);
        rescout.Margin = new Thickness(0, 8, 0, 0);
        host.Children.Add(rescout);
        return host;
    }

    private UIElement BuildScoutView()
    {
        rosterLabels.Clear();
        rosterAvatars.Clear();
        rosterBadges.Clear();
        rosterHeroes.Clear();
        scoutVersion = "";
        playersVersion = "";
        var host = new StackPanel();
        host.Children.Add(Text("Составы 5 × 5", 24, FontWeights.Bold));
        var note = Text("Позиции, Steam-профили и разведка. Карточки не пересоздаются при обновлении прогресса.", 13, color: muted);
        note.Margin = new Thickness(0, 5, 0, 18);
        host.Children.Add(note);
        var grid = new Grid();
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        var our = Surface(BuildSide("our"), new Thickness(0, 0, 8, 8));
        var theirs = Surface(BuildSide("enemy"), new Thickness(8, 0, 0, 8));
        // Surface is nested inside the root panel, using independent backgrounds.
        our.Background = soft; theirs.Background = soft;
        grid.Children.Add(our);
        Grid.SetColumn(theirs, 1);
        grid.Children.Add(theirs);
        host.Children.Add(grid);
        Header(host, "Активная разведка");
        scoutLive = Text("Ожидание данных…", 13, color: accent);
        scoutLive.Margin = new Thickness(0, 0, 0, 20);
        host.Children.Add(scoutLive);
        Header(host, "Адресная книга Steam");
        var refresh = Btn("Обновить список", LoadScoutAsync, true);
        refresh.Margin = new Thickness(0, 0, 0, 12);
        host.Children.Add(refresh);
        var book = new ListBox { Name = "KnownPlayers", MaxHeight = 350, BorderThickness = new Thickness(0), Background = panel,
            Foreground = foreground };
        knownPlayersList = book;
        VirtualizingStackPanel.SetIsVirtualizing(book, true);
        VirtualizingStackPanel.SetVirtualizationMode(book, VirtualizationMode.Recycling);
        host.Children.Add(book);
        return Scroll(host);
    }

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
            }
        }
        catch (Exception ex) { toast.Text = "Список: " + ex.Message; }
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
            "|" + stats?.ToJsonString()?.GetHashCode();
        if (version == scoutVersion) return;
        scoutVersion = version;
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
        var selected = (list.SelectedItem as PlayerRow)?.Id;
        var entries = players.At("players").Items()
            .Select(x => new PlayerRow { Id = x.At("accountId").L(), Role = x.At("role").S(), Name = x.At("persona").S("Steam ID"), Pinned = x.At("pinned").B() })
            .Where(x => x.Id > 0).ToList();
        list.ItemsSource = entries;
        list.DisplayMemberPath = "Label";
        if (selected is long id) list.SelectedItem = entries.FirstOrDefault(x => x.Id == id);
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

    private UIElement BuildDraftView()
    {
        var frame = new Grid();
        frame.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(310) });
        frame.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        var tools = new StackPanel { Margin = new Thickness(0, 0, 18, 0) };
        Header(tools, "Ход драфта");
        draftSummary = Text("Подключаем модель…", 15);
        draftSummary.Margin = new Thickness(0, 0, 0, 16);
        tools.Children.Add(draftSummary);
        var buttons = new WrapPanel();
        buttons.Children.Add(Btn("Отменить", async () => await RunAction("/api/undo"), true));
        buttons.Children.Add(Btn("Повторить", async () => await RunAction("/api/redo"), true));
        buttons.Children.Add(Btn("Новый", async () => await RunAction("/api/new-draft"), true));
        tools.Children.Add(buttons);
        Header(tools, "Рекомендации");
        draftRecommendations = new ListBox { Background = soft, Foreground = foreground, MinHeight = 250 };
        tools.Children.Add(draftRecommendations);
        var left = Scroll(tools);
        frame.Children.Add(left);
        var content = new StackPanel();
        Header(content, "Выбери героя");
        var search = new TextBox { Padding = new Thickness(10), Margin = new Thickness(0, 0, 0, 10) };
        search.TextChanged += (_, _) => UpdateHeroes(search.Text);
        content.Children.Add(search);
        heroList = new ListBox { ItemsSource = heroRows, DisplayMemberPath = "Label", Foreground = foreground, Background = soft };
        VirtualizingStackPanel.SetIsVirtualizing(heroList, true);
        VirtualizingStackPanel.SetVirtualizationMode(heroList, VirtualizationMode.Recycling);
        content.Children.Add(heroList);
        content.Children.Add(Btn("Применить выбранного героя", async () =>
        {
            if (heroList.SelectedItem is HeroRow h)
                await RunAction("/api/apply", new { heroId = h.Id });
        }));
        Grid.SetColumn(content, 1);
        frame.Children.Add(content);
        return frame;
    }

    private void UpdateDraft()
    {
        if (state == null || page != "Драфт") return;
        var m = state.At("meta");
        var step = state.At("step");
        if (draftSummary != null)
        {
            draftSummary.Text = !m.At("draftReady").B()
                ? m.At("draftError").S("Модель ещё не обучена. Открой раздел «Данные».")
                : $"Сторона: {m.At("ourTeam").S()}\nХод: {step.At("team").S("—")} · {step.At("action").S("—")}\nПатч: {m.At("patchTag").S("—")}";
        }
        if (draftRecommendations != null)
            draftRecommendations.ItemsSource = state.At("result","recs").Items().Take(6)
                .Select(x => $"{HeroName(x.At("heroId").I())} · {x.At("score").D():0.000}")
                .ToList();
        var key = string.Join("|",state.At("heroes").Items().Select(x=>x.ToJsonString()));
        if (key != heroCatalogKey)
        {
            heroCatalogKey = key;
            UpdateHeroes("");
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
        heroRows.Clear();
        foreach (var x in all) heroRows.Add(x);
    }

    private UIElement BuildDataView()
    {
        var host = new StackPanel();
        Header(host, "Модель и сбор матчей");
        var bar = new WrapPanel();
        bar.Children.Add(Btn("Собрать матчи", async () => await RunAction("/api/ops/sync")));
        bar.Children.Add(Btn("Обучить модель", async () => await RunAction("/api/ops/train")));
        bar.Children.Add(Btn("Обновить данные", LoadDataAsync, true));
        host.Children.Add(bar);
        Header(host, "Задания");
        dataActivity = Text("Нет активных заданий", 14, color: accent);
        host.Children.Add(dataActivity);
        var summary = new TextBlock { Name = "CorpusSummary", Margin = new Thickness(0, 15, 0, 0), TextWrapping = TextWrapping.Wrap,
            Foreground = foreground };
        host.Children.Add(summary);
        return Scroll(host);
    }
    private async Task LoadDataAsync()
    {
        if (page != "Данные" || core.BaseUri == null) return;
        try
        {
            var result = await core.GetAsync("/api/corpus");
            var text = "Корпус: " + result.At("corpus")?.ToJsonString();
            // Hide raw huge numeric arrays; metrics can be expanded in the full native migration.
            var summary = FindNamedText(body.Content as DependencyObject, "CorpusSummary");
            if (summary != null) summary.Text = text.Length > 1700 ? text[..1700] + "…" : text;
        }
        catch (Exception ex) { toast.Text = ex.Message; }
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
        Header(host,"Друзья и общий драфт");
        shareStatus=Text("Ожидание статуса…",15,color:accent);
        shareStatus.Margin=new Thickness(0,0,0,15);
        host.Children.Add(shareStatus);
        var buttons=new WrapPanel();
        buttons.Children.Add(Btn("Открыть раздачу",async()=>await RunAction("/api/share/start")));
        buttons.Children.Add(Btn("Закрыть раздачу",async()=>await RunAction("/api/share/stop")));
        host.Children.Add(buttons);
        Header(host,"Подключиться по коду");
        var code=new TextBox{Padding=new Thickness(12),MaxLength=24,MinWidth=210};
        host.Children.Add(code);
        host.Children.Add(Btn("Получить драфт",async()=>await RunAction("/api/share/receive",new{code=code.Text.Trim()})));
        return Scroll(host);
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
