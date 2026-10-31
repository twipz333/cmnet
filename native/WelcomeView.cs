using System;
using System.Diagnostics;
using System.IO;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using Microsoft.Win32;

namespace Cmnet.Native;

/// <summary>
/// In-app first-run / welcome: choose existing cmnet.db folder, browse, or start
/// fresh. No console, no SDK, no Electron welcome.html dependency.
/// </summary>
public sealed partial class NativeWindow
{
    private bool showingWelcome; // true while first-run blocks shell navigation
    private StackPanel? welcomeList;
    private TextBlock? welcomeError;

    private bool ShouldShowWelcome(string? explicitDataDir)
        => !isVisualPreview
           && string.IsNullOrWhiteSpace(explicitDataDir)
           && DesktopSettings.NeedsSetup();

    private UIElement BuildWelcomeView()
    {
        showingWelcome = true;
        heading.Text = "Первый запуск";
        subtitle.Text = "Выберите папку данных · без консоли и SDK";

        var host = new StackPanel { Name = "WelcomeWorkspace", MaxWidth = 720 };
        host.HorizontalAlignment = HorizontalAlignment.Left;

        host.Children.Add(Text("ПЕРВЫЙ ЗАПУСК", 10, FontWeights.Bold, accent));
        var title = Text("Где лежат ваши данные?", 26, FontWeights.Bold);
        title.Margin = new Thickness(0, 10, 0, 8);
        title.FontFamily = new FontFamily("Segoe UI Variable Display");
        host.Children.Add(title);
        host.Children.Add(Text(
            "Если cmnet уже работал на этом компьютере, у вас есть папка с cmnet.db: собранные матчи и обученная модель. Выберите её — и всё продолжится с того же места. Не нужны терминал, Go или .NET SDK.",
            13, color: muted));

        var banner = BuildEvidenceBanner("WelcomeEvidenceBanner");
        SetEvidenceBanner(banner, EvidenceKind.Empty,
            "Нужно выбрать папку данных",
            "Пока папка не выбрана, ядро и модель не запускаются. Это ожидаемый первый экран, не ошибка.",
            forceShow: true);
        banner.Visibility = Visibility.Visible;
        banner.Margin = new Thickness(0, 18, 0, 14);
        host.Children.Add(banner);

        welcomeList = new StackPanel { Margin = new Thickness(0, 0, 0, 8) };
        welcomeList.Children.Add(Text("Ищу папки с cmnet.db…", 13, color: muted));
        host.Children.Add(welcomeList);

        var actions = new WrapPanel { Margin = new Thickness(0, 18, 0, 0) };
        var browse = Btn("Указать папку вручную", BrowseWelcomeFolderAsync, true);
        browse.Margin = new Thickness(0, 0, 10, 8);
        System.Windows.Automation.AutomationProperties.SetName(browse, "Указать папку данных вручную");
        actions.Children.Add(browse);

        var browseDb = Btn("Указать cmnet.db…", BrowseWelcomeDbAsync, true);
        browseDb.Margin = new Thickness(0, 0, 10, 8);
        System.Windows.Automation.AutomationProperties.SetName(browseDb, "Указать файл cmnet.db");
        actions.Children.Add(browseDb);

        var fresh = Btn("Начать с нуля", FreshWelcomeAsync);
        fresh.Margin = new Thickness(0, 0, 10, 8);
        fresh.ToolTip = "Создать новую папку в %APPDATA%\\cmnet\\data";
        System.Windows.Automation.AutomationProperties.SetName(fresh, "Начать с нуля");
        actions.Children.Add(fresh);
        host.Children.Add(actions);

        welcomeError = Text("", 13, color: (Brush)FindResource("ErrorBrush"));
        welcomeError.Margin = new Thickness(0, 12, 0, 0);
        welcomeError.TextWrapping = TextWrapping.Wrap;
        host.Children.Add(welcomeError);

        var foot = Text(
            $"По умолчанию новая папка: {DesktopSettings.DefaultDataDirectory()}",
            11, color: muted);
        foot.Margin = new Thickness(0, 22, 0, 0);
        host.Children.Add(foot);

        _ = PopulateWelcomeScanAsync();
        return Scroll(host);
    }

