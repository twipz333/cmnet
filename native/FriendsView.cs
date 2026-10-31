using System;
using System.IO;
using System.Text.Json.Nodes;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using Microsoft.Win32;

namespace Cmnet.Native;

/// <summary>
/// Gate 2.5 Friends — intentional local file import/export only.
/// Public relay / room codes stay unavailable under CMNET_LOCAL_ONLY.
/// </summary>
public sealed partial class NativeWindow
{
    private Border? friendsEvidenceBanner;
    private TextBlock? friendsStatusText;
    private TextBlock? friendsPreviewText;
    private TextBlock? friendsFootnote;
    private Button? friendsExportButton;
    private Button? friendsImportPickButton;
    private Button? friendsImportConfirmButton;
    private string? friendsPendingImportPath;
    private string friendsLoadError = "";
    private JsonObject? previewFriendsShare;

    private UIElement BuildShareView()
    {
        friendsEvidenceBanner = BuildEvidenceBanner("FriendsEvidenceBanner");
        friendsStatusText = Text("", 13, FontWeights.SemiBold, accent);
        friendsStatusText.Name = "FriendsStatusText";
        friendsStatusText.TextWrapping = TextWrapping.Wrap;
        friendsPreviewText = Text("", 12, color: muted);
        friendsPreviewText.Name = "FriendsPreviewText";
        friendsPreviewText.TextWrapping = TextWrapping.Wrap;
        friendsPendingImportPath = null;

        var host = new StackPanel { Name = "FriendsWorkspace" };

        var heading = new DockPanel { LastChildFill = true, Margin = new Thickness(0, 0, 0, 10) };
        var titleCol = new StackPanel();
        titleCol.Children.Add(Text("FRIENDS  /  05", 10, FontWeights.Bold, accent));
        var headingText = Text("Обмен пакетом с друзьями", 22, FontWeights.Bold);
        headingText.Margin = new Thickness(0, 6, 0, 4);
        titleCol.Children.Add(headingText);
        titleCol.Children.Add(Text(
            "Только локальный файл: капитан сохраняет пакет, друг загружает его у себя. Без облака и публичного relay.",
            12, color: muted));
        heading.Children.Add(titleCol);
        host.Children.Add(heading);
        host.Children.Add(friendsEvidenceBanner);

        var stateBox = new StackPanel();
        stateBox.Children.Add(Text("ЛОКАЛЬНЫЙ ОБМЕН", 10, FontWeights.Bold, muted));
        friendsStatusText.Margin = new Thickness(0, 8, 0, 0);
        stateBox.Children.Add(friendsStatusText);
        friendsPreviewText.Margin = new Thickness(0, 8, 0, 0);
        stateBox.Children.Add(friendsPreviewText);
        host.Children.Add(Surface(stateBox, new Thickness(0, 0, 0, 16)));

        var cards = new Grid();
        cards.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        cards.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        cards.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        cards.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });

        var export = new StackPanel();
        export.Children.Add(Text("01  ·  ЭКСПОРТ", 10, FontWeights.Bold, accent));
        var exportTitle = Text("Сохранить пакет", 18, FontWeights.SemiBold);
        exportTitle.Margin = new Thickness(0, 8, 0, 8);
        export.Children.Add(exportTitle);
        export.Children.Add(Text(
            "Модель (bundle) и/или разведка (snapshot) в один файл .cmnetpack. Передай другу любым своим каналом.",
            13, color: muted));
        var exportActions = new WrapPanel { Margin = new Thickness(0, 16, 0, 0) };
        friendsExportButton = Btn("Сохранить пакет…", ExportFriendsPackageAsync);
        friendsExportButton.Name = "FriendsExportButton";
        friendsExportButton.ToolTip = "Выбрать файл и записать локальный пакет через /api/share/export";
        System.Windows.Automation.AutomationProperties.SetName(friendsExportButton, "Сохранить пакет для друзей");
        friendsExportButton.Margin = new Thickness(0, 0, 8, 8);
        exportActions.Children.Add(friendsExportButton);
        export.Children.Add(exportActions);
        var exportCard = Surface(export, new Thickness(0, 0, 9, 0));
        cards.Children.Add(exportCard);

        var import = new StackPanel();
        import.Children.Add(Text("02  ·  ИМПОРТ", 10, FontWeights.Bold, gold));
        var importTitle = Text("Загрузить пакет", 18, FontWeights.SemiBold);
        importTitle.Margin = new Thickness(0, 8, 0, 8);
        import.Children.Add(importTitle);
        import.Children.Add(Text(
            "Сначала просмотр содержимого, затем подтверждение. Перед записью создаётся резервная копия в import-backup/.",
            13, color: muted));
        var importActions = new WrapPanel { Margin = new Thickness(0, 16, 0, 0) };
        friendsImportPickButton = Btn("Выбрать файл…", PreviewFriendsImportAsync, true);
        friendsImportPickButton.Name = "FriendsImportPickButton";
        friendsImportPickButton.ToolTip = "Открыть .cmnetpack и показать сводку без записи";
        System.Windows.Automation.AutomationProperties.SetName(friendsImportPickButton, "Выбрать пакет для импорта");
        friendsImportPickButton.Margin = new Thickness(0, 0, 8, 8);
        importActions.Children.Add(friendsImportPickButton);
        friendsImportConfirmButton = Btn("Подтвердить импорт", ConfirmFriendsImportAsync);
        friendsImportConfirmButton.Name = "FriendsImportConfirmButton";
        friendsImportConfirmButton.IsEnabled = false;
        friendsImportConfirmButton.ToolTip = "Записать пакет в локальную папку данных после подтверждения";
        System.Windows.Automation.AutomationProperties.SetName(friendsImportConfirmButton, "Подтвердить импорт пакета");
        friendsImportConfirmButton.Margin = new Thickness(0, 0, 8, 8);
        importActions.Children.Add(friendsImportConfirmButton);
        import.Children.Add(importActions);
        var importCard = Surface(import, new Thickness(9, 0, 0, 0));
        Grid.SetColumn(importCard, 1);
        cards.Children.Add(importCard);

        bool compactLayout = false;
        cards.SizeChanged += (_, _) =>
        {
            bool compact = cards.ActualWidth < 700;
            if (compact == compactLayout) return;
            compactLayout = compact;
            cards.ColumnDefinitions[1].Width = compact ? new GridLength(0) : new GridLength(1, GridUnitType.Star);
            Grid.SetColumn(importCard, compact ? 0 : 1);
            Grid.SetRow(importCard, compact ? 1 : 0);
            exportCard.Margin = compact ? new Thickness(0, 0, 0, 12) : new Thickness(0, 0, 9, 0);
            importCard.Margin = compact ? new Thickness(0) : new Thickness(9, 0, 0, 0);
        };
        host.Children.Add(cards);

        friendsFootnote = Text(
            "CMNET_LOCAL_ONLY: публичный relay и коды комнат недоступны. Обмен — только через файл, который вы сами передаёте.",
            12, color: muted);
        friendsFootnote.Name = "FriendsFootnote";
        friendsFootnote.Margin = new Thickness(0, 18, 0, 0);
        friendsFootnote.TextWrapping = TextWrapping.Wrap;
        host.Children.Add(friendsFootnote);

        RefreshFriendsView();
        return Scroll(host);
    }

    private void RefreshFriendsView()
    {
        if (friendsStatusText == null || friendsEvidenceBanner == null) return;

        if (isVisualPreview && previewFriendsShare != null)
        {
            ApplyFriendsShareNode(previewFriendsShare);
            return;
        }

        if (!coreOnline || core.BaseUri == null)
        {
            SetEvidenceBanner(friendsEvidenceBanner, EvidenceKind.Offline,
                "Ядро офлайн", "Экспорт и импорт недоступны без локального advisor.", forceShow: true);
            friendsStatusText.Text = "Нет соединения с Go-ядром.";
            friendsPreviewText!.Text = "";
            SetFriendsActionsEnabled(false, false);
            return;
        }

        var share = state?.At("share") as JsonObject;
        if (share == null)
        {
            SetEvidenceBanner(friendsEvidenceBanner, EvidenceKind.Loading,
                "Читаем состояние обмена", "Ждём share из /api/state…", forceShow: true);
            friendsStatusText.Text = "Проверяем локальный режим…";
            friendsPreviewText!.Text = "";
            SetFriendsActionsEnabled(false, false);
            return;
        }

        ApplyFriendsShareNode(share);
    }

    private void ApplyFriendsShareNode(JsonObject share)
    {
        bool localOnly = share.At("localOnly").B(true);
        bool fileMode = share.At("fileMode").B(localOnly);
        bool canExport = share.At("canExport").B();
        bool canImport = share.At("canImport").B();
        string err = share.At("error").S();
        if (string.IsNullOrWhiteSpace(err)) err = friendsLoadError;

        if (!string.IsNullOrWhiteSpace(err))
        {
            SetEvidenceBanner(friendsEvidenceBanner, EvidenceKind.Error,
                "Ошибка обмена", err, forceShow: true);
        }
        else if (!fileMode && !localOnly)
        {
            SetEvidenceBanner(friendsEvidenceBanner, EvidenceKind.Partial,
                "Relay-режим вне local-only",
                "Эта сборка допускает публичный relay; native клиент всё равно предпочитает файловый обмен.",
                forceShow: true);
        }
        else if (!canExport && !canImport)
        {
            SetEvidenceBanner(friendsEvidenceBanner, EvidenceKind.Empty,
                "Обмен недоступен",
                "Нет модели/разведки для экспорта и нет обработчика импорта.",
                forceShow: true);
        }
        else if (share["importPreview"] is JsonObject)
        {
            SetEvidenceBanner(friendsEvidenceBanner, EvidenceKind.Partial,
                "Пакет готов к подтверждению",
                "Проверь сводку ниже и нажми «Подтвердить импорт». Без подтверждения ничего не записывается.",
                forceShow: true);
        }
        else
        {
            SetEvidenceBanner(friendsEvidenceBanner, EvidenceKind.Ready,
                "Локальный обмен готов",
                "Сохрани пакет для друга или загрузи полученный файл. Relay отключён.",
                forceShow: true);
        }

        friendsStatusText!.Text = DescribeFriendsStatus(share);
        friendsPreviewText!.Text = DescribeFriendsPreview(share);
        bool confirmReady = !string.IsNullOrWhiteSpace(friendsPendingImportPath) ||
                            share["importPreview"] is JsonObject;
        SetFriendsActionsEnabled(canExport && coreOnline, canImport && coreOnline, confirmReady && canImport && coreOnline);
    }

    private static string DescribeFriendsStatus(JsonObject share)
    {
        var lastExport = share["lastExport"] as JsonObject;
        var lastImport = share["lastImport"] as JsonObject;
        if (lastImport != null)
        {
            var path = lastImport.At("path").S();
            var backup = lastImport.At("backupNote").S();
            var line = "Последний импорт: " + (string.IsNullOrWhiteSpace(path) ? "готово" : Path.GetFileName(path));
            if (!string.IsNullOrWhiteSpace(backup)) line += " · " + backup;
            return line;
        }
        if (lastExport != null)
        {
            var path = lastExport.At("path").S();
            return "Последний экспорт: " + (string.IsNullOrWhiteSpace(path) ? "готово" : Path.GetFileName(path));
        }
        bool localOnly = share.At("localOnly").B(true);
        return localOnly
            ? "Режим: только файл (CMNET_LOCAL_ONLY). Публичный relay недоступен."
            : "Режим: файловый обмен доступен; публичный relay не используется этим экраном.";
    }

    private static string DescribeFriendsPreview(JsonObject share)
    {
        var preview = share["importPreview"] as JsonObject ?? share["lastImport"] as JsonObject ?? share["lastExport"] as JsonObject;
        if (preview == null) return "Пакет может содержать bundle.json (модель) и/или snapshot.json (разведка).";
        var meta = preview["meta"] as JsonObject;
        var bits = new System.Collections.Generic.List<string>();
        var bundle = meta.At("bundleVersion").S();
        var patch = meta.At("patchTag").S();
        var hasSnap = meta.At("hasSnapshot").B();
        var scouted = meta.At("scouted").I();
        if (!string.IsNullOrWhiteSpace(bundle)) bits.Add("бандл " + TruncateFriends(bundle, 14));
        if (!string.IsNullOrWhiteSpace(patch)) bits.Add("патч " + patch);
        if (hasSnap) bits.Add(scouted > 0 ? $"разведка {scouted}" : "разведка");
        var bytes = preview.At("bytes").L();
        if (bytes <= 0) bytes = preview.At("bytes").I();
        if (bytes > 0) bits.Add($"{bytes / 1024.0:0} КБ");
        return bits.Count == 0 ? "Метаданные пакета пусты." : string.Join(" · ", bits);
    }

    private static string TruncateFriends(string s, int n) => s.Length <= n ? s : s[..n];

    private void SetFriendsActionsEnabled(bool exportEnabled, bool importEnabled, bool confirmEnabled = false)
    {
        if (friendsExportButton != null)
        {
            friendsExportButton.IsEnabled = exportEnabled;
            friendsExportButton.Opacity = exportEnabled ? 1 : 0.45;
        }
        if (friendsImportPickButton != null)
        {
            friendsImportPickButton.IsEnabled = importEnabled;
            friendsImportPickButton.Opacity = importEnabled ? 1 : 0.45;
        }
        if (friendsImportConfirmButton != null)
        {
            friendsImportConfirmButton.IsEnabled = confirmEnabled;
            friendsImportConfirmButton.Opacity = confirmEnabled ? 1 : 0.45;
        }
    }

    private async Task ExportFriendsPackageAsync()
    {
        if (isVisualPreview)
        {
            toast.Text = "SAMPLE DATA · экспорт недоступен в preview";
            return;
        }
        var dialog = new SaveFileDialog
        {
            Title = "Сохранить пакет cmnet для друга",
            Filter = "CMNET package|*.cmnetpack|All files|*.*",
            FileName = $"cmnet-friends-{DateTime.Now:yyyyMMdd-HHmm}.cmnetpack",
            AddExtension = true,
            DefaultExt = ".cmnetpack"
        };
        if (dialog.ShowDialog(this) != true) return;
        try
        {
            await RunAction("/api/share/export", new { path = dialog.FileName });
            RefreshFriendsView();
        }
        catch (Exception ex)
        {
            friendsLoadError = ex.Message;
            MessageBox.Show(this, ex.Message, "Экспорт пакета", MessageBoxButton.OK, MessageBoxImage.Warning);
            RefreshFriendsView();
        }
    }

    private async Task PreviewFriendsImportAsync()
    {
        if (isVisualPreview)
        {
            toast.Text = "SAMPLE DATA · импорт недоступен в preview";
            return;
        }
        var dialog = new OpenFileDialog
        {
            Title = "Выбрать пакет cmnet",
            Filter = "CMNET package|*.cmnetpack|All files|*.*",
            CheckFileExists = true
        };
        if (dialog.ShowDialog(this) != true) return;
        friendsPendingImportPath = dialog.FileName;
        try
        {
            await RunAction("/api/share/import", new { path = dialog.FileName, confirm = false });
            RefreshFriendsView();
            toast.Text = "Пакет прочитан — нажми «Подтвердить импорт», если всё верно.";
        }
        catch (Exception ex)
        {
            friendsPendingImportPath = null;
            friendsLoadError = ex.Message;
            MessageBox.Show(this, ex.Message, "Импорт пакета", MessageBoxButton.OK, MessageBoxImage.Warning);
            RefreshFriendsView();
        }
    }

    private async Task ConfirmFriendsImportAsync()
    {
        if (isVisualPreview)
        {
            toast.Text = "SAMPLE DATA · подтверждение недоступно в preview";
            return;
        }
        var path = friendsPendingImportPath
                   ?? state?.At("share", "importPreview", "path").S();
        if (string.IsNullOrWhiteSpace(path))
        {
            MessageBox.Show(this, "Сначала выбери файл пакета.", "Импорт пакета",
                MessageBoxButton.OK, MessageBoxImage.Information);
            return;
        }
        var finalConfirm = MessageBox.Show(this,
            "Записать пакет в локальную папку данных?\n\n" + path +
            "\n\nСуществующие bundle.json / snapshot.json будут скопированы в import-backup/ перед перезаписью.",
            "Подтвердить импорт",
            MessageBoxButton.OKCancel,
            MessageBoxImage.Warning);
        if (finalConfirm != MessageBoxResult.OK) return;
        try
        {
            await RunAction("/api/share/import", new { path, confirm = true });
            friendsPendingImportPath = null;
            friendsLoadError = "";
            RefreshFriendsView();
        }
        catch (Exception ex)
        {
            friendsLoadError = ex.Message;
            MessageBox.Show(this, ex.Message, "Импорт пакета", MessageBoxButton.OK, MessageBoxImage.Warning);
            RefreshFriendsView();
        }
    }

    /// <summary>
    /// Gate 2.5 Friends honesty fixtures. Outside geometry.json (CI Count==33).
    /// </summary>
    internal void LoadFriendsPreviewScenario(string scenario)
    {
        LoadVisualFixture("Друзья");
        state!["meta"]!["previewOffline"] = false;
        coreOnline = true;
        ApplyConnectionChrome("Go-ядро подключено");
        friendsLoadError = "";
        friendsPendingImportPath = null;
        previewFriendsShare = SampleFriendsShare("ready");

        if (scenario is "ready" or "happy")
        {
            previewFriendsShare = SampleFriendsShare("ready");
        }
        else if (scenario == "empty")
        {
            previewFriendsShare = SampleFriendsShare("empty");
        }
        else if (scenario == "partial")
        {
            previewFriendsShare = SampleFriendsShare("partial");
            friendsPendingImportPath = @"C:\sample\friend-pack.cmnetpack";
        }
        else if (scenario == "loading")
        {
            previewFriendsShare = null;
        }
        else if (scenario == "error")
        {
            friendsLoadError = "SAMPLE · не удалось прочитать пакет";
            previewFriendsShare = SampleFriendsShare("error");
        }
        else if (scenario == "offline")
        {
            state["meta"]!["previewOffline"] = true;
            coreOnline = false;
            ApplyConnectionChrome("SAMPLE · офлайн");
            previewFriendsShare = SampleFriendsShare("ready");
        }
        else if (scenario == "imported")
        {
            previewFriendsShare = SampleFriendsShare("imported");
        }

        if (scenario == "loading")
        {
            if (friendsStatusText != null) friendsStatusText.Text = "SAMPLE · загрузка состояния…";
            if (friendsPreviewText != null) friendsPreviewText.Text = "";
            SetEvidenceBanner(friendsEvidenceBanner, EvidenceKind.Loading,
                "Загрузка", "SAMPLE DATA · ждём share state", forceShow: true);
            SetFriendsActionsEnabled(false, false);
        }
        else
        {
            RefreshFriendsView();
        }
        toast.Text = "SAMPLE DATA · friends-" + scenario;
    }

    private static JsonObject SampleFriendsShare(string kind)
    {
        return kind switch
        {
            "empty" => new JsonObject
            {
                ["localOnly"] = true,
                ["fileMode"] = true,
                ["canExport"] = false,
                ["canImport"] = true,
                ["guest"] = false
            },
            "partial" => new JsonObject
            {
                ["localOnly"] = true,
                ["fileMode"] = true,
                ["canExport"] = true,
                ["canImport"] = true,
                ["importPreview"] = new JsonObject
                {
                    ["path"] = @"C:\sample\friend-pack.cmnetpack",
                    ["bytes"] = 128000,
                    ["meta"] = new JsonObject
                    {
                        ["bundleVersion"] = "sample-bundle",
                        ["patchTag"] = "7.39",
                        ["hasSnapshot"] = true,
                        ["scouted"] = 5
                    }
                }
            },
            "error" => new JsonObject
            {
                ["localOnly"] = true,
                ["fileMode"] = true,
                ["canExport"] = true,
                ["canImport"] = true,
                ["error"] = "SAMPLE · файл не похож на локальный пакет cmnet"
            },
            "imported" => new JsonObject
            {
                ["localOnly"] = true,
                ["fileMode"] = true,
                ["canExport"] = true,
                ["canImport"] = true,
                ["lastImport"] = new JsonObject
                {
                    ["path"] = @"C:\sample\friend-pack.cmnetpack",
                    ["bytes"] = 128000,
                    ["backupNote"] = "резервная копия: C:\\sample\\import-backup\\20261031-120000",
                    ["meta"] = new JsonObject
                    {
                        ["bundleVersion"] = "sample-bundle",
                        ["patchTag"] = "7.39",
                        ["hasSnapshot"] = true,
                        ["scouted"] = 5
                    }
                }
            },
            _ => new JsonObject
            {
                ["localOnly"] = true,
                ["fileMode"] = true,
                ["canExport"] = true,
                ["canImport"] = true,
                ["lastExport"] = new JsonObject
                {
                    ["path"] = @"C:\sample\cmnet-friends.cmnetpack",
                    ["bytes"] = 256000,
                    ["meta"] = new JsonObject
                    {
                        ["bundleVersion"] = "sample-bundle",
                        ["patchTag"] = "7.39",
                        ["hasSnapshot"] = true,
                        ["scouted"] = 3
                    }
                }
            }
        };
    }
}
