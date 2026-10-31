using System;
using System.Collections.Generic;
using System.Linq;
using System.Text.Json.Nodes;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Controls.Primitives;
using System.Windows.Input;
using System.Windows.Media;

namespace Cmnet.Native;

// Minimal Tactical 3B. All informational elements are derived from live core
// state, cached scouting observations, or recommendations. No fake timers,
// threat levels, model percentages or generated "confidence" statistics.
public sealed partial class NativeWindow
{
    private StackPanel? tacticalSelection;
    private string tacticalSelectedKey = "";
    private StackPanel? draftEnemyIntelHost;
    private StackPanel? draftScoutSignalsHost;
    private TextBlock? draftTurnLabel;
    private TextBlock? draftTimerLabel;
    private TextBlock? draftTimerCaption;
    private Button? draftUndoButton;
    private Button? draftRedoButton;
    private Button? draftNewButton;
    private string draftIntelKey = "";
    private HeroAttributes.Attr heroAttrFilter = HeroAttributes.Attr.All;
    private int heroRoleFilter; // 0 = all; 1-5 = position
    private readonly Dictionary<HeroAttributes.Attr, Border> attrFilterChips = new();
    private readonly Dictionary<int, Border> roleFilterChips = new();

    private Border TacticalPanel(UIElement child, Thickness margin, bool elevated = false)
    {
        return new Border
        {
            Background = elevated ? soft : panel,
            BorderBrush = border,
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(4),
            Padding = new Thickness(10),
            Margin = margin,
            Child = child
        };
    }

