using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Linq;
using System.Text.Json.Nodes;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using Microsoft.Win32;

namespace Cmnet.Native;

/// <summary>
/// Gate 2.3 GSI install UX (Settings tab) + Draft live/readonly status chrome.
/// Install uses /api/gsi/*; live vs manual is always derived from meta.gsiMode/gsiSeen
/// (never faked as live).
/// </summary>
public sealed partial class NativeWindow
{
    private StackPanel? gsiHost;
    private TextBlock? gsiStatusTitle;
    private TextBlock? gsiStatusDetail;
    private TextBlock? gsiPathLine;
    private TextBlock? draftGsiBadge;
    private Button? gsiInstallButton;
    private Button? gsiBrowseButton;
    private Button? gsiRefreshButton;
    private Button? gsiOpenFolderButton;
    private string gsiCfgDirOverride = "";
    private JsonObject? previewGsiStatus;
    private EvidenceKind lastGsiEvidence = EvidenceKind.Ready;

    private UIElement BuildGsiPane()
    {
        var box = new StackPanel { Name = "GsiWorkspace" };
        box.Children.Add(Text("DOTA GAME STATE INTEGRATION", 10, FontWeights.Bold, accent));
        var title = Text("Установка конфига и честный статус", 16, FontWeights.SemiBold);
        title.Margin = new Thickness(0, 8, 0, 8);
        box.Children.Add(title);
        box.Children.Add(Text(
            "CMNET пишет gamestate_integration_advisor.cfg в папку Dota. Живой драфт появляется только когда ядро запущено с gsi: true и Dota шлёт события. Без GSI ручной режим работает как раньше.",
            12, color: muted));

        gsiStatusTitle = Text("Проверяю…", 14, FontWeights.SemiBold);
        gsiStatusTitle.Name = "GsiStatusTitle";
        gsiStatusTitle.Margin = new Thickness(0, 16, 0, 4);
        box.Children.Add(gsiStatusTitle);

        gsiStatusDetail = Text("", 12, color: muted);
        gsiStatusDetail.Name = "GsiStatusDetail";
        gsiStatusDetail.TextWrapping = TextWrapping.Wrap;
        box.Children.Add(gsiStatusDetail);

        gsiPathLine = Text("", 12, color: accent);
        gsiPathLine.Name = "GsiPathLine";
        gsiPathLine.Margin = new Thickness(0, 10, 0, 0);
        gsiPathLine.TextWrapping = TextWrapping.Wrap;
        box.Children.Add(gsiPathLine);

        var actions = new StackPanel
        {
            Orientation = Orientation.Horizontal,
            Margin = new Thickness(0, 16, 0, 0)
        };
        gsiInstallButton = Btn("Установить конфиг GSI", InstallGsiAsync);
        gsiInstallButton.Name = "GsiInstall";
        gsiInstallButton.Margin = new Thickness(0, 0, 8, 0);
        gsiInstallButton.ToolTip = "Записать gamestate_integration_advisor.cfg через /api/gsi/install";
        System.Windows.Automation.AutomationProperties.SetName(gsiInstallButton, "Установить конфиг GSI");
        actions.Children.Add(gsiInstallButton);

        gsiBrowseButton = Btn("Указать папку…", BrowseGsiFolderAsync, true);
        gsiBrowseButton.Margin = new Thickness(0, 0, 8, 0);
        gsiBrowseButton.ToolTip = "Выбрать …/dota/cfg/gamestate_integration вручную";
        System.Windows.Automation.AutomationProperties.SetName(gsiBrowseButton, "Указать папку GSI");
        actions.Children.Add(gsiBrowseButton);

        gsiRefreshButton = Btn("Обновить статус", RefreshGsiStatusAsync, true);
        gsiRefreshButton.Margin = new Thickness(0, 0, 8, 0);
        System.Windows.Automation.AutomationProperties.SetName(gsiRefreshButton, "Обновить статус GSI");
        actions.Children.Add(gsiRefreshButton);

        gsiOpenFolderButton = Btn("Открыть папку", OpenGsiFolderAsync, true);
        gsiOpenFolderButton.ToolTip = "Открыть обнаруженную/выбранную папку в Проводнике";
        System.Windows.Automation.AutomationProperties.SetName(gsiOpenFolderButton, "Открыть папку GSI");
        actions.Children.Add(gsiOpenFolderButton);
        box.Children.Add(actions);

        var steps = new StackPanel { Margin = new Thickness(0, 20, 0, 0) };
        steps.Children.Add(Text("КАК ВКЛЮЧИТЬ", 10, FontWeights.Bold, muted));
        steps.Children.Add(GsiStep("1", "Установи конфиг (кнопка выше) или укажи папку, если автопоиск не нашёл Dota."));
        steps.Children.Add(GsiStep("2", "Во вкладке «Приложение» включи ключ gsi и сохрани config.yaml."));
        steps.Children.Add(GsiStep("3", "Перезапусти CMNET — ядро поднимет слушатель GSI только при старте."));
        steps.Children.Add(GsiStep("4", "Перезапусти Dota, зайди в драфт. Статус на экране «Драфт»: жду событие → live (только чтение)."));
        steps.Children.Add(GsiStep("5", "Без GSI оставляй gsi: false — ручные пики/баны и undo/redo работают."));
        box.Children.Add(steps);

        gsiHost = box;
        return Surface(box, new Thickness(0));
    }

