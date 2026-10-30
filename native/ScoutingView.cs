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
    private bool compactScout;
    private StackPanel? tacticalInsights;
    private TextBlock? scoutFoundCount;
    private TextBlock? scoutReadyCount;
    private TextBlock? scoutLimitedCount;
    private TextBlock? scoutFailureCount;

    private Border TacticalStatistic(string label, TextBlock value, Brush tone)
    {
        var root = new Grid();
        root.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
        root.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        value.Text = "—";
        value.FontSize = 16;
        value.FontWeight = FontWeights.SemiBold;
        value.Foreground = tone;
        root.Children.Add(value);
        var heading = Text(label, 11, color: muted);
        heading.VerticalAlignment = VerticalAlignment.Center;
        heading.Margin = new Thickness(8, 0, 0, 0);
        Grid.SetColumn(heading, 1); root.Children.Add(heading);
        return new Border { Child = root, Padding = new Thickness(0, 6, 8, 6) };
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

        var pageContent = new Grid { Name = "ScoutingWorkspace" };
        pageContent.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        pageContent.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        pageContent.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        pageContent.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        pageContent.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
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
        var refresh = Btn("↻", LoadScoutAsync, true);
        refresh.ToolTip = "Обновить данные разведки";
        System.Windows.Automation.AutomationProperties.SetName(refresh, "Обновить данные разведки");
        actionRow.Children.Add(refresh);
        DockPanel.SetDock(actionRow, Dock.Right);
        heading.Children.Add(actionRow);
        var title = new StackPanel();

        var largeTitle = Text("Противники", 18, FontWeights.Bold);
        largeTitle.Margin = new Thickness(0, 5, 0, 5);
        title.Children.Add(largeTitle);

        heading.Children.Add(title);
        pageContent.Children.Add(heading);

        var metrics = new UniformGrid { Rows = 1, Columns = 4, Margin = new Thickness(0, 0, 0, 10) };
        metrics.Children.Add(TacticalStatistic("игроков", scoutFoundCount, foreground));
        metrics.Children.Add(TacticalStatistic("готово", scoutReadyCount, accent));
        metrics.Children.Add(TacticalStatistic("неполные данные", scoutLimitedCount, gold));
        metrics.Children.Add(TacticalStatistic("ошибки", scoutFailureCount, (Brush)FindResource("ErrorBrush")));
        Grid.SetRow(metrics, 1); pageContent.Children.Add(metrics);

        scoutSignalHost = new StackPanel { Name = "OpponentCards", Margin = new Thickness(0, 0, 0, 10) };
        Grid.SetRow(scoutSignalHost, 2); pageContent.Children.Add(scoutSignalHost);

        var split = new Grid();
        split.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        split.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        var intel = new Grid();
        intel.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        intel.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        var intelTitle = Text("АНАЛИТИКА СОСТАВА", 10, FontWeights.Bold, accent);
        intelTitle.Margin = new Thickness(0, 0, 0, 12); intel.Children.Add(intelTitle);
        tacticalInsights = new StackPanel();
        var intelScroll = Scroll(tacticalInsights);
        Grid.SetRow(intelScroll, 1); intel.Children.Add(intelScroll);
        split.Children.Add(TacticalPanel(intel, new Thickness(0, 0, 6, 0)));
        var details = new Grid();
        details.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        details.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
        var caption = Text("ПРОФИЛЬ ИГРОКА", 10, FontWeights.Bold, accent);
        caption.Margin = new Thickness(0, 0, 0, 12); details.Children.Add(caption);
        scoutDetails = new StackPanel { Name = "ScoutPlayerDetails" };
        var detailScroll = Scroll(scoutDetails);
        Grid.SetRow(detailScroll, 1); details.Children.Add(detailScroll);
        var detailCard = TacticalPanel(details, new Thickness(6, 0, 0, 0));
        Grid.SetColumn(detailCard, 1); split.Children.Add(detailCard);
        Grid.SetRow(split, 3); pageContent.Children.Add(split);

        var activityRow = new DockPanel { Margin = new Thickness(0, 10, 0, 8),
            LastChildFill = true };
        scoutLive = Text("Ожидание разведки", 12, color: muted);
        var activityCaption = Text("СТАТУС ЗАДАЧ  /  ", 10, FontWeights.Bold, accent);
        DockPanel.SetDock(activityCaption, Dock.Left);
        activityRow.Children.Add(activityCaption);
        activityRow.Children.Add(scoutLive);
        Grid.SetRow(activityRow, 4); pageContent.Children.Add(activityRow);

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

        var editorScroll = Scroll(editor);
        editorScroll.Visibility = Visibility.Collapsed;
        Grid.SetRow(editorScroll, 1); Grid.SetRowSpan(editorScroll, 3);
        pageContent.Children.Add(editorScroll);
        var editToggle = Btn("Составы и адресная книга", async () =>
        {
            bool editing = editorScroll.Visibility != Visibility.Visible;
            editorScroll.Visibility = editing ? Visibility.Visible : Visibility.Collapsed;
            metrics.Visibility = scoutSignalHost.Visibility = split.Visibility = editing ? Visibility.Collapsed : Visibility.Visible;
            await System.Threading.Tasks.Task.CompletedTask;
        }, true);
        editToggle.Margin = new Thickness(8, 0, 0, 0);
        DockPanel.SetDock(editToggle, Dock.Right);
        activityRow.Children.Insert(0, editToggle);
        pageContent.SizeChanged += (_, _) =>
        {
            bool compact = pageContent.ActualHeight < 510;
            heading.Margin = new Thickness(0, 0, 0, compact ? 3 : 11);
            metrics.Margin = new Thickness(0, 0, 0, compact ? 4 : 10);
            activityRow.Margin = new Thickness(0, compact ? 4 : 10, 0, 0);
            activityCaption.Visibility = compact ? Visibility.Collapsed : Visibility.Visible;
            intelTitle.Margin = caption.Margin = new Thickness(0, 0, 0, compact ? 6 : 12);
            if (compactScout == compact) return;
            compactScout = compact;
            scoutSignalKey = "";
            RefreshScoutSignals();
        };
        scoutViewport = new ScrollViewer { Content = pageContent,
            VerticalScrollBarVisibility = ScrollBarVisibility.Disabled,
            HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled };
        return scoutViewport;
    }
}
