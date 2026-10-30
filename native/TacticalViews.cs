using System;
using System.Linq;
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
    private StackPanel? tacticalInsights;
    private TextBlock? scoutFoundCount;
    private TextBlock? scoutReadyCount;
    private TextBlock? scoutLimitedCount;
    private TextBlock? scoutFailureCount;

    private Border TacticalPanel(UIElement child, Thickness margin, bool elevated = false)
    {
        return new Border
        {
            Background = Hex(elevated ? "#172737" : "#111E2B"),
            BorderBrush = Hex("#2A4054"),
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(8),
            Padding = new Thickness(12),
            Margin = margin,
            Child = child
        };
    }

    private Border TacticalStatistic(string label, TextBlock value, Brush tone)
    {
        var root = new StackPanel();
        var heading = Text(label.ToUpperInvariant(), 10, FontWeights.SemiBold, muted);
        heading.Margin = new Thickness(0, 0, 0, 7);
        root.Children.Add(heading);
        value.FontSize = 20;
        value.FontWeight = FontWeights.Bold;
        value.Foreground = tone;
        value.Text = "—";
        root.Children.Add(value);
        return TacticalPanel(root, new Thickness(0, 0, 6, 0));
    }

    private UIElement BuildTacticalScoutView()
    {
        allyMetric = new TextBlock();
        enemyMetric = new TextBlock();
        scoutFoundCount = new TextBlock();
        scoutReadyCount = new TextBlock();
        scoutLimitedCount = new TextBlock();
        scoutFailureCount = new TextBlock();
        rosterLabels.Clear();
        rosterAvatars.Clear();
        rosterBadges.Clear();
        rosterHeroes.Clear();
        scoutVersion = "";
        scoutSignalKey = "";
        playersVersion = "";

        var pageContent = new StackPanel();
        var heading = new DockPanel { LastChildFill = true, Margin = new Thickness(0, 0, 0, 11) };
        var actionRow = new StackPanel { Orientation = Orientation.Horizontal,
            VerticalAlignment = VerticalAlignment.Center };
        var rescout = Btn("◎  Разведать соперников", async () =>
        {
            var ids = state.At("team", "enemy").Items()
                .Select(n => n.L()).Where(id => id > 0).Distinct().ToArray();
            if (ids.Length == 0)
            {
                toast.Text = "Добавь противников через «Управление составами».";
                return;
            }
            await RunAction("/api/ops/scout", new { players = string.Join(",", ids) });
        });
        rescout.Margin = new Thickness(0, 0, 9, 0);
        actionRow.Children.Add(rescout);
        actionRow.Children.Add(Btn("↻  Обновить", LoadScoutAsync, true));
        DockPanel.SetDock(actionRow, Dock.Right);
        heading.Children.Add(actionRow);
        var title = new StackPanel();
        title.Children.Add(Text("PRE-MATCH  /  INTELLIGENCE", 10, FontWeights.Bold, accent));
        var largeTitle = Text("Разведка противников", 21, FontWeights.Bold);
        largeTitle.Margin = new Thickness(0, 5, 0, 5);
        title.Children.Add(largeTitle);
        title.Children.Add(Text("Состав, источники и сигнатурные герои перед драфтом.",
            12, color: muted));
        heading.Children.Add(title);
        pageContent.Children.Add(heading);

        var metrics = new UniformGrid { Rows = 1, Columns = 4, Margin = new Thickness(0, 0, 0, 10) };
        metrics.Children.Add(TacticalStatistic("Найдено игроков", scoutFoundCount, foreground));
        metrics.Children.Add(TacticalStatistic("Данные готовы", scoutReadyCount, accent));
        metrics.Children.Add(TacticalStatistic("Ограниченные данные", scoutLimitedCount, gold));
        metrics.Children.Add(TacticalStatistic("Ошибки источников", scoutFailureCount, Hex("#ED8185")));
        pageContent.Children.Add(metrics);

        var opponentSection = new StackPanel();
        opponentSection.Children.Add(Text("ПРОТИВНИКИ  /  5 ПОЗИЦИЙ", 10, FontWeights.Bold, accent));
        var sub = Text("Выбери игрока, чтобы посмотреть подтверждённые статистические сведения.",
            12, color: muted);
        sub.Margin = new Thickness(0, 5, 0, 14);
        opponentSection.Children.Add(sub);
        scoutSignalHost = new StackPanel();
        opponentSection.Children.Add(scoutSignalHost);
        pageContent.Children.Add(TacticalPanel(opponentSection, new Thickness(0, 0, 0, 10)));

        var split = new Grid();
        split.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        split.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        split.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        split.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });

        var intel = new StackPanel();
        intel.Children.Add(Text("MATCH INTELLIGENCE", 10, FontWeights.Bold, accent));
        var threats = Text("Сводка по составу", 16, FontWeights.Bold);
        threats.Margin = new Thickness(0, 6, 0, 12);
        intel.Children.Add(threats);
        tacticalInsights = new StackPanel();
        intel.Children.Add(tacticalInsights);
        var intelCard = TacticalPanel(intel, new Thickness(0, 0, 8, 0));
        split.Children.Add(intelCard);

        var details = new StackPanel();
        details.Children.Add(Text("PLAYER PROFILE", 10, FontWeights.Bold, accent));
        var caption = Text("Данные выбранного игрока", 16, FontWeights.Bold);
        caption.Margin = new Thickness(0, 6, 0, 12);
        details.Children.Add(caption);
        scoutDetails = new StackPanel();
        var detailScroll = Scroll(scoutDetails);
        detailScroll.MaxHeight = 410;
        details.Children.Add(detailScroll);
        var detailCard = TacticalPanel(details, new Thickness(8, 0, 0, 0));
        Grid.SetColumn(detailCard, 1);
        split.Children.Add(detailCard);
        split.SizeChanged += (_, _) =>
        {
            bool compact = split.ActualWidth < 750;
            split.ColumnDefinitions[1].Width = compact
                ? new GridLength(0) : new GridLength(1, GridUnitType.Star);
            Grid.SetColumn(detailCard, compact ? 0 : 1);
            Grid.SetRow(detailCard, compact ? 1 : 0);
            intelCard.Margin = compact ? new Thickness(0, 0, 0, 12) : new Thickness(0, 0, 8, 0);
            detailCard.Margin = compact ? new Thickness(0) : new Thickness(8, 0, 0, 0);
        };
        pageContent.Children.Add(split);

        var activityRow = new DockPanel { Margin = new Thickness(0, 10, 0, 8),
            LastChildFill = true };
        scoutLive = Text("Ожидание разведки", 12, color: muted);
        var activityCaption = Text("СТАТУС ЗАДАЧ  /  ", 10, FontWeights.Bold, accent);
        DockPanel.SetDock(activityCaption, Dock.Left);
        activityRow.Children.Add(activityCaption);
        activityRow.Children.Add(scoutLive);
        pageContent.Children.Add(activityRow);

        // Keep every existing editing action, but out of the primary intel
        // path. The expansion state does not affect the underlying Go session.
        var editor = new StackPanel();
        var roster = new Grid();
        roster.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        roster.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        roster.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        roster.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        var allies = TacticalPanel(BuildSide("our"), new Thickness(0, 0, 8, 0));
        var enemies = TacticalPanel(BuildSide("enemy"), new Thickness(8, 0, 0, 0));
        roster.Children.Add(allies);
        Grid.SetColumn(enemies, 1);
        roster.Children.Add(enemies);
        roster.SizeChanged += (_, _) =>
        {
            bool compact = roster.ActualWidth < 850;
            roster.ColumnDefinitions[1].Width = compact
                ? new GridLength(0) : new GridLength(1, GridUnitType.Star);
            Grid.SetColumn(enemies, compact ? 0 : 1);
            Grid.SetRow(enemies, compact ? 1 : 0);
            allies.Margin = compact ? new Thickness(0, 0, 0, 11) : new Thickness(0, 0, 8, 0);
            enemies.Margin = compact ? new Thickness(0) : new Thickness(8, 0, 0, 0);
        };
        teamGrid = roster;
        editor.Children.Add(roster);

        var directory = new StackPanel { Margin = new Thickness(0, 14, 0, 0) };
        directory.Children.Add(Text("АДРЕСНАЯ КНИГА STEAM", 11, FontWeights.Bold, muted));
        var filter = new TextBox { Margin = new Thickness(0, 10, 0, 10),
            ToolTip = "Фильтр по имени, позиции или Steam account ID" };
        filter.TextChanged += (_, _) =>
        {
            knownPlayerFilter = filter.Text;
            FilterKnownPlayers();
        };
        directory.Children.Add(filter);
        knownPlayersList = new ListBox { Name = "KnownPlayers", MaxHeight = 270,
            MinHeight = 95, BorderThickness = new Thickness(0),
            Background = Brushes.Transparent };
        VirtualizingStackPanel.SetIsVirtualizing(knownPlayersList, true);
        VirtualizingStackPanel.SetVirtualizationMode(knownPlayersList, VirtualizationMode.Recycling);
        ScrollViewer.SetCanContentScroll(knownPlayersList, true);
        directory.Children.Add(knownPlayersList);
        bookEmptyHint = Text("Пока нет сохранённых игроков", 12, color: muted);
        directory.Children.Add(bookEmptyHint);
        scoutedPlayers = new WrapPanel { Margin = new Thickness(0, 10, 0, 0) };
        directory.Children.Add(scoutedPlayers);
        editor.Children.Add(TacticalPanel(directory, new Thickness(0, 14, 0, 0)));

        var editing = new Expander {
            Header = "Управление составами и сохранёнными игроками",
            IsExpanded = false,
            Margin = new Thickness(0, 11, 0, 0),
            Foreground = foreground,
            Content = editor,
            Padding = new Thickness(0, 7, 0, 0)
        };
        pageContent.Children.Add(editing);

        scoutViewport = Scroll(pageContent);
        return scoutViewport;
    }

    private UIElement BuildTacticalDraftView()
    {
        heroCatalogKey = "";
        recommendationKey = "";
        heroPageSize = 48;
        heroSearchText = "";
        selectedHeroId = 0;
        draftBoardKey = "";
        tacticalSelectedKey = "";

        var root = new StackPanel();

        var heading = new DockPanel { LastChildFill = true, Margin = new Thickness(0, 0, 0, 10) };
        var newDraft = Btn("＋  Новый драфт", async () => await RunAction("/api/new-draft"), true);
        DockPanel.SetDock(newDraft, Dock.Right);
        heading.Children.Add(newDraft);
        var title = new StackPanel();
        title.Children.Add(Text("CAPTAINS MODE  /  TACTICAL DRAFT", 10, FontWeights.Bold, accent));
        var name = Text("Драфт", 21, FontWeights.Bold);
        name.Margin = new Thickness(0, 6, 0, 5);
        title.Children.Add(name);
        title.Children.Add(Text("Выбери героя по текущему состоянию драфта и данным модели.",
            12, color: muted));
        heading.Children.Add(title);
        root.Children.Add(heading);

        draftBoardHost = new StackPanel();
        root.Children.Add(TacticalPanel(draftBoardHost, new Thickness(0, 0, 0, 10), true));

        var workspace = new Grid();
        workspace.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(169) });
        workspace.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        workspace.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(195) });
        for (int i = 0; i < 3; i++)
            workspace.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });

        var recommendations = new StackPanel();
        recommendations.Children.Add(Text("RECOMMENDED", 10, FontWeights.Bold, accent));
        var recTitle = Text("Рекомендации", 15, FontWeights.Bold);
        recTitle.Margin = new Thickness(0, 6, 0, 6);
        recommendations.Children.Add(recTitle);
        recommendations.Children.Add(Text("Оценки модели · без выдуманных процентов",
            11, color: muted));
        draftRecommendations = new ListBox {
            MinHeight = 180,
            MaxHeight = 360,
            Margin = new Thickness(0, 13, 0, 12)
        };
        recommendations.Children.Add(draftRecommendations);
        var historyTitle = Text("ИСТОРИЯ ДРАФТА", 10, FontWeights.Bold, muted);
        historyTitle.Margin = new Thickness(0, 13, 0, 10);
        recommendations.Children.Add(historyTitle);
        var actions = new WrapPanel();
        var undo = Btn("↶  Назад", async () => await RunAction("/api/undo"), true);
        undo.Margin = new Thickness(0, 0, 6, 8);
        actions.Children.Add(undo);
        actions.Children.Add(Btn("↷  Вперёд", async () => await RunAction("/api/redo"), true));
        recommendations.Children.Add(actions);
        var left = TacticalPanel(recommendations, new Thickness(0, 0, 8, 0));
        workspace.Children.Add(left);

        var catalog = new StackPanel();
        var filterHeading = new DockPanel { LastChildFill = true };
        heroResultsCount = Text("Герои", 11, FontWeights.SemiBold, muted);
        DockPanel.SetDock(heroResultsCount, Dock.Right);
        filterHeading.Children.Add(heroResultsCount);
        var poolTitle = new StackPanel();
        poolTitle.Children.Add(Text("HERO POOL", 10, FontWeights.Bold, accent));
        var mainLabel = Text("Каталог героев", 15, FontWeights.Bold);
        mainLabel.Margin = new Thickness(0, 6, 0, 0);
        poolTitle.Children.Add(mainLabel);
        filterHeading.Children.Add(poolTitle);
        catalog.Children.Add(filterHeading);

        heroSearchBox = new TextBox { Margin = new Thickness(0, 9, 0, 9),
            ToolTip = "Поиск героя (Ctrl+F)" };
        System.Windows.Automation.AutomationProperties.SetName(heroSearchBox, "Поиск героев");
        heroSearchBox.TextChanged += (_, _) =>
        {
            heroSearchText = heroSearchBox.Text;
            heroPageSize = 48;
            UpdateHeroes(heroSearchText);
        };
        catalog.Children.Add(heroSearchBox);
        heroGallery = new WrapPanel();
        var gallery = Scroll(heroGallery);
        gallery.Height = 365;
        catalog.Children.Add(gallery);
        heroShowMore = Btn("Показать ещё", async () =>
        {
            heroPageSize += 48;
            UpdateHeroes(heroSearchText);
            await System.Threading.Tasks.Task.CompletedTask;
        }, true);
        heroShowMore.Margin = new Thickness(0, 11, 0, 0);
        heroShowMore.HorizontalAlignment = HorizontalAlignment.Stretch;
        catalog.Children.Add(heroShowMore);
        var center = TacticalPanel(catalog, new Thickness(8, 0, 8, 0));
        Grid.SetColumn(center, 1);
        workspace.Children.Add(center);

        var selection = new StackPanel();
        selection.Children.Add(Text("SELECTED HERO", 10, FontWeights.Bold, accent));
        tacticalSelection = new StackPanel { Margin = new Thickness(0, 12, 0, 13) };
        selection.Children.Add(tacticalSelection);
        selectedHeroHint = Text("Герой не выбран", 11, FontWeights.SemiBold, muted);
        selectedHeroHint.Margin = new Thickness(0, 9, 0, 8);
        selection.Children.Add(selectedHeroHint);
        applyDraftButton = Btn("Применить героя  →", async () =>
        {
            if (selectedHeroId > 0)
                await RunAction("/api/apply", new { heroId = selectedHeroId });
        });
        applyDraftButton.IsEnabled = false;
        applyDraftButton.HorizontalContentAlignment = HorizontalAlignment.Center;
        selection.Children.Add(applyDraftButton);
        var actionTitle = Text("ТЕКУЩИЙ ХОД", 10, FontWeights.Bold, muted);
        actionTitle.Margin = new Thickness(0, 15, 0, 8);
        selection.Children.Add(actionTitle);
        var stateBox = new Border {
            Background = Hex("#102B3B"), BorderBrush = border,
            BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(7),
            Padding = new Thickness(10, 9, 10, 9)
        };
        draftSummary = Text("Ожидание модели…", 12);
        stateBox.Child = draftSummary;
        selection.Children.Add(stateBox);
        var right = TacticalPanel(selection, new Thickness(8, 0, 0, 0));
        Grid.SetColumn(right, 2);
        workspace.Children.Add(right);

        workspace.SizeChanged += (_, _) =>
        {
            bool compact = workspace.ActualWidth < 690;
            if (compact)
            {
                workspace.ColumnDefinitions[0].Width = new GridLength(0);
                workspace.ColumnDefinitions[1].Width = new GridLength(1, GridUnitType.Star);
                workspace.ColumnDefinitions[2].Width = new GridLength(0);
                Grid.SetColumn(center, 1); Grid.SetRow(center, 0);
                Grid.SetColumn(left, 1); Grid.SetRow(left, 1);
                Grid.SetColumn(right, 1); Grid.SetRow(right, 2);
                center.Margin = new Thickness(0, 0, 0, 11);
                left.Margin = new Thickness(0, 0, 0, 11);
                right.Margin = new Thickness(0);
            }
            else
            {
                workspace.ColumnDefinitions[0].Width = new GridLength(238);
                workspace.ColumnDefinitions[1].Width = new GridLength(1, GridUnitType.Star);
                workspace.ColumnDefinitions[2].Width = new GridLength(245);
                Grid.SetColumn(left, 0); Grid.SetRow(left, 0);
                Grid.SetColumn(center, 1); Grid.SetRow(center, 0);
                Grid.SetColumn(right, 2); Grid.SetRow(right, 0);
                left.Margin = new Thickness(0, 0, 8, 0);
                center.Margin = new Thickness(8, 0, 8, 0);
                right.Margin = new Thickness(8, 0, 0, 0);
            }
        };
        root.Children.Add(workspace);
        RefreshSelectedHeroDetail();
        return Scroll(root);
    }

    private UIElement TacticalHeroTile(int id)
    {
        var stack = new StackPanel();
        stack.Children.Add(HeroPortrait(id, 50, 32));
        var title = Text(HeroName(id), 9, FontWeights.SemiBold);
        title.Width = 50;
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
        button.Width = 58;
        button.Height = 60;
        button.Padding = new Thickness(2);
        button.Margin = new Thickness(0, 0, 3, 4);
        button.Background = id == selectedHeroId ? Hex("#1B5858") : Hex("#172738");
        button.BorderBrush = id == selectedHeroId ? accent : border;
        return button;
    }

    private void SelectTacticalHero(int id)
    {
        selectedHeroId = id;
        if (selectedHeroHint != null)
            selectedHeroHint.Text = "Выбран: " + HeroName(id);
        if (applyDraftButton != null) applyDraftButton.IsEnabled = true;
        HighlightDraftSelection();
        RefreshSelectedHeroDetail();
    }

    private void RefreshSelectedHeroDetail()
    {
        if (tacticalSelection == null) return;
        var selectedRecommendation = state.At("result", "recs").Items()
            .FirstOrDefault(r => r.At("heroId").I() == selectedHeroId);
        var key = selectedHeroId + "|" + (selectedRecommendation?.At("score").D().ToString("R") ?? "missing");
        if (key == tacticalSelectedKey && tacticalSelection.Children.Count > 0) return;
        tacticalSelectedKey = key;
        tacticalSelection.Children.Clear();
        if (state == null || selectedHeroId <= 0)
        {
            var empty = new Border {
                Background = Hex("#112737"), BorderBrush = border,
                BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(8),
                Padding = new Thickness(12), MinHeight = 110
            };
            empty.Child = Text("Нажми на портрет героя или рекомендацию.\n\nЗдесь появятся герой и оценка модели.",
                12, color: muted);
            tacticalSelection.Children.Add(empty);
            return;
        }

        var selected = selectedHeroId;
        tacticalSelection.Children.Add(HeroPortrait(selected, 157, 91));
        var heroName = Text(HeroName(selected), 16, FontWeights.Bold);
        heroName.Margin = new Thickness(0, 10, 0, 7);
        tacticalSelection.Children.Add(heroName);
        var rec = state.At("result", "recs").Items()
            .FirstOrDefault(r => r.At("heroId").I() == selected);
        if (rec != null)
        {
            var score = rec.At("score").D();
            tacticalSelection.Children.Add(Pill("Оценка модели: " + score.ToString("0.000"),
                Hex("#1B4D51"), accent));
        }
        else
        {
            tacticalSelection.Children.Add(Text("В топе рекомендаций сейчас не представлен.",
                11, color: muted));
        }
        var meta = Text("Проверь роль и состав команды перед применением.",
            11, color: muted);
        meta.Margin = new Thickness(0, 12, 0, 0);
        tacticalSelection.Children.Add(meta);
    }
}