    private UIElement GsiStep(string n, string text)
    {
        var row = new DockPanel { Margin = new Thickness(0, 8, 0, 0), LastChildFill = true };
        var mark = Text(n, 12, FontWeights.Bold, accent);
        mark.Width = 18;
        DockPanel.SetDock(mark, Dock.Left);
        row.Children.Add(mark);
        var body = Text(text, 12, color: muted);
        body.TextWrapping = TextWrapping.Wrap;
        row.Children.Add(body);
        return row;
    }

    private async Task BrowseGsiFolderAsync()
    {
        var dialog = new OpenFolderDialog
        {
            Title = "Папка gamestate_integration (…\\dota\\cfg\\gamestate_integration)"
        };
        if (!string.IsNullOrWhiteSpace(gsiCfgDirOverride) && Directory.Exists(gsiCfgDirOverride))
            dialog.InitialDirectory = gsiCfgDirOverride;
        if (dialog.ShowDialog(this) != true) return;
        gsiCfgDirOverride = dialog.FolderName;
        await RefreshGsiStatusAsync();
    }

    private async Task OpenGsiFolderAsync()
    {
        var path = gsiCfgDirOverride;
        if (string.IsNullOrWhiteSpace(path) && gsiPathLine != null)
        {
            // Prefer cfgDir from last status paint stored in Tag.
            path = gsiPathLine.Tag as string ?? "";
        }
        if (string.IsNullOrWhiteSpace(path) || !Directory.Exists(path))
        {
            toast.Text = "Папка GSI ещё не известна — укажи её вручную.";
            return;
        }
        try
        {
            Process.Start(new ProcessStartInfo("explorer.exe", $"\"{path}\"") { UseShellExecute = true });
        }
        catch (Exception ex) { toast.Text = "Проводник: " + ex.Message; }
        await Task.CompletedTask;
    }

    private async Task InstallGsiAsync()
    {
        if (isVisualPreview)
        {
            previewGsiStatus = SampleGsiStatus(installed: true, enabled: false, mode: false, seen: false);
            ApplyGsiStatus(previewGsiStatus);
            ShowSaveBanner("SAMPLE · GSI cfg записан (fixture). Перезапусти Dota.");
            toast.Text = "SAMPLE DATA · gsi install";
            return;
        }
        if (core.BaseUri == null)
        {
            toast.Text = "Ядро офлайн — установка GSI недоступна.";
            return;
        }
        try
        {
            var payload = new Dictionary<string, string>();
            if (!string.IsNullOrWhiteSpace(gsiCfgDirOverride))
                payload["cfgDir"] = gsiCfgDirOverride;
            payload["addr"] = "127.0.0.1:54001";
            var body = await core.PostAsync("/api/gsi/install", payload);
            var node = JsonNode.Parse(body);
            var status = node.At("status").S("GSI config written");
            toast.Text = status;
            ShowSaveBanner(status);
            await RefreshGsiStatusAsync();
        }
        catch (Exception ex)
        {
            toast.Text = "GSI install: " + ex.Message;
            if (gsiStatusTitle != null)
            {
                gsiStatusTitle.Text = "Установка не удалась";
                gsiStatusTitle.Foreground = (Brush)FindResource("ErrorBrush");
            }
            if (gsiStatusDetail != null)
                gsiStatusDetail.Text = ex.Message;
        }
    }