    private async Task PopulateWelcomeScanAsync()
    {
        if (welcomeList == null) return;
        var found = await Task.Run(() =>
        {
            var extras = new[]
            {
                AppContext.BaseDirectory,
                DesktopSettings.DefaultDataDirectory(),
                core.DataDirectory
            };
            return DesktopSettings.Scan(extras);
        });
        if (welcomeList == null) return;
        welcomeList.Children.Clear();
        if (found.Count == 0)
        {
            welcomeList.Children.Add(Text(
                "Папок с cmnet.db не нашлось. Укажите файл вручную или начните с нуля.",
                13, color: muted));
            return;
        }
        foreach (var item in found)
        {
            var capture = item;
            var card = new Border
            {
                Background = ThemeBrush("RaisedBrush"),
                BorderBrush = border,
                BorderThickness = new Thickness(1),
                CornerRadius = new CornerRadius(8),
                Padding = new Thickness(14, 12, 14, 12),
                Margin = new Thickness(0, 0, 0, 8),
                Cursor = System.Windows.Input.Cursors.Hand
            };
            var row = new DockPanel { LastChildFill = true };
            var go = Text("Использовать", 12, FontWeights.SemiBold, accent);
            go.VerticalAlignment = VerticalAlignment.Center;
            go.Margin = new Thickness(12, 0, 0, 0);
            DockPanel.SetDock(go, Dock.Right);
            row.Children.Add(go);
            var col = new StackPanel();
            col.Children.Add(Text(capture.Dir, 13, FontWeights.SemiBold));
            col.Children.Add(Text(
                $"{DesktopSettings.FormatSize(capture.DbBytes)}, {DesktopSettings.FormatAgo(capture.ModifiedUtc)}" +
                (capture.HasBundle ? ", модель обучена" : ", модели ещё нет"),
                12, color: muted));
            row.Children.Add(col);
            card.Child = row;
            card.MouseLeftButtonUp += async (_, _) => await UseWelcomeDirAsync(capture.Dir);
            welcomeList.Children.Add(card);
        }
    }

    private async Task BrowseWelcomeFolderAsync()
    {
        ClearWelcomeError();
        var dialog = new OpenFolderDialog { Title = "Папка с cmnet.db (и при наличии bundle.json)" };
        if (dialog.ShowDialog(this) != true) return;
        var described = DesktopSettings.Describe(dialog.FolderName);
        if (described == null)
        {
            SetWelcomeError("В этой папке нет cmnet.db. Выберите папку данных cmnet или начните с нуля.");
            return;
        }
        await UseWelcomeDirAsync(described.Dir);
    }

    private async Task BrowseWelcomeDbAsync()
    {
        ClearWelcomeError();
        var dialog = new OpenFileDialog
        {
            Title = "Укажите cmnet.db",
            Filter = "cmnet database|cmnet.db|All files|*.*",
            CheckFileExists = true
        };
        if (dialog.ShowDialog(this) != true) return;
        var dir = Path.GetDirectoryName(dialog.FileName);
        if (string.IsNullOrWhiteSpace(dir) || DesktopSettings.Describe(dir) == null)
        {
            SetWelcomeError("Не удалось прочитать выбранный cmnet.db.");
            return;
        }
        await UseWelcomeDirAsync(dir);
    }

    private async Task FreshWelcomeAsync()
    {
        ClearWelcomeError();
        var dir = DesktopSettings.DefaultDataDirectory();
        try
        {
            Directory.CreateDirectory(dir);
        }
        catch (Exception ex)
        {
            SetWelcomeError("Не удалось создать папку: " + ex.Message);
            return;
        }
        await UseWelcomeDirAsync(dir);
    }

    private async Task UseWelcomeDirAsync(string dir)
    {
        ClearWelcomeError();
        try
        {
            DesktopSettings.SaveChoice(dir);
            showingWelcome = false;
            toast.Text = "Папка данных выбрана. Запускаю ядро…";
            await BootAsync(dir);
        }
        catch (Exception ex)
        {
            showingWelcome = true;
            SetWelcomeError(ex.Message);
        }
    }

    private void ClearWelcomeError()
    {
        if (welcomeError == null) return;
        welcomeError.Text = "";
    }

    private void SetWelcomeError(string message)
    {
        if (welcomeError != null) welcomeError.Text = message;
        toast.Text = message;
    }

    private void ShowWelcomeOverlay()
    {
        body.Content = BuildWelcomeView();
        foreach (var item in navigation)
        {
            item.Value.IsEnabled = false;
            item.Value.Opacity = 0.45;
        }
        status.Text = "Первый запуск · выберите папку данных";
        ApplyConnectionChrome("Ожидание выбора папки");
    }

    private void EnableNavigationAfterWelcome()
    {
        foreach (var item in navigation)
        {
            item.Value.IsEnabled = true;
            item.Value.Opacity = 1;
        }
    }
}
