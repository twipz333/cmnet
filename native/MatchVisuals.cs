using System;
using System.Linq;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Controls.Primitives;
using System.Windows.Media;

namespace Cmnet.Native;

// Native match-specific visualizations. No estimates or invented scouting
// results: every card is derived from the current Go snapshot.
public sealed partial class NativeWindow
{
    private StackPanel? draftBoardHost;
    private StackPanel? scoutSignalHost;
    private string draftBoardKey = "";
    private string scoutSignalKey = "";

    private void RefreshDraftBoard()
    {
        if (draftBoardHost == null || state == null || page != "Драфт") return;
        var nextKey = state.At("board")?.ToJsonString() + "|" +
            state.At("step")?.ToJsonString();
        if (nextKey == draftBoardKey) return;
        draftBoardKey = nextKey;
        draftBoardHost.Children.Clear();

        var radiantPicks = state.At("board", "radiantPicks").Items()
            .Select(x => x.I()).Where(x => x > 0).ToArray();
        var direPicks = state.At("board", "direPicks").Items()
            .Select(x => x.I()).Where(x => x > 0).ToArray();
        var radiantBans = state.At("board", "radiantBans").Items()
            .Select(x => x.I()).Where(x => x > 0).ToArray();
        var direBans = state.At("board", "direBans").Items()
            .Select(x => x.I()).Where(x => x > 0).ToArray();

        var teams = new Grid();
        teams.ColumnDefinitions.Add(new ColumnDefinition {
            Width = new GridLength(1, GridUnitType.Star) });
        teams.ColumnDefinitions.Add(new ColumnDefinition {
            Width = new GridLength(1, GridUnitType.Star) });
        var radiant = DraftBoardSide("RADIANT", radiantPicks, radiantBans, accent);
        radiant.Margin = new Thickness(0, 0, 6, 0);
        var dire = DraftBoardSide("DIRE", direPicks, direBans, gold);
        dire.Margin = new Thickness(6, 0, 0, 0);
        teams.Children.Add(radiant);
        Grid.SetColumn(dire, 1);
        teams.Children.Add(dire);
        draftBoardHost.Children.Add(teams);
    }

    private Border DraftBoardSide(string team, int[] picks, int[] bans, Brush color)
    {
        var host = new StackPanel();
        var line = new DockPanel { LastChildFill = true };
        var banned = Text($"БАНЫ  {bans.Length}", 10, FontWeights.SemiBold, muted);
        DockPanel.SetDock(banned, Dock.Right);
        line.Children.Add(banned);
        line.Children.Add(Text($"{team}  ·  {picks.Length}/5", 11,
            FontWeights.Bold, color));
        host.Children.Add(line);

        var slots = new UniformGrid { Rows = 1, Columns = 5,
            Margin = new Thickness(0, 7, 0, 0) };
        for (int i = 0; i < 5; i++)
        {
            var heroId = i < picks.Length ? picks[i] : 0;
            var box = new Border {
                Background = (Brush)FindResource("CanvasBrush"),
                BorderBrush = heroId > 0 ? color : border,
                BorderThickness = new Thickness(1),
                CornerRadius = new CornerRadius(6),
                Margin = new Thickness(0, 0, 5, 0),
                Height = 35,
                ClipToBounds = true
            };
            if (heroId > 0)
            {
                var frame = new Grid();
                var pickPortrait = HeroPortrait(heroId, 50, 35);
                pickPortrait.Width = double.NaN;
                if (pickPortrait is Grid portraitGrid)
                    foreach (var image in portraitGrid.Children.OfType<Image>()) image.Width = double.NaN;
                pickPortrait.HorizontalAlignment = HorizontalAlignment.Stretch;
                frame.Children.Add(pickPortrait);
                box.Child = frame;
                box.ToolTip = HeroName(heroId);
            }
            else
            {
                var label = Text((i + 1).ToString("D2"), 10,
                    FontWeights.Bold, muted);
                label.HorizontalAlignment = HorizontalAlignment.Center;
                box.Child = label;
                box.ToolTip = "Ожидает пика";
            }
            slots.Children.Add(box);
        }
        host.Children.Add(slots);
        var banStrip = new WrapPanel { Margin = new Thickness(0, 6, 0, 0) };
        foreach (var heroId in bans.Take(7))
        {
            var portrait = HeroPortrait(heroId, 30, 18);
            portrait.Opacity = 0.58;
            portrait.ToolTip = "Бан: " + HeroName(heroId);
            var frame = new Border {
                Child = portrait,
                Margin = new Thickness(0, 0, 5, 0),
                BorderBrush = border,
                BorderThickness = new Thickness(1),
                CornerRadius = new CornerRadius(3),
                ClipToBounds = true
            };
            banStrip.Children.Add(frame);
        }
        if (bans.Length == 0)
            banStrip.Children.Add(Text("Баны пока не выбраны", 10, color: muted));
        host.Children.Add(banStrip);
        return new Border {
            Child = host,
            Background = (Brush)FindResource("PanelBrush"),
            BorderBrush = (Brush)FindResource("BorderBrush"),
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(10),
            Padding = new Thickness(9, 8, 6, 8)
        };
    }