    private UIElement BuildTacticalDraftView()
    {
        heroCatalogKey = recommendationKey = draftBoardKey = tacticalSelectedKey = draftIntelKey = "";
        attrFilterChips.Clear();
        roleFilterChips.Clear();
        heroAttrFilter = HeroAttributes.Attr.All;
        heroRoleFilter = 0;

        var root = new Grid();
        root.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto }); // heading
        root.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto }); // evidence
        root.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto }); // board
        root.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        root.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto }); // intel

        var heading = new DockPanel { Margin = new Thickness(0, 0, 0, 8) };
        draftNewButton = Btn("＋ Новый драфт", async () => await RunAction("/api/new-draft"), true);
        draftNewButton.Name = "NewDraft";
        draftNewButton.VerticalAlignment = VerticalAlignment.Center;
        draftNewButton.Margin = new Thickness(8, 0, 0, 0);
        DockPanel.SetDock(draftNewButton, Dock.Right); heading.Children.Add(draftNewButton);
        draftGsiBadge = Text("●  Ручной режим", 11, FontWeights.SemiBold, muted);
        draftGsiBadge.Name = "DraftGsiBadge";
        draftGsiBadge.VerticalAlignment = VerticalAlignment.Center;
        draftGsiBadge.Margin = new Thickness(8, 0, 0, 0);
        DockPanel.SetDock(draftGsiBadge, Dock.Right); heading.Children.Add(draftGsiBadge);
        heading.Children.Add(Text("CAPTAINS MODE", 12, FontWeights.Bold, accent));
        root.Children.Add(heading);

        draftEvidenceBanner = BuildEvidenceBanner("DraftEvidenceBanner");
        Grid.SetRow(draftEvidenceBanner, 1);
        root.Children.Add(draftEvidenceBanner);

        draftBoardHost = new StackPanel { Name = "DraftBoard" };
        var board = TacticalPanel(draftBoardHost, new Thickness(0, 0, 0, 8));
        Grid.SetRow(board, 2); root.Children.Add(board);

        var workspace = new Grid { Name = "DraftWorkspace" };
        workspace.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(200) });
        workspace.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        workspace.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(230) });
        Grid.SetRow(workspace, 3); root.Children.Add(workspace);

        // —— Left: recommendations ——
        var recommendations = new Grid();
        recommendations.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        recommendations.RowDefinitions.Add(new RowDefinition());
        recommendations.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        recommendations.Children.Add(Text("РЕКОМЕНДАЦИИ", 10, FontWeights.Bold, accent));
        draftRecommendations = new ListBox { Margin = new Thickness(0, 8, 0, 6) };
        draftRecommendations.ItemContainerStyle = (Style)FindResource("CompactListItem");
        Grid.SetRow(draftRecommendations, 1); recommendations.Children.Add(draftRecommendations);
        var actions = new UniformGrid { Columns = 2 };
        draftUndoButton = Btn("↶", async () =>
        {
            if (!IsGsiReadonly()) await RunAction("/api/undo");
        }, true);
        draftRedoButton = Btn("↷", async () =>
        {
            if (!IsGsiReadonly()) await RunAction("/api/redo");
        }, true);
        draftUndoButton.ToolTip = "Отменить ход";
        draftRedoButton.ToolTip = "Повторить ход";
        actions.Children.Add(draftUndoButton);
        actions.Children.Add(draftRedoButton);
        Grid.SetRow(actions, 2); recommendations.Children.Add(actions);
        var left = TacticalPanel(recommendations, new Thickness(0, 0, 6, 0));
        left.Name = "DraftRecommendations"; workspace.Children.Add(left);

        // —— Center: gallery + filters ——
        var catalog = new Grid();
        catalog.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        catalog.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        catalog.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        catalog.RowDefinitions.Add(new RowDefinition());
        var filterHeading = new DockPanel();
        heroResultsCount = Text("", 10, color: muted);
        DockPanel.SetDock(heroResultsCount, Dock.Right); filterHeading.Children.Add(heroResultsCount);
        filterHeading.Children.Add(Text("ГЕРОИ", 10, FontWeights.Bold, accent));
        catalog.Children.Add(filterHeading);

        heroSearchBox = new TextBox
        {
            Text = heroSearchText,
            Margin = new Thickness(0, 8, 0, 6),
            ToolTip = "Поиск героя (Ctrl+F)",
            Height = 32
        };
        System.Windows.Automation.AutomationProperties.SetName(heroSearchBox, "Поиск героев");
        heroSearchBox.TextChanged += (_, _) => { heroSearchText = heroSearchBox.Text; UpdateHeroes(heroSearchText); };
        Grid.SetRow(heroSearchBox, 1); catalog.Children.Add(heroSearchBox);

        var filterStrip = new StackPanel
        {
            Name = "DraftAttrFilter",
            Margin = new Thickness(0, 0, 0, 8)
        };
        var roleStrip = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 0, 0, 6) };
        for (int role = 0; role <= 5; role++)
        {
            var chip = RoleFilterChip(role);
            roleFilterChips[role] = chip;
            roleStrip.Children.Add(chip);
        }
        filterStrip.Children.Add(roleStrip);
        var attrStrip = new StackPanel { Orientation = Orientation.Horizontal };
        foreach (var attr in new[] {
            HeroAttributes.Attr.All, HeroAttributes.Attr.Str,
            HeroAttributes.Attr.Agi, HeroAttributes.Attr.Int, HeroAttributes.Attr.Uni })
        {
            var chip = AttrFilterChip(attr);
            attrFilterChips[attr] = chip;
            attrStrip.Children.Add(chip);
        }
        filterStrip.Children.Add(attrStrip);
        Grid.SetRow(filterStrip, 2); catalog.Children.Add(filterStrip);

        heroGallery = new HeroCatalog();
        Grid.SetRow(heroGallery, 3); catalog.Children.Add(heroGallery);
        var center = TacticalPanel(catalog, new Thickness(0, 0, 6, 0));
        center.Name = "DraftCatalog"; Grid.SetColumn(center, 1); workspace.Children.Add(center);

        // —— Right: selected hero ——
        var selection = new Grid();
        selection.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        selection.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        selection.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        selection.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        selection.Children.Add(Text("ВЫБРАННЫЙ ГЕРОЙ", 10, FontWeights.Bold, accent));
        tacticalSelection = new StackPanel { Margin = new Thickness(0, 8, 0, 8) };
        var selectedScroll = Scroll(tacticalSelection);
        Grid.SetRow(selectedScroll, 1); selection.Children.Add(selectedScroll);
        draftSummary = Text("Ожидание модели…", 11, color: muted);
        draftSummary.TextWrapping = TextWrapping.Wrap;
        draftSummary.Margin = new Thickness(0, 0, 0, 8);
        Grid.SetRow(draftSummary, 2); selection.Children.Add(draftSummary);
        selectedHeroHint = Text("", 11);
        applyDraftButton = Btn("Выбрать героя", async () =>
        {
            if (CanApplyHero(selectedHeroId)) await RunAction("/api/apply", new { heroId = selectedHeroId });
        });
        applyDraftButton.Name = "ApplyHero";
        applyDraftButton.Background = ThemeBrush("PrimaryBrush");
        applyDraftButton.BorderBrush = accent;
        applyDraftButton.FontWeight = FontWeights.Bold;
        applyDraftButton.IsEnabled = CanApplyHero(selectedHeroId);
        Grid.SetRow(applyDraftButton, 3); selection.Children.Add(applyDraftButton);
        var right = TacticalPanel(selection, new Thickness(0));
        right.Name = "DraftContext"; Grid.SetColumn(right, 2); workspace.Children.Add(right);

        // —— Bottom: enemy intel + scout signals (D5) ——
        var intelRow = new Grid { Name = "DraftEnemyIntel", Margin = new Thickness(0, 8, 0, 0) };
        intelRow.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1.6, GridUnitType.Star) });
        intelRow.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        draftEnemyIntelHost = new StackPanel();
        var intelPanel = TacticalPanel(Stack(
            Text("РАЗВЕДКА ПРОТИВНИКОВ", 10, FontWeights.Bold, accent),
            draftEnemyIntelHost
        ), new Thickness(0, 0, 6, 0));
        intelPanel.Padding = new Thickness(10, 8, 10, 8);
        intelRow.Children.Add(intelPanel);
        draftScoutSignalsHost = new StackPanel();
        var signalsPanel = TacticalPanel(Stack(
            Text("СИГНАЛЫ РАЗВЕДКИ", 10, FontWeights.Bold, accent),
            draftScoutSignalsHost
        ), new Thickness(0));
        signalsPanel.Padding = new Thickness(10, 8, 10, 8);
        Grid.SetColumn(signalsPanel, 1);
        intelRow.Children.Add(signalsPanel);
        Grid.SetRow(intelRow, 4); root.Children.Add(intelRow);

        workspace.SizeChanged += (_, _) =>
        {
            selectedScroll.MaxHeight = Math.Max(80, Math.Min(320, workspace.ActualHeight - 120));
            bool compact = workspace.ActualWidth < 900;
            workspace.ColumnDefinitions[0].Width = new GridLength(compact ? 176 : 210);
            workspace.ColumnDefinitions[2].Width = new GridLength(compact ? 200 : 250);
            intelRow.Visibility = workspace.ActualHeight < 280 ? Visibility.Collapsed : Visibility.Visible;
        };
        RefreshSelectedHeroDetail();
        return root;
    }

    private Border AttrFilterChip(HeroAttributes.Attr attr)
    {
        var label = Text(HeroAttributes.Short(attr), 11, FontWeights.SemiBold);
        label.HorizontalAlignment = HorizontalAlignment.Center;
        var chip = new Border
        {
            Child = label,
            Background = ThemeBrush("RaisedBrush"),
            BorderBrush = border,
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(4),
            Padding = new Thickness(10, 4, 10, 4),
            Margin = new Thickness(0, 0, 6, 0),
            Cursor = Cursors.Hand,
            ToolTip = HeroAttributes.Label(attr) + " (справочник атрибутов UI)"
        };
        chip.MouseLeftButtonUp += (_, _) =>
        {
            heroAttrFilter = attr;
            SyncDraftFilterChips();
            UpdateHeroes(heroSearchText);
        };
        return chip;
    }

    private Border RoleFilterChip(int role)
    {
        var caption = role == 0 ? "Все роли" : "Поз." + role;
        var label = Text(caption, 11, FontWeights.SemiBold);
        label.HorizontalAlignment = HorizontalAlignment.Center;
        var chip = new Border
        {
            Child = label,
            Background = ThemeBrush("RaisedBrush"),
            BorderBrush = border,
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(4),
            Padding = new Thickness(8, 4, 8, 4),
            Margin = new Thickness(0, 0, 6, 0),
            Cursor = Cursors.Hand,
            ToolTip = role == 0
                ? "Все роли (без фильтра позиции)"
                : "Фильтр по позиции " + role + " из каталога ролей"
        };
        chip.MouseLeftButtonUp += (_, _) =>
        {
            heroRoleFilter = role;
            SyncDraftFilterChips();
            UpdateHeroes(heroSearchText);
        };
        return chip;
    }

    private void SyncDraftFilterChips()
    {
        foreach (var (attr, chip) in attrFilterChips)
        {
            bool on = attr == heroAttrFilter;
            chip.Background = on ? ThemeBrush("PrimaryBrush") : ThemeBrush("RaisedBrush");
            chip.BorderBrush = on ? accent : border;
            if (chip.Child is TextBlock t)
                t.Foreground = on ? accent : foreground;
        }
        foreach (var (role, chip) in roleFilterChips)
        {
            bool on = role == heroRoleFilter;
            chip.Background = on ? ThemeBrush("PrimaryBrush") : ThemeBrush("RaisedBrush");
            chip.BorderBrush = on ? accent : border;
            if (chip.Child is TextBlock t)
                t.Foreground = on ? accent : foreground;
        }
    }

    private bool CanApplyHero(int id) => id > 0
        && !IsGsiReadonly()
        && state.At("meta", "draftReady").B()
        && !state.At("step", "done").B()
        && state.At("step", "action").S() is "pick" or "ban"
        && state.At("heroes").Items().Any(h => h.At("id").I() == id)
        && !IsHeroTaken(id);

    private UIElement TacticalHeroTile(int id)
    {
        var stack = new StackPanel();
        stack.Children.Add(HeroPortrait(id, 64, 38));
        var title = Text(HeroName(id), 9, FontWeights.SemiBold);
        title.Width = 64;
        title.TextTrimming = TextTrimming.CharacterEllipsis;
        title.TextWrapping = TextWrapping.NoWrap;
        title.Margin = new Thickness(0, 3, 0, 0);
        stack.Children.Add(title);
        var button = Btn("", async () =>
        {
            SelectTacticalHero(id);
            await System.Threading.Tasks.Task.CompletedTask;
        }, true);
        button.Content = stack;
        button.Tag = id;
        button.ToolTip = "Выбрать " + HeroName(id);
        button.Width = 72;
        button.Height = 66;
        button.Padding = new Thickness(2);
        button.Margin = new Thickness(0, 0, 3, 4);
        button.Background = id == selectedHeroId ? ThemeBrush("PrimaryBrush") : ThemeBrush("RaisedBrush");
        button.BorderBrush = id == selectedHeroId ? accent : border;
        return button;
    }

    private bool IsHeroTaken(int id) => id > 0 && new[] { "radiantPicks", "direPicks", "radiantBans", "direBans" }
        .Any(k => state.At("board", k).Items().Any(h => h.I() == id));

    private void SelectTacticalHero(int id)
    {
        // Selection is for inspection; Apply stays gated by CanApplyHero.
        if (id <= 0 || IsHeroTaken(id)) return;
        if (state.At("heroes").Items().All(h => h.At("id").I() != id)) return;
        selectedHeroId = id;
        if (selectedHeroHint != null)
            selectedHeroHint.Text = "Выбран: " + HeroName(id);
        if (applyDraftButton != null) applyDraftButton.IsEnabled = CanApplyHero(id);
        HighlightDraftSelection();
        RefreshSelectedHeroDetail();
    }

    private static int ScoreToInteger(double score)
    {
        // Model scores are calibrated probabilities in [0,1]; older fixtures may
        // already ship integer 0–100. Never invent a second ranking scale.
        if (score > 1.0) return (int)Math.Round(score);
        return (int)Math.Round(score * 100.0);
    }

    private static IReadOnlyList<string> DeriveRecTags(JsonNode? rec)
    {
        var tags = new List<string>();
        foreach (var t in rec.At("tags").Items())
        {
            var s = t.S().Trim();
            if (!string.IsNullOrEmpty(s)) tags.Add(s);
        }
        var why = rec.At("why").S();
        if (why.Contains("из наших", StringComparison.OrdinalIgnoreCase))
            tags.Add("Комфорт");
        if (why.Contains("контр", StringComparison.OrdinalIgnoreCase) ||
            why.Contains("угроз", StringComparison.OrdinalIgnoreCase))
            tags.Add("Контрпик");
        if (why.Contains("синерг", StringComparison.OrdinalIgnoreCase))
            tags.Add("Синергия");
        if (why.Contains("геймплей", StringComparison.OrdinalIgnoreCase) ||
            why.Contains("мобильн", StringComparison.OrdinalIgnoreCase))
            tags.Add("Геймплей");
        return tags.Distinct(StringComparer.OrdinalIgnoreCase).Take(3).ToList();
    }

    private void RefreshSelectedHeroDetail()
    {
        if (tacticalSelection == null) return;
        var selectedRecommendation = state?.At("result", "recs").Items()
            .FirstOrDefault(r => r.At("heroId").I() == selectedHeroId);
        var key = selectedHeroId + "|" + (selectedRecommendation?.At("score").D().ToString("R") ?? "missing")
            + "|" + (selectedRecommendation?.At("why").S() ?? "");
        if (key == tacticalSelectedKey && tacticalSelection.Children.Count > 0) return;
        tacticalSelectedKey = key;
        tacticalSelection.Children.Clear();
        if (state == null || selectedHeroId <= 0)
        {
            var empty = new Border {
                Background = ThemeBrush("RaisedBrush"), BorderBrush = border,
                BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(4),
                Padding = new Thickness(10), MinHeight = 110
            };
            empty.Child = Text("Нажми на портрет героя или рекомендацию.\n\nЗдесь появятся роли, винрейт и «почему».",
                12, color: muted);
            tacticalSelection.Children.Add(empty);
            return;
        }

        var selected = selectedHeroId;
        tacticalSelection.Children.Add(HeroPortrait(selected, 148, 74));
        var heroName = Text(HeroName(selected), 16, FontWeights.Bold);
        heroName.Margin = new Thickness(0, 10, 0, 6);
        tacticalSelection.Children.Add(heroName);

        var roles = ResolveHeroRoleLabels(selected);
        if (roles.Count > 0)
        {
            var roleRow = new WrapPanel { Margin = new Thickness(0, 0, 0, 8) };
            foreach (var role in roles.Take(4))
                roleRow.Children.Add(Pill(role, ThemeBrush("RaisedBrush"), accent));
            tacticalSelection.Children.Add(roleRow);
        }
        else
        {
            tacticalSelection.Children.Add(Text("Роли: нет данных в каталоге", 11, color: muted));
        }

        var (wrText, popText) = ResolveHeroMetaStats(selected, selectedRecommendation);
        var stats = new Grid { Margin = new Thickness(0, 8, 0, 10) };
        stats.ColumnDefinitions.Add(new ColumnDefinition());
        stats.ColumnDefinitions.Add(new ColumnDefinition());
        var wrBlock = new StackPanel();
        wrBlock.Children.Add(Text(wrText, 16, FontWeights.Bold, accent));
        wrBlock.Children.Add(Text("Винрейт", 10, color: muted));
        stats.Children.Add(wrBlock);
        var popBlock = new StackPanel();
        popBlock.Children.Add(Text(popText, 16, FontWeights.Bold, gold));
        popBlock.Children.Add(Text("Популярность", 10, color: muted));
        Grid.SetColumn(popBlock, 1);
        stats.Children.Add(popBlock);
        tacticalSelection.Children.Add(stats);

        var whyLines = ResolveWhyLines(selectedRecommendation);
        tacticalSelection.Children.Add(Text("ПОЧЕМУ РЕКОМЕНДУЕМ", 10, FontWeights.Bold, muted));
        if (whyLines.Count == 0)
        {
            var missing = Text(
                selectedRecommendation == null
                    ? "Герой не в топе рекомендаций — объяснения модели нет."
                    : "Ядро не вернуло why для этой рекомендации.",
                11, color: muted);
            missing.Margin = new Thickness(0, 6, 0, 0);
            missing.TextWrapping = TextWrapping.Wrap;
            tacticalSelection.Children.Add(missing);
        }
        else
        {
            foreach (var line in whyLines.Take(5))
            {
                var row = new DockPanel { Margin = new Thickness(0, 6, 0, 0), LastChildFill = true };
                var bullet = Text("•", 12, FontWeights.Bold, accent);
                bullet.Margin = new Thickness(0, 0, 6, 0);
                DockPanel.SetDock(bullet, Dock.Left);
                row.Children.Add(bullet);
                var body = Text(line, 12);
                body.TextWrapping = TextWrapping.Wrap;
                row.Children.Add(body);
                tacticalSelection.Children.Add(row);
            }
        }
    }

    private List<string> ResolveHeroRoleLabels(int heroId)
    {
        var labels = new List<string>();
        var hero = state?.At("heroes").Items().FirstOrDefault(h => h.At("id").I() == heroId);
        foreach (var role in hero.At("roles").Items())
        {
            if (role is JsonValue jv && jv.TryGetValue<int>(out var pos) && pos is >= 1 and <= 5)
                labels.Add("Поз. " + pos);
            else
            {
                var s = role.S().Trim();
                if (!string.IsNullOrEmpty(s)) labels.Add(s);
            }
        }
        var recRole = state?.At("result", "recs").Items()
            .FirstOrDefault(r => r.At("heroId").I() == heroId)?.At("role").I() ?? 0;
        if (recRole is >= 1 and <= 5 && !labels.Contains("Поз. " + recRole))
            labels.Insert(0, "Поз. " + recRole);
        return labels;
    }

    private (string Wr, string Pop) ResolveHeroMetaStats(int heroId, JsonNode? rec)
    {
        string? wr = null;
        string? pop = null;
        // Prefer explicit sample/corpus fields when present (honest labels only).
        if (rec?.At("winRate") != null) wr = (rec.At("winRate").D() * 100).ToString("0.0") + "%";
        if (rec?.At("pickRate") != null) pop = (rec.At("pickRate").D() * 100).ToString("0.0") + "%";
        foreach (var lane in state?.At("result", "positionMeta").Items() ?? [])
        {
            var hero = lane.At("heroes").Items().FirstOrDefault(h => h.At("heroId").I() == heroId);
            if (hero == null) continue;
            if (wr == null && hero.At("winRate").D() > 0)
                wr = (hero.At("winRate").D() * 100).ToString("0.0") + "%";
            if (pop == null && hero.At("score").D() > 0 && hero.At("score").D() <= 1)
                pop = (hero.At("score").D() * 100).ToString("0.0") + "%";
            break;
        }
        // Parse "вин ~54%" from why when model embeds it — still core-authored.
        var why = rec.At("why").S();
        if (wr == null)
        {
            var m = System.Text.RegularExpressions.Regex.Match(why, @"вин\s*~?\s*(\d+(?:[.,]\d+)?)\s*%",
                System.Text.RegularExpressions.RegexOptions.IgnoreCase);
            if (m.Success) wr = m.Groups[1].Value.Replace(',', '.') + "%";
        }
        return (wr ?? "—", pop ?? "—");
    }

    private static List<string> ResolveWhyLines(JsonNode? rec)
    {
        var lines = new List<string>();
        foreach (var line in rec.At("whyLines").Items())
        {
            var s = line.S().Trim();
            if (!string.IsNullOrEmpty(s)) lines.Add(s);
        }
        if (lines.Count > 0) return lines;
        var why = rec.At("why").S().Trim();
        if (string.IsNullOrEmpty(why)) return lines;
        foreach (var part in why.Split(['·', '|', ';', '\n'], StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries))
            if (!string.IsNullOrWhiteSpace(part)) lines.Add(part);
        return lines;
    }

    private void RefreshDraftEnemyIntel()
    {
        if (draftEnemyIntelHost == null || draftScoutSignalsHost == null || page != "Драфт") return;
        var ids = state?.At("team", "enemy").Items().Select(x => x.L()).Take(5).ToArray() ?? [];
        var reports = stats?.At("stats", "enemyPlayers").Items().ToArray() ?? [];
        var key = string.Join("|", ids) + "|" +
            string.Join("|", reports.Select(p => p?.ToJsonString() ?? "")) + "|" +
            (players?.ToJsonString() ?? "");
        if (key == draftIntelKey) return;
        draftIntelKey = key;

        draftEnemyIntelHost.Children.Clear();
        draftScoutSignalsHost.Children.Clear();

        if (ids.Length == 0 || ids.All(id => id <= 0))
        {
            draftEnemyIntelHost.Children.Add(Text(
                "Состав противников пуст — назначь игроков в «Разведка → Составы».",
                11, color: muted));
            draftScoutSignalsHost.Children.Add(Text(
                "Сигналов нет: нет подтверждённых профилей.",
                11, color: muted));
            return;
        }

        var cards = new UniformGrid { Rows = 1, Columns = 5, Margin = new Thickness(0, 8, 0, 0) };
        var readyHeroes = new List<(int Id, int Games, long Account)>();
        var narrowPools = 0;
        var ready = 0;
        var hidden = 0;
        var errors = 0;

        for (int i = 0; i < 5; i++)
        {
            var id = i < ids.Length ? ids[i] : 0L;
            var report = id == 0 ? null : reports.FirstOrDefault(p => p.At("accountId").L() == id);
            var source = id == 0 ? "empty" : report?.At("scoutStatus").S("pending") ?? "pending";
            if (source == "ok") ready++;
            else if (source is "no_stats" or "stale" or "unresolved") hidden++;
            else if (source == "error") errors++;

            var tone = source switch
            {
                "ok" => good,
                "error" => (Brush)FindResource("ErrorBrush"),
                "no_stats" or "stale" or "unresolved" => gold,
                _ => muted
            };
            var body = new StackPanel();
            var name = Text(id > 0 ? PlayerName(id) : "Слот " + (i + 1), 11, FontWeights.SemiBold);
            name.TextTrimming = TextTrimming.CharacterEllipsis;
            body.Children.Add(name);
            body.Children.Add(Text(source switch
            {
                "ok" => "данные",
                "no_stats" => "скрыто",
                "stale" => "кэш",
                "error" => "ошибка",
                "unresolved" => "не опознан",
                "empty" => "пусто",
                _ => "ожидание"
            }, 9, FontWeights.SemiBold, tone));

            var heroes = new StackPanel { Orientation = Orientation.Horizontal, Margin = new Thickness(0, 4, 0, 0) };
            var tops = report?.At("topHeroes").Items().Take(2).ToArray() ?? [];
            if (source == "ok" && tops.Length > 0)
            {
                if (tops.Length <= 2) narrowPools++;
                foreach (var h in tops)
                {
                    var hid = h.At("id").I();
                    if (hid <= 0) continue;
                    readyHeroes.Add((hid, h.At("games").I(), id));
                    var p = HeroPortrait(hid, 28, 16);
                    p.Margin = new Thickness(0, 0, 3, 0);
                    p.ToolTip = HeroName(hid) + " · " + h.At("games").I() + " игр · " +
                        h.At("winRate").D().ToString("P0");
                    heroes.Children.Add(p);
                }
                var wr = tops[0].At("winRate").D();
                if (wr > 0)
                    body.Children.Add(Text(wr.ToString("P0"), 10, FontWeights.Bold, accent));
            }
            else if (source is "ok" or "stale")
            {
                body.Children.Add(Text("пул пуст", 9, color: muted));
            }
            body.Children.Add(heroes);

            cards.Children.Add(new Border
            {
                Child = body,
                Background = ThemeBrush("RaisedBrush"),
                BorderBrush = tone,
                BorderThickness = new Thickness(1),
                CornerRadius = new CornerRadius(6),
                Padding = new Thickness(6, 5, 6, 5),
                Margin = new Thickness(0, 0, 4, 0)
            });
        }
        draftEnemyIntelHost.Children.Add(cards);

        void Signal(string text)
        {
            var row = new DockPanel { Margin = new Thickness(0, 6, 0, 0), LastChildFill = true };
            var mark = Text("▸", 11, FontWeights.Bold, accent);
            mark.Margin = new Thickness(0, 0, 6, 0);
            DockPanel.SetDock(mark, Dock.Left);
            row.Children.Add(mark);
            var body = Text(text, 11);
            body.TextWrapping = TextWrapping.Wrap;
            row.Children.Add(body);
            draftScoutSignalsHost.Children.Add(row);
        }

        if (ready == 0 && hidden == 0 && errors == 0)
            Signal("Разведка ещё не запускалась — сигналов нет.");
        if (narrowPools > 0) Signal(narrowPools + " игрок(а) с узким наблюдаемым пулом (≤2 героя).");
        var frequent = readyHeroes
            .GroupBy(x => x.Id)
            .Select(g => new { Id = g.Key, Players = g.Select(x => x.Account).Distinct().Count(), Games = g.Sum(x => x.Games) })
            .Where(x => x.Players >= 2 || x.Games >= 20)
            .OrderByDescending(x => x.Players).ThenByDescending(x => x.Games).ThenBy(x => x.Id)
            .FirstOrDefault();
        if (frequent != null)
            Signal("Часто в пулах: " + HeroName(frequent.Id) + " (" + frequent.Players + " игр./" + frequent.Games + " игр).");
        if (hidden > 0) Signal(hidden + " профиль(я) скрыты или устарели — без выдуманной аналитики.");
        if (errors > 0) Signal(errors + " ошибка(и) источника.");
        if (draftScoutSignalsHost.Children.Count == 0)
            Signal("Подтверждённых сигналов по текущей выборке нет.");
    }
}
