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

        var header = new DockPanel { Margin = new Thickness(0, 0, 0, 9),
            LastChildFill = true };
        var badge = Pill("LIVE BOARD", Hex("#163B3B"), accent);
        DockPanel.SetDock(badge, Dock.Right);
        header.Children.Add(badge);
        header.Children.Add(Text("ПИКИ И БАНЫ  /  CAPTAINS MODE", 10,
            FontWeights.Bold, muted));
        draftBoardHost.Children.Add(header);

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
            Margin = new Thickness(0, 10, 0, 0) };
        for (int i = 0; i < 5; i++)
        {
            var heroId = i < picks.Length ? picks[i] : 0;
            var box = new Border {
                Background = Hex("#101D2B"),
                BorderBrush = heroId > 0 ? color : border,
                BorderThickness = new Thickness(1),
                CornerRadius = new CornerRadius(6),
                Margin = new Thickness(0, 0, 5, 0),
                Height = 40,
                ClipToBounds = true
            };
            if (heroId > 0)
            {
                var frame = new Grid();
                frame.Children.Add(HeroPortrait(heroId, 52, 40));
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
        var banStrip = new WrapPanel { Margin = new Thickness(0, 10, 0, 0) };
        foreach (var heroId in bans.Take(7))
        {
            var portrait = HeroPortrait(heroId, 35, 22);
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
            Background = Hex("#122235"),
            BorderBrush = Hex("#304155"),
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(10),
            Padding = new Thickness(12, 11, 7, 12)
        };
    }

    private void RefreshScoutSignals()
    {
        if (scoutSignalHost == null || page != "Разведка" || state == null) return;
        var ids = state.At("team", "enemy").Items().Select(x => x.L()).Take(5).ToArray();
        var reports = stats.At("stats", "enemyPlayers").Items().ToArray();
        var key = string.Join("|", ids) + "|" +
            string.Join("|", reports.Select(p => p.ToJsonString())) + "|" +
            players?.ToJsonString() + "|" + selectedScoutSide + ":" + selectedScoutId;
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
        var cards = new WrapPanel();
        for (int i = 0; i < 5; i++)
        {
            var id = i < ids.Length ? ids[i] : 0L;
            var report = id == 0 ? null :
                reports.FirstOrDefault(p => p.At("accountId").L() == id);
            var source = id == 0 ? "empty" :
                report?.At("scoutStatus").S("pending") ?? "pending";
            bool valid = source == "ok";
            bool selected = id > 0 && selectedScoutId == id &&
                selectedScoutSide == "enemy";
            var tone = source switch {
                "ok" => good,
                "error" => Hex("#EF7D88"),
                "no_stats" or "stale" or "unresolved" => gold,
                _ => muted
            };
            var statusLabel = source switch {
                "ok" => "ГОТОВО",
                "no_stats" => "СКРЫТО",
                "stale" => "УСТАРЕЛО",
                "error" => "ОШИБКА",
                "unresolved" => "НЕ ОПОЗНАН",
                "empty" => "НЕТ ИГРОКА",
                _ => "ОЖИДАНИЕ"
            };
            var content = new StackPanel();
            var statusRow = new DockPanel { LastChildFill = true };
            var seat = Text("POS " + (i + 1), 10, FontWeights.Bold, muted);
            DockPanel.SetDock(seat, Dock.Right);
            statusRow.Children.Add(seat);
            statusRow.Children.Add(Text("● " + statusLabel, 10, FontWeights.Bold, tone));
            content.Children.Add(statusRow);

            var portrait = new Grid { Width = 48, Height = 48, Margin = new Thickness(0, 14, 0, 9) };
            portrait.Children.Add(new Border {
                Background = Hex("#263E53"), CornerRadius = new CornerRadius(24),
                Child = new TextBlock {
                    Text = id == 0 ? "?" : (i + 1).ToString(),
                    HorizontalAlignment = HorizontalAlignment.Center,
                    VerticalAlignment = VerticalAlignment.Center,
                    Foreground = muted, FontSize = 17, FontWeight = FontWeights.Bold
                }
            });
            if (id > 0 && core.BaseUri != null)
            {
                var image = new Image { Width = 48, Height = 48, Stretch = Stretch.UniformToFill };
                try
                {
                    image.Source = new System.Windows.Media.Imaging.BitmapImage(
                        new Uri(core.BaseUri, "assets/avatars/" + id + ".jpg"));
                    var clip = new System.Windows.Media.EllipseGeometry(
                        new Point(24, 24), 24, 24);
                    image.Clip = clip;
                }
                catch (Exception) { /* fallback initials */ }
                portrait.Children.Add(image);
            }
            portrait.HorizontalAlignment = HorizontalAlignment.Center;
            content.Children.Add(portrait);

            var name = Text(id == 0 ? "Место пусто" : PlayerName(id),
                13, FontWeights.Bold, foreground);
            name.TextAlignment = TextAlignment.Center;
            name.TextWrapping = TextWrapping.NoWrap;
            name.TextTrimming = TextTrimming.CharacterEllipsis;
            name.Width = 144;
            content.Children.Add(name);

            var strong = report?.At("topHeroes").Items().Take(2).ToArray() ??
                Array.Empty<System.Text.Json.Nodes.JsonNode?>();
            var heroList = new StackPanel { Orientation = Orientation.Horizontal,
                HorizontalAlignment = HorizontalAlignment.Center,
                Margin = new Thickness(0, 11, 0, 4) };
            foreach (var hero in strong)
            {
                var heroId = hero.At("id").I();
                if (heroId <= 0) continue;
                var thumb = HeroPortrait(heroId, 54, 31);
                thumb.Margin = new Thickness(0, 0, 5, 0);
                thumb.ToolTip = HeroName(heroId);
                heroList.Children.Add(thumb);
            }
            if (heroList.Children.Count == 0)
                heroList.Children.Add(Text("Нет героев в выборке", 10, color: muted));
            content.Children.Add(heroList);

            var hint = Text(id > 0 ? "Подробнее  ›" : "Назначить в составах",
                10, FontWeights.SemiBold, id > 0 ? accent : muted);
            hint.HorizontalAlignment = HorizontalAlignment.Center;
            hint.Margin = new Thickness(0, 5, 0, 0);
            content.Children.Add(hint);
            var account = id;
            var button = Btn("", async () =>
            {
                if (account > 0) ShowScoutDetails("enemy", account);
                await System.Threading.Tasks.Task.CompletedTask;
            }, true);
            button.Content = content;
            button.HorizontalContentAlignment = HorizontalAlignment.Stretch;
            button.Width = 177;
            button.Padding = new Thickness(11, 11, 11, 12);
            button.Margin = new Thickness(0, 0, 9, 8);
            button.Background = selected ? Hex("#1D4149") : Hex("#172838");
            button.BorderBrush = selected ? accent : border;
            button.ToolTip = id > 0 ? "Профиль Steam account ID: " + id :
                "Добавить игрока можно в «Управление составами»";
            button.IsEnabled = id > 0;
            cards.Children.Add(button);
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
        var notice = Text("CMNET не выводит риск, рейтинг или уверенность без достоверной метрики.",
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