    private void RefreshScoutSignals()
    {
        if (scoutSignalHost == null || page != "Разведка" || state == null || stats == null) return;
        var ids = state.At("team", "enemy").Items().Select(x => x.L()).Take(5).ToArray();
        var reports = stats.At("stats", "enemyPlayers").Items().ToArray();
        var key = string.Join("|", ids) + "|" +
            string.Join("|", reports.Select(p => p?.ToJsonString() ?? "")) + "|" +
            (players?.ToJsonString() ?? "") + "|" + compactScout + "|" + selectedScoutSide + ":" + selectedScoutId;
        if (key == scoutSignalKey) return;
        scoutSignalKey = key;

        var found = ids.Count(id => id > 0);
        var ready = 0;
        var limited = 0;
        var errors = 0;
        foreach (var id in ids.Where(id => id > 0))
        {
            var source = reports.FirstOrDefault(p => p.At("accountId").L() == id)
                ?.At("scoutStatus").S("pending") ?? "pending";
            if (source == "ok") ready++;
            else if (source is "no_stats" or "stale" or "unresolved") limited++;
            else if (source == "error") errors++;
        }
        if (scoutFoundCount != null) scoutFoundCount.Text = found + " / 5";
        if (scoutReadyCount != null) scoutReadyCount.Text = ready + " / " + found;
        if (scoutLimitedCount != null) scoutLimitedCount.Text = limited.ToString();
        if (scoutFailureCount != null) scoutFailureCount.Text = errors.ToString();

        scoutSignalHost.Children.Clear();
        var cards = new Grid { Name = "OpponentCardGrid" };
        for (int column = 0; column < 5; column++)
            cards.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        for (int i = 0; i < 5; i++)
        {
            var id = i < ids.Length ? ids[i] : 0L;
            var report = id == 0 ? null :
                reports.FirstOrDefault(p => p.At("accountId").L() == id);
            var source = id == 0 ? "empty" :
                report?.At("scoutStatus").S("pending") ?? "pending";
            
            bool selected = id > 0 && selectedScoutId == id &&
                selectedScoutSide == "enemy";
            var tone = source switch {
                "ok" => good,
                "error" => (Brush)FindResource("ErrorBrush"),
                "no_stats" or "stale" or "unresolved" => gold,
                _ => muted
            };
            var statusLabel = source switch {
                "ok" => "ГОТОВО",
                "no_stats" => "НЕТ СТАТИСТИКИ",
                "stale" => "УСТАРЕЛО",
                "error" => "ОШИБКА",
                "unresolved" => "НЕ ОПОЗНАН",
                "empty" => "НЕТ ИГРОКА",
                _ => "ОЖИДАНИЕ"
            };
            var content = new StackPanel();
            var statusRow = new DockPanel { LastChildFill = true };
            var seat = Text("#" + (i + 1), 10, FontWeights.Bold, muted);
            DockPanel.SetDock(seat, Dock.Right);
            statusRow.Children.Add(seat);
            var statusText = Text("● " + statusLabel, 9, FontWeights.SemiBold, tone);
            statusText.TextWrapping = TextWrapping.NoWrap;
            statusText.TextTrimming = TextTrimming.CharacterEllipsis;
            statusText.ToolTip = statusLabel;
            statusRow.Children.Add(statusText);
            content.Children.Add(statusRow);

            var portrait = new Grid { Width = 42, Height = 42, Margin = new Thickness(0, 8, 0, 7) };
            portrait.Children.Add(new Border {
                Background = (Brush)FindResource("RaisedBrush"), CornerRadius = new CornerRadius(21),
                Child = new TextBlock {
                    Text = id == 0 ? "?" :
                        (PlayerName(id).Length > 0 ? PlayerName(id).Substring(0, 1).ToUpperInvariant() : "?"),
                    HorizontalAlignment = HorizontalAlignment.Center,
                    VerticalAlignment = VerticalAlignment.Center,
                    Foreground = muted, FontSize = 17, FontWeight = FontWeights.Bold
                }
            });
            if (id > 0 && core.BaseUri != null)
            {
                var image = new Image { Width = 42, Height = 42, Stretch = Stretch.UniformToFill };
                try
                {
                    image.Source = new System.Windows.Media.Imaging.BitmapImage(
                        new Uri(core.BaseUri, "assets/avatars/" + id + ".jpg"));
                    var clip = new System.Windows.Media.EllipseGeometry(
                        new Point(21, 21), 21, 21);
                    image.Clip = clip;
                }
                catch (Exception) { /* fallback initials */ }
                portrait.Children.Add(image);
            }
            portrait.HorizontalAlignment = HorizontalAlignment.Center;
            portrait.Visibility = compactScout ? Visibility.Collapsed : Visibility.Visible;
            content.Children.Add(portrait);

            var name = Text(id == 0 ? "Место пусто" : PlayerName(id),
                13, FontWeights.Bold, foreground);
            name.Margin = new Thickness(0, compactScout ? 4 : 0, 0, 0);
            name.TextAlignment = TextAlignment.Center;
            name.TextWrapping = TextWrapping.NoWrap;
            name.TextTrimming = TextTrimming.CharacterEllipsis;
            name.ToolTip = name.Text;
            content.Children.Add(name);

            var strong = report?.At("topHeroes").Items().Take(2).ToArray() ??
                Array.Empty<System.Text.Json.Nodes.JsonNode?>();
            var heroList = new StackPanel { Orientation = Orientation.Horizontal, MinHeight = 26,
                HorizontalAlignment = HorizontalAlignment.Center,
                Margin = new Thickness(0, compactScout ? 4 : 7, 0, 4) };
            foreach (var hero in strong)
            {
                var heroId = hero.At("id").I();
                if (heroId <= 0) continue;
                var thumb = HeroPortrait(heroId, 46, 26);
                thumb.Margin = new Thickness(0, 0, 5, 0);
                thumb.ToolTip = HeroName(heroId);
                heroList.Children.Add(thumb);
            }
            if (heroList.Children.Count == 0)
                heroList.Children.Add(Text("Нет героев в выборке", 10, color: muted));
            content.Children.Add(heroList);
            var top = strong.FirstOrDefault();
            var observed = top == null ? "Статистика недоступна" :
                $"{top.At("games").I()} игр · {top.At("winRate").D():P0} побед";
            var summary = Text(observed, 10, color: muted);
            summary.TextAlignment = TextAlignment.Center;
            summary.TextTrimming = TextTrimming.CharacterEllipsis;
            summary.TextWrapping = TextWrapping.NoWrap;
            summary.ToolTip = "Статистика первого известного героя: " + observed;
            content.Children.Add(summary);

            var hint = Text(id > 0 ? "Подробнее  ›" : "Назначить в составах",
                10, FontWeights.SemiBold, id > 0 ? accent : muted);
            hint.HorizontalAlignment = HorizontalAlignment.Center;
            hint.Margin = new Thickness(0, 5, 0, 0);
            hint.Visibility = compactScout ? Visibility.Collapsed : Visibility.Visible;
            content.Children.Add(hint);
            var account = id;
            var button = Btn("", async () =>
            {
                if (account > 0) ShowScoutDetails("enemy", account);
                await System.Threading.Tasks.Task.CompletedTask;
            }, true);
            button.Content = content;
            button.HorizontalContentAlignment = HorizontalAlignment.Stretch;
            button.VerticalContentAlignment = VerticalAlignment.Top;
            button.MinWidth = 0;
            button.Padding = new Thickness(7, compactScout ? 6 : 9, 7, compactScout ? 6 : 10);
            button.Margin = new Thickness(0, 0, i == 4 ? 0 : 6, 0);
            button.Background = selected ? (Brush)FindResource("SelectedBrush") : (Brush)FindResource("PanelBrush");
            button.BorderBrush = selected ? accent : border;
            button.ToolTip = id > 0 ? "Профиль Steam account ID: " + id :
                "Добавить игрока можно в «Управление составами»";
            button.IsEnabled = id > 0;
            Grid.SetColumn(button, i); cards.Children.Add(button);
        }
        scoutSignalHost.Children.Add(cards);
        if (tacticalInsights == null) return;
        tacticalInsights.Children.Clear();
        var visible = ids.Where(id => id > 0).Select(id =>
            reports.FirstOrDefault(p => p.At("accountId").L() == id))
            .Where(p => p != null).ToArray();
        var favorites = visible.Where(p => p!.At("scoutStatus").S() == "ok")
            .Select(p => p!.At("topHeroes").Items().FirstOrDefault()?.At("id").I() ?? 0)
            .Where(id => id > 0).ToArray();
        TacticalInsightRow("◎", "Источники разведки",
            ready + " подтверждено · " + limited + " ограничено · " + errors + " ошибок",
            accent);
        TacticalInsightRow("◇", "Сильнейшие известные герои",
            favorites.Length == 0 ? "Статистика героев не получена" :
            string.Join(", ", favorites.Distinct().Take(4).Select(HeroName)),
            gold);
        TacticalInsightRow("▤", "Неполные профили",
            (found - ready) == 0 ? "Для всех назначенных есть ответ источника" :
            "Без подтверждённых данных: " + (found - ready) +
            ". Не считай отсутствие статистики слабостью игрока.",
            muted);
        var overlaps = visible.SelectMany(p => p!.At("topHeroes").Items()
                .Select(h => h.At("id").I()).Where(id => id > 0).Distinct())
            .GroupBy(id => id).Where(g => g.Count() > 1).OrderByDescending(g => g.Count())
            .Select(g => HeroName(g.Key) + " · " + g.Count() + " игроков").Take(3).ToArray();
        TacticalInsightRow("↔", "Пересечения пулов",
            overlaps.Length == 0 ? "В доступной выборке общие герои не найдены" : string.Join(", ", overlaps), accent);
        var notice = Text("Статистика ограничена доступной выборкой источника.",
            11, color: muted);
        notice.Margin = new Thickness(0, 12, 0, 0);
        tacticalInsights.Children.Add(notice);
    }

    private void TacticalInsightRow(string glyph, string heading, string message, Brush tone)
    {
        if (tacticalInsights == null) return;
        var line = new Grid { Margin = new Thickness(0, 0, 0, 12) };
        line.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(34) });
        line.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        var icon = Text(glyph, 17, FontWeights.Bold, tone);
        line.Children.Add(icon);
        var texts = new StackPanel();
        texts.Children.Add(Text(heading, 12, FontWeights.SemiBold, foreground));
        var details = Text(message, 11, color: muted);
        details.Margin = new Thickness(0, 3, 0, 0);
        texts.Children.Add(details);
        Grid.SetColumn(texts, 1);
        line.Children.Add(texts);
        tacticalInsights.Children.Add(line);
    }
}
