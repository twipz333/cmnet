using System;
using System.Linq;
using System.Windows;
using System.Windows.Controls;
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
                Height = 47,
                ClipToBounds = true
            };
            if (heroId > 0)
            {
                var frame = new Grid();
                frame.Children.Add(HeroPortrait(heroId, 65, 47));
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
        var ids = state.At("team", "enemy").Items()
            .Select(x => x.L()).Take(5).ToArray();
        var reports = stats.At("stats", "enemyPlayers").Items().ToArray();
        var key = string.Join("|", ids) + "|" +
            string.Join("|", reports.Select(p => p.ToJsonString())) + "|" +
            players?.ToJsonString();
        if (key == scoutSignalKey) return;
        scoutSignalKey = key;
        scoutSignalHost.Children.Clear();

        var ready = ids.Count(id => id > 0 &&
            reports.Any(p => p.At("accountId").L() == id &&
                p.At("scoutStatus").S() == "ok"));
        var assigned = ids.Count(id => id > 0);
        var top = new DockPanel { LastChildFill = true,
            Margin = new Thickness(0, 0, 0, 15) };
        var badge = Pill($"{ready} / {assigned}  ГОТОВО",
            ready == assigned && assigned > 0 ? Hex("#1B403D") : Hex("#443527"),
            ready == assigned && assigned > 0 ? good : gold);
        DockPanel.SetDock(badge, Dock.Right);
        top.Children.Add(badge);
        var titles = new StackPanel();
        titles.Children.Add(Text("СИГНАЛЫ РАЗВЕДКИ", 10, FontWeights.Bold, accent));
        var caption = Text("Пять противников. Достоверность данных — без догадок.",
            12, color: muted);
        caption.Margin = new Thickness(0, 5, 0, 0);
        titles.Children.Add(caption);
        top.Children.Add(titles);
        scoutSignalHost.Children.Add(top);

        var cards = new WrapPanel();
        for (var i = 0; i < 5; i++)
        {
            var id = i < ids.Length ? ids[i] : 0L;
            var report = reports.FirstOrDefault(p => p.At("accountId").L() == id);
            var source = id == 0 ? "empty" :
                report?.At("scoutStatus").S("pending") ?? "pending";
            var valid = source == "ok";
            var color = valid ? good : source == "pending" || source == "empty"
                ? muted : gold;
            var shortStatus = source switch {
                "ok" => "ДАННЫЕ ГОТОВЫ",
                "no_stats" => "СКРЫТА СТАТИСТИКА",
                "stale" => "УСТАРЕЛИ",
                "error" => "ОШИБКА ИСТОЧНИКА",
                "unresolved" => "НЕ ОПОЗНАН",
                "empty" => "НЕ НАЗНАЧЕН",
                _ => "ОЖИДАНИЕ"
            };
            var card = new StackPanel();
            var topLine = new DockPanel();
            var number = Text($"0{i + 1}", 11, FontWeights.Bold, color);
            DockPanel.SetDock(number, Dock.Right);
            topLine.Children.Add(number);
            topLine.Children.Add(Text(shortStatus, 9, FontWeights.Bold, color));
            card.Children.Add(topLine);

            var person = Text(id == 0 ? "Свободное место" : PlayerName(id),
                13, FontWeights.Bold);
            person.TextTrimming = TextTrimming.CharacterEllipsis;
            person.TextWrapping = TextWrapping.NoWrap;
            person.Margin = new Thickness(0, 10, 0, 4);
            card.Children.Add(person);

            var best = report?.At("topHeroes").Items().FirstOrDefault();
            var heroId = best?.At("id").I() ?? 0;
            var detail = id == 0 ? "Добавь Steam ID" :
                heroId > 0 ? $"{HeroName(heroId)} · {best?.At("games").I()} игр" :
                "Открой профиль для деталей";
            var small = Text(detail, 10, color: muted);
            small.TextTrimming = TextTrimming.CharacterEllipsis;
            small.TextWrapping = TextWrapping.NoWrap;
            card.Children.Add(small);

            var tile = new Border {
                Child = card,
                Width = 170,
                Background = Hex(valid ? "#163332" : "#172838"),
                BorderBrush = valid ? Hex("#397467") : border,
                BorderThickness = new Thickness(1),
                CornerRadius = new CornerRadius(10),
                Padding = new Thickness(12, 11, 12, 12),
                Margin = new Thickness(0, 0, 9, 9),
                ToolTip = id > 0 ? $"Steam Account ID: {id}" : "Место не занято"
            };
            cards.Children.Add(tile);
        }
        scoutSignalHost.Children.Add(cards);
    }
}
