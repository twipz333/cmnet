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
            state.At("step")?.ToJsonString() + "|" +
            state.At("meta", "previewDraftTimerSec").S() + "|" +
            state.At("status").S();
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
        teams.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(120) });
        teams.ColumnDefinitions.Add(new ColumnDefinition {
            Width = new GridLength(1, GridUnitType.Star) });

        var radiant = DraftBoardSide("RADIANT", radiantPicks, radiantBans, accent);
        radiant.Margin = new Thickness(0, 0, 6, 0);
        teams.Children.Add(radiant);

        var stepTeam = state.At("step", "team").S("—");
        var stepAction = state.At("step", "action").S("—");
        var turnTone = stepTeam.Equals("dire", StringComparison.OrdinalIgnoreCase)
            ? (Brush)FindResource("ErrorBrush") : accent;
        draftTimerLabel = Text("—", 22, FontWeights.Bold, foreground);
        draftTimerLabel.HorizontalAlignment = HorizontalAlignment.Center;
        draftTimerCaption = Text("таймер хода недоступен", 9, color: muted);
        draftTimerCaption.HorizontalAlignment = HorizontalAlignment.Center;
        draftTimerCaption.TextWrapping = TextWrapping.Wrap;
        draftTimerCaption.TextAlignment = TextAlignment.Center;
        // Honest timer: only SAMPLE/PREVIEW fixtures may ship previewDraftTimerSec.
        // Live GSI does not expose a captains-mode pick clock in the wire DTO.
        var previewTimer = state.At("meta", "previewDraftTimerSec").I();
        var isPreview = state.At("status").S().Contains("PREVIEW", StringComparison.OrdinalIgnoreCase)
            || state.At("status").S().Contains("SAMPLE", StringComparison.OrdinalIgnoreCase);
        if (isPreview && previewTimer > 0)
        {
            draftTimerLabel.Text = $"{previewTimer / 60}:{previewTimer % 60:D2}";
            draftTimerCaption.Text = "SAMPLE · демо-таймер";
            draftTimerCaption.Foreground = gold;
        }
        draftTurnLabel = Text(
            stepTeam.Equals("—", StringComparison.Ordinal) ? "Ход —" :
            "Ход " + char.ToUpperInvariant(stepTeam[0]) + stepTeam[1..] +
            (string.IsNullOrWhiteSpace(stepAction) || stepAction == "—" ? "" : " · " + stepAction),
            11, FontWeights.Bold, turnTone);
        draftTurnLabel.HorizontalAlignment = HorizontalAlignment.Center;
        draftTurnLabel.Margin = new Thickness(0, 4, 0, 0);
        var center = new StackPanel
        {
            VerticalAlignment = VerticalAlignment.Center,
            HorizontalAlignment = HorizontalAlignment.Center
        };
        center.Children.Add(draftTimerLabel);
        center.Children.Add(draftTimerCaption);
        center.Children.Add(draftTurnLabel);
        var centerFrame = new Border
        {
            Child = center,
            Background = ThemeBrush("RaisedBrush"),
            BorderBrush = border,
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(10),
            Padding = new Thickness(8, 10, 8, 10),
            Margin = new Thickness(0, 0, 6, 0)
        };
        Grid.SetColumn(centerFrame, 1);
        teams.Children.Add(centerFrame);

        var dire = DraftBoardSide("DIRE", direPicks, direBans, gold);
        Grid.SetColumn(dire, 2);
        teams.Children.Add(dire);
        draftBoardHost.Children.Add(teams);
    }

    private Border DraftBoardSide(string team, int[] picks, int[] bans, Brush color)
    {
        var host = new StackPanel();
        var line = new DockPanel { LastChildFill = true };
        var banned = Text($"Баны {bans.Length}/5", 10, FontWeights.SemiBold, muted);
        DockPanel.SetDock(banned, Dock.Right);
        line.Children.Add(banned);
        line.Children.Add(Text($"{team}  ·  пики {picks.Length}/5", 11,
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
        var hidden = 0;
        var errors = 0;
        foreach (var id in ids.Where(id => id > 0))
        {
            var source = reports.FirstOrDefault(p => p.At("accountId").L() == id)
                ?.At("scoutStatus").S("pending") ?? "pending";
            if (source == "ok") ready++;
            else if (source is "no_stats" or "unresolved") hidden++;
            else if (source == "error") errors++;
            else if (source == "stale") hidden++; // stale is incomplete evidence, not invented intel
        }
        if (scoutFoundCount != null) scoutFoundCount.Text = found + " / 5";
        if (scoutReadyCount != null)
            scoutReadyCount.Text = found == 0 ? "0 / 0" : ready + " / " + found;
        if (scoutLimitedCount != null) scoutLimitedCount.Text = hidden.ToString();
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
                "ok" => "Готов",
                "no_stats" => "Скрыто",
                "stale" => "Частично",
                "error" => "Ошибка",
                "unresolved" => "Не опознан",
                "empty" => "Пусто",
                _ => "Ожидание"
            };
            var content = new StackPanel();
            var statusBadge = new Border
            {
                HorizontalAlignment = HorizontalAlignment.Left,
                Background = (Brush)FindResource("RaisedBrush"),
                BorderBrush = tone,
                BorderThickness = new Thickness(1),
                CornerRadius = new CornerRadius(99),
                Padding = new Thickness(8, 2, 8, 2),
                Child = Text(statusLabel, 9, FontWeights.SemiBold, tone)
            };
            content.Children.Add(statusBadge);

            var portrait = new Grid { Width = 44, Height = 44, Margin = new Thickness(0, 10, 0, 8) };
            portrait.Children.Add(new Border {
                Background = (Brush)FindResource("RaisedBrush"), CornerRadius = new CornerRadius(22),
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
                var image = new Image { Width = 44, Height = 44, Stretch = Stretch.UniformToFill };
                try
                {
                    image.Source = new System.Windows.Media.Imaging.BitmapImage(
                        new Uri(core.BaseUri, "assets/avatars/" + id + ".jpg"));
                    image.Clip = new System.Windows.Media.EllipseGeometry(new Point(22, 22), 22, 22);
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

            var heroes = report?.At("topHeroes").Items()
                .Where(h => h.At("id").I() > 0).ToArray()
                ?? Array.Empty<System.Text.Json.Nodes.JsonNode?>();
            int observedGames = heroes.Sum(h => h!.At("games").I());
            double weightedWins = heroes.Sum(h => h!.At("games").I() * h!.At("winRate").D());
            // error/stale may retain prior topHeroes — never present as fresh ok WR.
            bool retainedCache = (source is "error" or "stale") && observedGames > 0;
            string wrText = observedGames > 0 ? (weightedWins / observedGames).ToString("P0") : "—";
            string gamesText = observedGames == 0 ? "нет игр"
                : retainedCache
                    ? (source == "stale" ? "устарело · " : "кэш · ") + observedGames + " игр"
                    : observedGames + " игр";
            var wr = Text(wrText, compactScout ? 14 : 18, FontWeights.Bold,
                observedGames > 0 && !retainedCache ? foreground : muted);
            wr.TextAlignment = TextAlignment.Center;
            wr.Margin = new Thickness(0, compactScout ? 4 : 8, 0, 0);
            wr.ToolTip = retainedCache
                ? "Сохранённая выборка при ошибке/устаревании источника — не свежий WR"
                : observedGames > 0
                    ? "Взвешенный WR по известным героям источника (не модель силы)"
                    : "WR недоступен: источник не отдал героев";
            content.Children.Add(wr);
            var games = Text(gamesText, 10, color: retainedCache ? gold : muted);
            games.TextAlignment = TextAlignment.Center;
            games.ToolTip = wr.ToolTip;
            content.Children.Add(games);

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
            button.Padding = new Thickness(8, compactScout ? 5 : 9, 8, compactScout ? 5 : 10);
            button.Margin = new Thickness(0, 0, i == 4 ? 0 : 6, 0);
            button.Background = selected ? (Brush)FindResource("SelectedBrush") : (Brush)FindResource("PanelBrush");
            button.BorderBrush = selected ? accent : border;
            button.ToolTip = id > 0 ? "Профиль Steam account ID: " + id :
                "Добавить игрока можно в «Управление составами»";
            button.IsEnabled = id > 0;
            Grid.SetColumn(button, i); cards.Children.Add(button);
        }
        scoutSignalHost.Children.Add(cards);
        RenderScoutThreatModules(ids, reports, found, ready, hidden, errors);
    }

    private void RenderScoutThreatModules(
        long[] ids,
        System.Text.Json.Nodes.JsonNode?[] reports,
        int found, int ready, int hidden, int errors)
    {
        if (tacticalInsights == null) return;
        tacticalInsights.Children.Clear();

        var visible = ids.Where(id => id > 0)
            .Select(id => reports.FirstOrDefault(p => p.At("accountId").L() == id))
            .Where(p => p != null).ToArray();
        var readyReports = visible.Where(p => p!.At("scoutStatus").S() == "ok").ToArray();
        var heroRows = readyReports
            .SelectMany(p => p!.At("topHeroes").Items()
                .Select(h => new {
                    Id = h.At("id").I(),
                    Games = h.At("games").I(),
                    WinRate = h.At("winRate").D(),
                    Account = p!.At("accountId").L()
                }))
            .Where(h => h.Id > 0 && h.Games > 0)
            .ToArray();

        var grid = new Grid { Name = "ScoutThreatGrid" };
        bool stacked = compactScout;
        grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        if (!stacked)
            grid.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        grid.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        grid.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        if (stacked)
        {
            grid.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
            grid.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        }

        // Signature heroes — distinct top heroes by observed games.
        var signatures = heroRows
            .GroupBy(h => h.Id)
            .Select(g => new { Id = g.Key, Games = g.Sum(x => x.Games), Players = g.Select(x => x.Account).Distinct().Count() })
            .OrderByDescending(x => x.Games).ThenBy(x => x.Id)
            .Take(5).ToArray();
        var signatureBody = new StackPanel();
        if (signatures.Length == 0)
        {
            signatureBody.Children.Add(HonestEmpty(
                "Сигнатурные герои недоступны",
                "Нужны подтверждённые ответы источника с topHeroes."));
        }
        else
        {
            var strip = new StackPanel { Orientation = Orientation.Horizontal };
            foreach (var hero in signatures)
            {
                var cell = new StackPanel { Margin = new Thickness(0, 0, 8, 0), Width = 54 };
                var portrait = HeroPortrait(hero.Id, 54, 32);
                portrait.ToolTip = HeroName(hero.Id) + " · " + hero.Games + " игр в выборке";
                cell.Children.Add(portrait);
                var label = Text(hero.Games + "и", 9, color: muted);
                label.TextAlignment = TextAlignment.Center;
                label.Margin = new Thickness(0, 4, 0, 0);
                cell.Children.Add(label);
                strip.Children.Add(cell);
            }
            signatureBody.Children.Add(strip);
            signatureBody.Children.Add(Text("По сумме игр в подтверждённых пулах, не рейтинг силы.",
                10, color: muted));
        }
        var signatureCard = ThreatModule("Сигнатурные герои", signatureBody);
        grid.Children.Add(signatureCard);

        // Dangerous combos — only real pool overlaps (same hero on 2+ players).
        var overlaps = heroRows
            .GroupBy(h => h.Id)
            .Select(g => new {
                Id = g.Key,
                Players = g.Select(x => x.Account).Distinct().Count(),
                Games = g.Sum(x => x.Games)
            })
            .Where(x => x.Players > 1)
            .OrderByDescending(x => x.Players).ThenByDescending(x => x.Games).ThenBy(x => x.Id)
            .Take(4).ToArray();
        var comboBody = new StackPanel();
        if (overlaps.Length == 0)
        {
            comboBody.Children.Add(HonestEmpty(
                "Общие герои не найдены",
                "Не выдумываем связки: покажем пересечения пулов, когда они есть."));
        }
        else
        {
            foreach (var hit in overlaps)
            {
                var row = new DockPanel { Margin = new Thickness(0, 0, 0, 8), LastChildFill = true };
                var portrait = HeroPortrait(hit.Id, 42, 24);
                portrait.Margin = new Thickness(0, 0, 8, 0);
                DockPanel.SetDock(portrait, Dock.Left);
                row.Children.Add(portrait);
                var count = Text(hit.Players + " игрока", 11, FontWeights.SemiBold, gold);
                DockPanel.SetDock(count, Dock.Right);
                row.Children.Add(count);
                row.Children.Add(Text(HeroName(hit.Id) + " в нескольких пулах", 12, FontWeights.SemiBold));
                comboBody.Children.Add(row);
            }
            comboBody.Children.Add(Text("Это пересечение известных героев, не предсказанная связка.",
                10, color: muted));
        }
        var comboCard = ThreatModule("Опасные пересечения", comboBody);
        if (stacked) { Grid.SetRow(comboCard, 1); comboCard.Margin = new Thickness(0, 8, 0, 0); }
        else { Grid.SetColumn(comboCard, 1); comboCard.Margin = new Thickness(6, 0, 0, 0); }
        grid.Children.Add(comboCard);

        // Key risks — only observable source/sample signals.
        var risks = new System.Collections.Generic.List<string>();
        if (found == 0) risks.Add("Состав противников пуст — назначь игроков в «Составы».");
        if (hidden > 0) risks.Add(hidden + " профиль(я) без публичной статистики или устарели.");
        if (errors > 0) risks.Add(errors + " ошибка(и) источника — не трактуй как слабость игрока.");
        if (found > 0 && ready == 0) risks.Add("Нет ни одного подтверждённого ответа разведки.");
        var thin = heroRows.Where(h => h.Games < 8).Select(h => h.Account).Distinct().Count();
        if (thin > 0 && ready > 0)
            risks.Add(thin + " игрок(а) с тонкой выборкой (<8 игр на героя) — выводы хрупкие.");
        foreach (var player in readyReports)
        {
            var smurf = player!.At("smurf");
            if (smurf == null) continue;
            var level = smurf.At("level").S();
            var label = smurf.At("label").S();
            if (string.IsNullOrWhiteSpace(level) && string.IsNullOrWhiteSpace(label)) continue;
            risks.Add(PlayerName(player.At("accountId").L()) + ": метка smurf от ядра — " +
                (string.IsNullOrWhiteSpace(label) ? level : label));
        }
        var riskBody = new StackPanel();
        if (risks.Count == 0)
        {
            riskBody.Children.Add(HonestEmpty(
                "Явных рисков в данных нет",
                "Нет скрытых профилей, ошибок источника или тонкой выборки."));
        }
        else
        {
            foreach (var risk in risks.Take(5))
            {
                var row = new DockPanel { Margin = new Thickness(0, 0, 0, 8), LastChildFill = true };
                var mark = Text("!", 12, FontWeights.Bold, gold);
                mark.Margin = new Thickness(0, 0, 8, 0);
                DockPanel.SetDock(mark, Dock.Left);
                row.Children.Add(mark);
                var text = Text(risk, 12, color: foreground);
                text.TextWrapping = TextWrapping.Wrap;
                row.Children.Add(text);
                riskBody.Children.Add(row);
            }
        }
        var riskCard = ThreatModule("Ключевые риски", riskBody);
        if (stacked) { Grid.SetRow(riskCard, 2); riskCard.Margin = new Thickness(0, 8, 0, 0); }
        else { Grid.SetRow(riskCard, 1); riskCard.Margin = new Thickness(0, 8, 6, 0); }
        grid.Children.Add(riskCard);

        // Recommended ban — highest observed comfort hero with enough games; never invent.
        var banCandidate = heroRows
            .GroupBy(h => h.Id)
            .Select(g => new {
                Id = g.Key,
                Games = g.Sum(x => x.Games),
                WinRate = g.Sum(x => x.Games * x.WinRate) / Math.Max(1, g.Sum(x => x.Games)),
                Players = g.Select(x => x.Account).Distinct().Count()
            })
            .Where(x => x.Games >= 12)
            .OrderByDescending(x => x.Players)
            .ThenByDescending(x => x.Games)
            .ThenByDescending(x => x.WinRate)
            .ThenBy(x => x.Id)
            .FirstOrDefault();
        var banBody = new StackPanel();
        if (banCandidate == null)
        {
            banBody.Children.Add(HonestEmpty(
                "Наблюдаемый бан-кандидат недоступен",
                "Нужно ≥12 подтверждённых игр по герою в выборке. Модель бана здесь не подставляется."));
        }
        else
        {
            var row = new DockPanel { LastChildFill = true };
            var portrait = HeroPortrait(banCandidate.Id, 72, 42);
            portrait.Margin = new Thickness(0, 0, 12, 0);
            DockPanel.SetDock(portrait, Dock.Left);
            row.Children.Add(portrait);
            var info = new StackPanel();
            info.Children.Add(Text(HeroName(banCandidate.Id), 15, FontWeights.Bold));
            info.Children.Add(Text(
                banCandidate.Games + " игр · " + banCandidate.WinRate.ToString("P0") +
                " · у " + banCandidate.Players + " игрока(ов)",
                11, color: muted));
            info.Children.Add(Text("По частоте в подтверждённых пулах, не сила драфта.",
                10, FontWeights.SemiBold, accent));
            row.Children.Add(info);
            banBody.Children.Add(row);
            var priority = new Border
            {
                Margin = new Thickness(0, 10, 0, 0),
                HorizontalAlignment = HorizontalAlignment.Left,
                Background = (Brush)FindResource("RaisedBrush"),
                BorderBrush = accent,
                BorderThickness = new Thickness(1),
                CornerRadius = new CornerRadius(99),
                Padding = new Thickness(10, 3, 10, 3),
                Child = Text("Наблюдаемый приоритет", 10, FontWeights.SemiBold, accent)
            };
            banBody.Children.Add(priority);
        }
        var banCard = ThreatModule("Наблюдаемый бан-кандидат", banBody);
        if (stacked) { Grid.SetRow(banCard, 3); banCard.Margin = new Thickness(0, 8, 0, 0); }
        else
        {
            Grid.SetRow(banCard, 1);
            Grid.SetColumn(banCard, 1);
            banCard.Margin = new Thickness(6, 8, 0, 0);
        }
        grid.Children.Add(banCard);

        if (!stacked) signatureCard.Margin = new Thickness(0, 0, 6, 0);

        tacticalInsights.Children.Add(grid);
        var notice = Text("Статистика ограничена доступной выборкой источника. Пустые модули — честный пробел, не заглушка.",
            11, color: muted);
        notice.Margin = new Thickness(0, 12, 0, 0);
        tacticalInsights.Children.Add(notice);
    }

    private Border ThreatModule(string title, UIElement body)
    {
        var host = new StackPanel();
        host.Children.Add(Text(title.ToUpperInvariant(), 10, FontWeights.Bold, muted));
        var content = new Border { Child = body, Margin = new Thickness(0, compactScout ? 4 : 8, 0, 0) };
        host.Children.Add(content);
        return new Border
        {
            Child = host,
            Background = (Brush)FindResource("RaisedBrush"),
            BorderBrush = border,
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(10),
            Padding = new Thickness(compactScout ? 8 : 12, compactScout ? 6 : 10, compactScout ? 8 : 12, compactScout ? 8 : 12)
        };
    }

    private StackPanel HonestEmpty(string title, string detail)
    {
        var host = new StackPanel();
        host.Children.Add(Text(title, 12, FontWeights.SemiBold, muted));
        var line = Text(detail, 11, color: muted);
        line.Margin = new Thickness(0, 4, 0, 0);
        line.TextWrapping = TextWrapping.Wrap;
        host.Children.Add(line);
        return host;
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