    private async Task RefreshGsiStatusAsync()
    {
        if (gsiStatusTitle == null) return;
        if (isVisualPreview)
        {
            ApplyGsiStatus(previewGsiStatus ?? SampleGsiStatus(false, false, false, false));
            return;
        }
        if (core.BaseUri == null)
        {
            ApplyGsiStatus(new JsonObject
            {
                ["installed"] = false,
                ["detected"] = false,
                ["gsiEnabled"] = false,
                ["gsiMode"] = false,
                ["gsiSeen"] = false,
                ["note"] = "Ядро недоступно — статус GSI не подтверждён.",
                ["cfgDir"] = gsiCfgDirOverride,
                ["detectError"] = "offline"
            });
            return;
        }
        try
        {
            var path = "/api/gsi/status";
            if (!string.IsNullOrWhiteSpace(gsiCfgDirOverride))
                path += "?cfgDir=" + Uri.EscapeDataString(gsiCfgDirOverride);
            var st = await core.GetAsync(path);
            ApplyGsiStatus(st);
        }
        catch (Exception ex)
        {
            toast.Text = "GSI status: " + ex.Message;
            if (gsiStatusDetail != null) gsiStatusDetail.Text = ex.Message;
        }
    }

    private void ApplyGsiStatus(JsonObject st)
    {
        if (gsiStatusTitle == null || gsiStatusDetail == null) return;
        var installed = st.At("installed").B();
        var enabled = st.At("gsiEnabled").B();
        var mode = st.At("gsiMode").B();
        var seen = st.At("gsiSeen").B();
        var detected = st.At("detected").B();
        var kind = ResolveGsiEvidence(installed, enabled, mode, seen, detected, st.At("detectError").S(),
            out var title, out var detail);
        lastGsiEvidence = kind;
        var (label, tone, _) = EvidenceChrome(kind);
        gsiStatusTitle.Text = label + " · " + title;
        gsiStatusTitle.Foreground = kind == EvidenceKind.Ready ? foreground : tone;
        gsiStatusDetail.Text = string.IsNullOrWhiteSpace(detail) ? st.At("note").S() : detail;
        if (!string.IsNullOrWhiteSpace(st.At("note").S()) &&
            !string.Equals(gsiStatusDetail.Text, st.At("note").S(), StringComparison.Ordinal))
            gsiStatusDetail.Text += "\n" + st.At("note").S();

        if (gsiPathLine != null)
        {
            var dir = st.At("cfgDir").S(gsiCfgDirOverride);
            var cfgPath = st.At("cfgPath").S();
            gsiPathLine.Tag = dir;
            gsiPathLine.Text = string.IsNullOrWhiteSpace(cfgPath)
                ? (string.IsNullOrWhiteSpace(dir) ? "Путь не определён" : "Папка: " + dir)
                : (installed ? "Файл: " + cfgPath : "Ожидаемый файл: " + cfgPath);
        }
        RefreshSettingsEvidence();
    }

    private static EvidenceKind ResolveGsiEvidence(
        bool installed, bool enabled, bool mode, bool seen, bool detected, string detectError,
        out string title, out string detail)
    {
        title = "";
        detail = "";
        if (!string.IsNullOrWhiteSpace(detectError) && detectError == "offline")
        {
            title = "Ядро офлайн";
            detail = "Установка и проверка пути недоступны, пока Go-ядро не подключено.";
            return EvidenceKind.Offline;
        }
        if (!installed && !detected)
        {
            title = "Папка Dota не найдена";
            detail = "Укажи …/game/dota/cfg/gamestate_integration вручную.";
            return EvidenceKind.Empty;
        }
        if (!installed)
        {
            title = "Конфиг не установлен";
            detail = "Нажми «Установить конфиг GSI».";
            return EvidenceKind.Empty;
        }
        if (!enabled)
        {
            title = "Конфиг есть · слушатель выключен";
            detail = "Включи gsi в «Приложение» и перезапусти CMNET. Сейчас ручной режим.";
            return EvidenceKind.Partial;
        }
        if (mode && !seen)
        {
            title = "GSI ждёт событие";
            detail = "Доска только для чтения. Запусти драфт в Dota.";
            return EvidenceKind.Loading;
        }
        if (mode && seen)
        {
            title = "GSI live";
            detail = "Доска следует за Dota (только чтение).";
            return EvidenceKind.Ready;
        }
        title = "Конфиг готов";
        detail = "Ручной режим; live появится после включения gsi и перезапуска.";
        return EvidenceKind.Ready;
    }

