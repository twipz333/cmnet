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
        heroCatalogKey = recommendationKey = draftBoardKey = tacticalSelectedKey = "";
        var root = new Grid();
        root.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        root.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        root.RowDefinitions.Add(new RowDefinition());
        var heading = new DockPanel { Margin = new Thickness(0, 0, 0, 8) };
        var newDraft = Btn("＋ Новый драфт", async () => await RunAction("/api/new-draft"), true);
        newDraft.VerticalAlignment = VerticalAlignment.Center;
        DockPanel.SetDock(newDraft, Dock.Right); heading.Children.Add(newDraft);
        heading.Children.Add(Text("CAPTAINS MODE", 12, FontWeights.Bold, accent));
        root.Children.Add(heading);
        draftBoardHost = new StackPanel();
        var board = TacticalPanel(draftBoardHost, new Thickness(0, 0, 0, 8));
        Grid.SetRow(board, 1); root.Children.Add(board);
        var workspace = new Grid();
        workspace.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(184) });
        workspace.ColumnDefinitions.Add(new ColumnDefinition());
        workspace.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(210) });
        Grid.SetRow(workspace, 2); root.Children.Add(workspace);
        var recommendations = new Grid();
        recommendations.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        recommendations.RowDefinitions.Add(new RowDefinition());
        recommendations.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        recommendations.Children.Add(Text("РЕКОМЕНДАЦИИ", 10, FontWeights.Bold, accent));
        draftRecommendations = new ListBox { Margin = new Thickness(0, 8, 0, 6) };
        draftRecommendations.ItemContainerStyle = (Style)FindResource("CompactListItem");
        Grid.SetRow(draftRecommendations, 1); recommendations.Children.Add(draftRecommendations);
        var actions = new UniformGrid { Columns = 2 };
        actions.Children.Add(Btn("↶", async () => await RunAction("/api/undo"), true));
        actions.Children.Add(Btn("↷", async () => await RunAction("/api/redo"), true));
        ((Button)actions.Children[0]).ToolTip = "Отменить ход";
        ((Button)actions.Children[1]).ToolTip = "Повторить ход";
        Grid.SetRow(actions, 2); recommendations.Children.Add(actions);
        var left = TacticalPanel(recommendations, new Thickness(0, 0, 6, 0));
        left.Name = "DraftRecommendations"; workspace.Children.Add(left);
        var catalog = new Grid();
        catalog.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        catalog.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        catalog.RowDefinitions.Add(new RowDefinition());
        var filterHeading = new DockPanel();
        heroResultsCount = Text("", 10, color: muted);
        DockPanel.SetDock(heroResultsCount, Dock.Right); filterHeading.Children.Add(heroResultsCount);
        filterHeading.Children.Add(Text("ГЕРОИ", 10, FontWeights.Bold, accent));
        catalog.Children.Add(filterHeading);
        heroSearchBox = new TextBox { Text = heroSearchText, Margin = new Thickness(0, 8, 0, 8), ToolTip = "Поиск героя (Ctrl+F)", Height = 32 };
        System.Windows.Automation.AutomationProperties.SetName(heroSearchBox, "Поиск героев");
        heroSearchBox.TextChanged += (_, _) => { heroSearchText = heroSearchBox.Text; UpdateHeroes(heroSearchText); };
        Grid.SetRow(heroSearchBox, 1); catalog.Children.Add(heroSearchBox);
        heroGallery = new HeroCatalog();
        Grid.SetRow(heroGallery, 2); catalog.Children.Add(heroGallery);
        var center = TacticalPanel(catalog, new Thickness(0, 0, 6, 0));
        center.Name = "DraftCatalog"; Grid.SetColumn(center, 1); workspace.Children.Add(center);
        var selection = new Grid();
        selection.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        selection.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        selection.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        selection.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        selection.RowDefinitions.Add(new RowDefinition());
        selection.Children.Add(Text("ВЫБРАННЫЙ ГЕРОЙ", 10, FontWeights.Bold, accent));
        tacticalSelection = new StackPanel { Margin = new Thickness(0, 8, 0, 8) };
        var selectedScroll = Scroll(tacticalSelection);
        Grid.SetRow(selectedScroll, 1); selection.Children.Add(selectedScroll);
        draftSummary = Text("Ожидание модели…", 11, color: muted);
        draftSummary.MaxHeight = 58; draftSummary.TextTrimming = TextTrimming.CharacterEllipsis;
        draftSummary.Margin = new Thickness(0, 5, 0, 8);
        Grid.SetRow(draftSummary, 2); selection.Children.Add(draftSummary);
        selectedHeroHint = Text("", 11);
        applyDraftButton = Btn("Применить героя →", async () =>
        {
            if (CanApplyHero(selectedHeroId)) await RunAction("/api/apply", new { heroId = selectedHeroId });
        });
        applyDraftButton.Name = "ApplyHero";
        applyDraftButton.IsEnabled = CanApplyHero(selectedHeroId);
        Grid.SetRow(applyDraftButton, 3); selection.Children.Add(applyDraftButton);
        var right = TacticalPanel(selection, new Thickness(0));
        right.Name = "DraftContext"; Grid.SetColumn(right, 2); workspace.Children.Add(right);
        workspace.SizeChanged += (_, _) =>
        {
            selectedScroll.MaxHeight = Math.Max(48, Math.Min(240, workspace.ActualHeight - 136));
            bool compact = workspace.ActualWidth < 850;
            workspace.ColumnDefinitions[0].Width = new GridLength(compact ? 166 : 210);
            workspace.ColumnDefinitions[2].Width = new GridLength(compact ? 180 : 230);
        };
        RefreshSelectedHeroDetail();
        return root;
    }

    private bool CanApplyHero(int id) => id > 0 && state.At("meta", "draftReady").B()
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
                Background = ThemeBrush("RaisedBrush"), BorderBrush = border,
                BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(4),
                Padding = new Thickness(10), MinHeight = 110
            };
            empty.Child = Text("Нажми на портрет героя или рекомендацию.\n\nЗдесь появятся герой и оценка модели.",
                12, color: muted);
            tacticalSelection.Children.Add(empty);
            return;
        }

        var selected = selectedHeroId;

        var heroName = Text(HeroName(selected), 16, FontWeights.Bold);
        heroName.Margin = new Thickness(0, 10, 0, 7);
        tacticalSelection.Children.Add(heroName);
        tacticalSelection.Children.Add(HeroPortrait(selected, 140, 70));
        var rec = state.At("result", "recs").Items()
            .FirstOrDefault(r => r.At("heroId").I() == selected);
        if (rec != null)
        {
            var score = rec.At("score").D();
            tacticalSelection.Children.Add(Pill("Оценка модели: " + score.ToString("0.000"),
                ThemeBrush("PrimaryBrush"), accent));
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