    internal static JsonObject SampleGsiStatus(bool installed, bool enabled, bool mode, bool seen)
    {
        return new JsonObject
        {
            ["cfgDir"] = @"SAMPLE\Steam\steamapps\common\dota 2 beta\game\dota\cfg\gamestate_integration",
            ["cfgPath"] = @"SAMPLE\…\gamestate_integration_advisor.cfg",
            ["cfgFileName"] = "gamestate_integration_advisor.cfg",
            ["detected"] = true,
            ["installed"] = installed,
            ["gsiEnabled"] = enabled,
            ["gsiMode"] = mode,
            ["gsiSeen"] = seen,
            ["defaultAddr"] = "127.0.0.1:54001",
            ["note"] = "SAMPLE DATA · статус GSI (fixture, не живой Dota)."
        };
    }

    private void RefreshDraftGsiBadge()
    {
        if (draftGsiBadge == null || state == null) return;
        var mode = state.At("meta", "gsiMode").B();
        var seen = state.At("meta", "gsiSeen").B();
        if (!mode)
        {
            draftGsiBadge.Text = "●  Ручной режим";
            draftGsiBadge.Foreground = muted;
            draftGsiBadge.ToolTip = "GSI выключен — пики/баны вручную. Установка конфига: Настройки → GSI.";
            return;
        }
        if (!seen)
        {
            draftGsiBadge.Text = "○  GSI · жду событие";
            draftGsiBadge.Foreground = gold;
            draftGsiBadge.ToolTip = "Ядро в GSI-режиме, доска только для чтения. Ждём первый POST от Dota.";
            return;
        }
        draftGsiBadge.Text = "●  GSI · live (только чтение)";
        draftGsiBadge.Foreground = good;
        draftGsiBadge.ToolTip = "Доска следует за Dota. Ручной apply/undo отключены.";
    }

    private bool IsGsiReadonly() => state?.At("meta", "gsiMode").B() == true;

    internal void LoadGsiPreviewScenario(string scenario)
    {
        coreOnline = true;
        ApplyConnectionChrome("Go-ядро подключено");
        settingsLoading = false;
        settingsLoadError = null;
        HideSaveBanner();

        if (scenario.StartsWith("draft-", StringComparison.Ordinal))
        {
            LoadVisualFixture("Драфт");
            state!["meta"]!["previewOffline"] = false;
            state["meta"]!["draftReady"] = true;
            var kind = scenario["draft-".Length..];
            if (kind == "manual")
            {
                state["meta"]!["gsiMode"] = false;
                state["meta"]!["gsiSeen"] = false;
            }
            else if (kind == "waiting")
            {
                state["meta"]!["gsiMode"] = true;
                state["meta"]!["gsiSeen"] = false;
            }
            else if (kind == "live")
            {
                state["meta"]!["gsiMode"] = true;
                state["meta"]!["gsiSeen"] = true;
            }
            else if (kind == "offline")
            {
                state["meta"]!["gsiMode"] = false;
                state["meta"]!["gsiSeen"] = false;
                state["meta"]!["previewOffline"] = true;
                coreOnline = false;
                ApplyConnectionChrome("SAMPLE · офлайн");
            }
            draftBoardKey = heroCatalogKey = recommendationKey = tacticalSelectedKey = draftIntelKey = "";
            UpdateLiveState();
            toast.Text = "SAMPLE DATA · " + scenario;
            return;
        }

        LoadVisualFixture("Настройки");
        state!["meta"]!["previewOffline"] = false;
        ShowSettingsTab("gsi");
        previewGsiStatus = scenario switch
        {
            "missing" => SampleGsiStatus(false, false, false, false),
            "installed" => SampleGsiStatus(true, false, false, false),
            "waiting" => SampleGsiStatus(true, true, true, false),
            "live" => SampleGsiStatus(true, true, true, true),
            "offline" => new JsonObject
            {
                ["installed"] = false,
                ["detected"] = false,
                ["gsiEnabled"] = false,
                ["gsiMode"] = false,
                ["gsiSeen"] = false,
                ["note"] = "SAMPLE · ядро офлайн",
                ["detectError"] = "offline",
                ["cfgDir"] = ""
            },
            _ => SampleGsiStatus(true, false, false, false)
        };
        if (scenario == "offline")
        {
            state["meta"]!["previewOffline"] = true;
            coreOnline = false;
            ApplyConnectionChrome("SAMPLE · офлайн");
        }
        ApplyGsiStatus(previewGsiStatus);
        toast.Text = "SAMPLE DATA · gsi-" + scenario;
    }
}
