using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Globalization;
using System.Linq;
using System.Text.Json.Nodes;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using Microsoft.Win32;

namespace Cmnet.Native;

/// <summary>
/// Gate 2.2 Maintain / Settings. Typed config editor with validation + save
/// confirmation, About without auto-update, and an honest cut-list for tabs
/// that stay out of native local-first (Discord / Alerts / Rules / updates).
/// Evidence states share StateKit with Scout/Draft/Data.
/// </summary>
public sealed partial class NativeWindow
{
    private Border? settingsEvidenceBanner;
    private StackPanel? settingsTabBar;
    private ContentControl? settingsBodyHost;
    private StackPanel? configHost;
    private TextBlock? settingsSaveBanner;
    private TextBlock? settingsPathLine;
    private TextBlock? aboutFacts;
    private TextBlock? migrateStatusText;
    private Button? saveSettingsButton;
    private Button? reloadSettingsButton;
    private Button? openDataFolderButton;
    private Button? migrateRefreshButton;
    private Button? migrateBackupButton;
    private Button? migrateRunButton;
    private Button? migrateRollbackButton;
    private string settingsTab = "app";
    private EvidenceKind lastSettingsEvidence = EvidenceKind.Ready;
    private string? lastMigrateBackupPath;
    private bool settingsLoading;
    private string? settingsLoadError;
    private JsonObject? previewConfig;
    private readonly List<ConfigField> configEditors = new();

    private UIElement BuildSettingsView()
    {
        configEditors.Clear();
        settingsTab = "app";
        settingsLoading = false;
        settingsLoadError = null;

        var host = new StackPanel { Name = "SettingsWorkspace" };

        var heading = new DockPanel { LastChildFill = true, Margin = new Thickness(0, 0, 0, 10) };
        var actions = new StackPanel
        {
            Orientation = Orientation.Horizontal,
            VerticalAlignment = VerticalAlignment.Center
        };
        openDataFolderButton = Btn("📂  Папка данных", () =>
        {
            try
            {
                Process.Start(new ProcessStartInfo("explorer.exe", $"\"{core.DataDirectory}\"")
                {
                    UseShellExecute = true
                });
            }
            catch (Exception ex) { toast.Text = "Папка данных: " + ex.Message; }
            return Task.CompletedTask;
        }, true);
        openDataFolderButton.Margin = new Thickness(0, 0, 8, 0);
        openDataFolderButton.ToolTip = "Открыть локальную папку с cmnet.db / config.yaml / bundle.json";
        System.Windows.Automation.AutomationProperties.SetName(openDataFolderButton, "Открыть папку данных");
        actions.Children.Add(openDataFolderButton);

        reloadSettingsButton = Btn("Отменить изменения", LoadSettingsAsync, true);
        reloadSettingsButton.Margin = new Thickness(0, 0, 8, 0);
        reloadSettingsButton.ToolTip = "Перечитать config.yaml с диска через API ядра";
        System.Windows.Automation.AutomationProperties.SetName(reloadSettingsButton, "Отменить изменения настроек");
        actions.Children.Add(reloadSettingsButton);

        saveSettingsButton = Btn("Сохранить", SaveSettingsAsync);
        saveSettingsButton.ToolTip = "Записать отмеченные поля в config.yaml";
        System.Windows.Automation.AutomationProperties.SetName(saveSettingsButton, "Сохранить настройки");
        actions.Children.Add(saveSettingsButton);
        DockPanel.SetDock(actions, Dock.Right);
        heading.Children.Add(actions);

        var titleCol = new StackPanel();
        titleCol.Children.Add(Text("Настройки", 18, FontWeights.Bold));
        titleCol.Children.Add(Text("Понятные параметры · проверка перед записью · без мёртвых вкладок", 11, color: muted));
        heading.Children.Add(titleCol);
        host.Children.Add(heading);

        settingsEvidenceBanner = BuildEvidenceBanner("SettingsEvidenceBanner");
        host.Children.Add(settingsEvidenceBanner);

        settingsSaveBanner = Text("", 12, FontWeights.SemiBold, good);
        settingsSaveBanner.Name = "SettingsSaveBanner";
        settingsSaveBanner.Visibility = Visibility.Collapsed;
        settingsSaveBanner.Margin = new Thickness(0, 0, 0, 10);
        settingsSaveBanner.TextWrapping = TextWrapping.Wrap;
        host.Children.Add(settingsSaveBanner);

        settingsTabBar = new StackPanel
        {
            Orientation = Orientation.Horizontal,
            Margin = new Thickness(0, 0, 0, 14)
        };
        settingsTabBar.Children.Add(SettingsTabButton("app", "Приложение"));
        settingsTabBar.Children.Add(SettingsTabButton("gsi", "GSI"));
        settingsTabBar.Children.Add(SettingsTabButton("migrate", "Миграция"));
        settingsTabBar.Children.Add(SettingsTabButton("about", "О программе"));
        settingsTabBar.Children.Add(SettingsTabButton("deferred", "Вне объёма"));
        host.Children.Add(settingsTabBar);

        settingsPathLine = Text(core.DataDirectory, 12, color: accent);
        settingsPathLine.Margin = new Thickness(0, 0, 0, 12);
        settingsPathLine.TextWrapping = TextWrapping.Wrap;
        host.Children.Add(settingsPathLine);

        settingsBodyHost = new ContentControl();
        configHost = new StackPanel();
        aboutFacts = new TextBlock();
        host.Children.Add(settingsBodyHost);

        ShowSettingsTab("app");
        return Scroll(host);
    }

    private Button SettingsTabButton(string id, string label)
    {
        var b = Btn(label, async () =>
        {
            ShowSettingsTab(id);
            await Task.CompletedTask;
        }, true);
        b.Tag = id;
        b.Margin = new Thickness(0, 0, 8, 0);
        b.MinWidth = 110;
        System.Windows.Automation.AutomationProperties.SetName(b, "Вкладка настроек " + label);
        return b;
    }

    private void ShowSettingsTab(string id)
    {
        settingsTab = id;
        if (settingsTabBar != null)
        {
            foreach (Button child in settingsTabBar.Children.OfType<Button>())
            {
                bool on = (child.Tag as string) == id;
                child.Background = on ? ThemeBrush("PrimaryBrush") : soft;
                child.BorderBrush = on ? ThemeBrush("AccentBrush") : border;
            }
        }
        if (settingsBodyHost == null) return;
        settingsBodyHost.Content = id switch
        {
            "about" => BuildAboutPane(),
            "deferred" => BuildDeferredPane(),
            "gsi" => BuildGsiPane(),
            "migrate" => BuildMigratePane(),
            _ => BuildAppPaneShell()
        };
        if (id == "app" && configHost != null && configHost.Children.Count == 0 && !settingsLoading)
            _ = LoadSettingsAsync();
        if (id == "gsi")
            _ = RefreshGsiStatusAsync();
        if (id == "migrate")
            _ = RefreshMigrateStatusAsync();
        RefreshSettingsEvidence();
        if (saveSettingsButton != null)
            saveSettingsButton.Visibility = id == "app" ? Visibility.Visible : Visibility.Collapsed;
        if (reloadSettingsButton != null)
            reloadSettingsButton.Visibility = id == "app" ? Visibility.Visible : Visibility.Collapsed;
    }

    private UIElement BuildMigratePane()
    {
        var box = new StackPanel();
        box.Children.Add(Text("ELECTRON → NATIVE", 10, FontWeights.Bold, muted));
        box.Children.Add(Text(
            "Честный статус папок данных, timestamped backup и откат. Ничего не выдумываем: только то, что лежит на диске. Не открывайте Electron и native на одной cmnet.db одновременно.",
            12, color: muted));

        migrateStatusText ??= new TextBlock();
        migrateStatusText.Name = "MigrateStatusText";
        migrateStatusText.FontSize = 13;
        migrateStatusText.TextWrapping = TextWrapping.Wrap;
        migrateStatusText.LineHeight = 22;
        migrateStatusText.Margin = new Thickness(0, 14, 0, 0);
        migrateStatusText.Text = "Читаю статус…";
        // Tab rebuilds reparent the same TextBlock — detach first (WPF logical child rule).
        if (migrateStatusText.Parent is Panel prevPanel)
            prevPanel.Children.Remove(migrateStatusText);
        else if (migrateStatusText.Parent is ContentControl prevCc)
            prevCc.Content = null;
        box.Children.Add(migrateStatusText);

        var actions = new WrapPanel { Margin = new Thickness(0, 16, 0, 0) };
        migrateRefreshButton = Btn("Обновить статус", RefreshMigrateStatusAsync, true);
        migrateRefreshButton.Margin = new Thickness(0, 0, 8, 8);
        System.Windows.Automation.AutomationProperties.SetName(migrateRefreshButton, "Обновить статус миграции");
        actions.Children.Add(migrateRefreshButton);

        migrateBackupButton = Btn("Показать команду backup", async () =>
        {
            var cmd = $"advisor migrate backup --from \"{core.DataDirectory}\" --to \"{core.DataDirectory}\"";
            ShowSaveBanner("Закройте Electron и это окно, затем:\n" + cmd);
            toast.Text = "Команда backup скопирована в баннер (CLI при закрытых клиентах)";
            await Task.CompletedTask;
        }, true);
        migrateBackupButton.Margin = new Thickness(0, 0, 8, 8);
        migrateBackupButton.ToolTip = "Пока ядро держит БД, backup только через CLI после закрытия приложения";
        System.Windows.Automation.AutomationProperties.SetName(migrateBackupButton, "Показать команду backup");
        actions.Children.Add(migrateBackupButton);

        migrateRunButton = Btn("Показать команду migrate run", async () =>
        {
            var dest = DesktopSettings.DefaultDataDirectory();
            var cmd = $"advisor migrate run --from \"{core.DataDirectory}\" --to \"{dest}\" --update-desktop";
            ShowSaveBanner("Закройте Electron и это окно, затем:\n" + cmd);
            toast.Text = "Команда migrate run в баннере";
            await Task.CompletedTask;
        }, true);
        migrateRunButton.Margin = new Thickness(0, 0, 8, 8);
        migrateRunButton.ToolTip = "advisor migrate run — backup + copy; не из живой сессии с открытой БД";
        System.Windows.Automation.AutomationProperties.SetName(migrateRunButton, "Показать команду migrate run");
        actions.Children.Add(migrateRunButton);

        migrateRollbackButton = Btn("Показать команду rollback", async () =>
        {
            if (string.IsNullOrWhiteSpace(lastMigrateBackupPath))
            {
                toast.Text = "Нет backup — сначала advisor migrate backup при закрытых клиентах";
                return;
            }
            var cmd = $"advisor migrate rollback --backup \"{lastMigrateBackupPath}\" --to \"{core.DataDirectory}\"";
            ShowSaveBanner("Закройте Electron и это окно, затем:\n" + cmd);
            toast.Text = "Команда rollback в баннере";
            await Task.CompletedTask;
        }, true);
        migrateRollbackButton.Margin = new Thickness(0, 0, 8, 8);
        migrateRollbackButton.ToolTip = "advisor migrate rollback из migration-backup/<stamp>";
        System.Windows.Automation.AutomationProperties.SetName(migrateRollbackButton, "Показать команду rollback");
        actions.Children.Add(migrateRollbackButton);
        box.Children.Add(actions);

        var cli = Text(
            "CLI: advisor migrate status | backup | run --update-desktop | rollback --backup <path>\n" +
            "Документация и демо: docs/_gate0/migration-2b-wip/NOTES.md",
            11, color: muted);
        cli.Margin = new Thickness(0, 18, 0, 0);
        box.Children.Add(cli);
        return Surface(box, new Thickness(0));
    }

    private async Task RefreshMigrateStatusAsync()
    {
        if (migrateStatusText == null) return;
        migrateStatusText.Text = "Читаю статус…";
        try
        {
            if (isVisualPreview)
            {
                migrateStatusText.Text =
                    "SAMPLE · источник %APPDATA%\\cmnet\\data\n" +
                    "SAMPLE · назначение то же (одна папка)\n" +
                    "SAMPLE · ядро держит БД · backup (нет)\n" +
                    "SAMPLE · совет: закройте Electron перед migrate run";
                lastSettingsEvidence = EvidenceKind.Ready;
                SetEvidenceBanner(settingsEvidenceBanner, EvidenceKind.Ready,
                    "SAMPLE · миграция",
                    "Превью не читает ваш cmnet.db и не запускает migrate.",
                    forceShow: true);
                return;
            }
            var st = DataMigrate.InspectFilesystem(core.DataDirectory, coreOnline);
            migrateStatusText.Text = DataMigrate.FormatStatus(st);
            lastMigrateBackupPath = st.Backups.Length > 0 ? st.Backups[0] : null;
            if (st.NeedsMigrate)
            {
                lastSettingsEvidence = EvidenceKind.Partial;
                SetEvidenceBanner(settingsEvidenceBanner, EvidenceKind.Partial,
                    "Нужна миграция в native-папку",
                    st.Advice, forceShow: true);
            }
            else if (st.Backups.Length == 0)
            {
                lastSettingsEvidence = EvidenceKind.Empty;
                SetEvidenceBanner(settingsEvidenceBanner, EvidenceKind.Empty,
                    "Backup ещё не создавался",
                    st.Advice, forceShow: true);
            }
            else
            {
                lastSettingsEvidence = EvidenceKind.Ready;
                SetEvidenceBanner(settingsEvidenceBanner, EvidenceKind.Ready,
                    "Есть migration-backup",
                    st.Advice, forceShow: true);
            }
        }
        catch (Exception ex)
        {
            migrateStatusText.Text = "Статус недоступен: " + ex.Message;
            lastSettingsEvidence = EvidenceKind.Error;
            SetEvidenceBanner(settingsEvidenceBanner, EvidenceKind.Error,
                "Не удалось прочитать статус миграции",
                ex.Message, forceShow: true);
        }
        await Task.CompletedTask;
    }

    private UIElement BuildAppPaneShell()
    {
        var box = new StackPanel();
        box.Children.Add(Text(
            "Включённый переключатель слева — значение записано в config.yaml; выключенный оставляет значение по умолчанию. Числа проверяются до сохранения.",
            12, color: muted));
        if (configHost == null) configHost = new StackPanel();
        // Tab switches rebuild the pane; detach before reparenting (WPF logical child rule).
        if (configHost.Parent is Panel previous)
            previous.Children.Remove(configHost);
        else if (configHost.Parent is ContentControl cc)
            cc.Content = null;
        configHost.Margin = new Thickness(0, 14, 0, 0);
        box.Children.Add(configHost);
        return box;
    }

    private UIElement BuildAboutPane()
    {
        var box = new StackPanel();
        box.Children.Add(Text("О ПРОГРАММЕ", 10, FontWeights.Bold, muted));
        var title = Text(ProductIdentity.AboutHeading, 18, FontWeights.SemiBold);
        title.Margin = new Thickness(0, 8, 0, 4);
        box.Children.Add(title);
        box.Children.Add(Text(
            "Исполняемый файл: " + ProductIdentity.ExeFileName + " · ядро: " + ProductIdentity.CoreFileName,
            12, color: muted));

        aboutFacts ??= new TextBlock();
        aboutFacts.FontSize = 13;
        aboutFacts.TextWrapping = TextWrapping.Wrap;
        aboutFacts.LineHeight = 22;
        aboutFacts.Margin = new Thickness(0, 12, 0, 0);
        RefreshAboutFacts();
        box.Children.Add(aboutFacts);

        var note = Text(
            "Подписанный установщик и канал обновлений — Gate 3.2–3.3. Эта native-сборка не проверяет обновления, не подписывает бинарь и не является Released.",
            12, color: muted);
        note.Margin = new Thickness(0, 16, 0, 0);
        box.Children.Add(note);

        var change = Btn("Сменить папку данных…", async () =>
        {
            var dialog = new OpenFolderDialog { Title = "Папка с cmnet.db и bundle.json" };
            if (dialog.ShowDialog(this) != true) return;
            DesktopSettings.SaveChoice(dialog.FolderName);
            toast.Text = "Папка данных сохранена. Перезапуск с новым путём…";
            var executable = Environment.ProcessPath
                ?? throw new InvalidOperationException("Путь приложения неизвестен");
            var psi = new ProcessStartInfo(executable) { UseShellExecute = true };
            psi.ArgumentList.Add("--data-dir");
            psi.ArgumentList.Add(dialog.FolderName);
            Process.Start(psi);
            Close();
            await Task.CompletedTask;
        }, true);
        change.Margin = new Thickness(0, 16, 0, 0);
        change.HorizontalAlignment = HorizontalAlignment.Left;
        System.Windows.Automation.AutomationProperties.SetName(change, "Сменить папку данных");
        box.Children.Add(change);
        return Surface(box, new Thickness(0));
    }

    private void RefreshAboutFacts()
    {
        if (aboutFacts == null) return;
        var draftReady = state?.At("meta", "draftReady").B() == true;
        var patch = state?.At("meta", "patchTag").S("—") ?? "—";
        var bundle = state?.At("meta", "bundleVersion").S() ?? "";
        if (bundle.Length > 12) bundle = bundle[..12];
        var model = draftReady
            ? $"патч {patch}, {bundle}"
            : "не обучена (см. раздел «Данные»)";
        aboutFacts.Text =
            $"Версия приложения\t{DesktopSettings.AppVersionLabel()}\n" +
            $"Версия ядра (advisor)\t{DesktopSettings.CoreVersionLabel()}\n" +
            $"Папка данных\t{core.DataDirectory}\n" +
            $"Модель\t{model}\n" +
            $"Локальный режим\tда (CMNET_LOCAL_ONLY, Discord выключен)\n" +
            $"Статус релиза\tне Released (Gate 3.1 identity / preview)\n" +
            $"Перезапуск\tчасть полей config.yaml применяется только после перезапуска ядра";
    }

    private UIElement BuildDeferredPane()
    {
        var box = new StackPanel();
        box.Children.Add(Text("ЧЕСТНЫЙ CUT-LIST", 10, FontWeights.Bold, gold));
        var title = Text("Эти возможности не входят в native Gate 2.2", 16, FontWeights.SemiBold);
        title.Margin = new Thickness(0, 8, 0, 10);
        box.Children.Add(title);
        box.Children.Add(Text(
            "Вкладки ниже сознательно не реализованы как рабочие экраны — вместо мёртвых кнопок здесь явный статус. Product cut-list подтверждён для #43.",
            12, color: muted));

        void CutRow(string name, string reason)
        {
            var row = new StackPanel { Margin = new Thickness(0, 14, 0, 0) };
            row.Children.Add(Text(name, 14, FontWeights.SemiBold, foreground));
            var why = Text(reason, 12, color: muted);
            why.Margin = new Thickness(0, 4, 0, 0);
            row.Children.Add(why);
            box.Children.Add(row);
        }

        CutRow("Discord-бот",
            "Ядро стартует с --discord=false. Публичные/сетевые бот-интеграции не входят в local-first native.");
        CutRow("Таймеры и озвучка (Alerts / TTS)",
            "Отдельная вкладка Electron не перенесена. Ключи alert-* в config.yaml скрыты здесь же, чтобы не дублировать мёртвый редактор.");
        CutRow("Советы по предметам (Rules)",
            "Редактор item-rules остаётся в Electron; native фокус — драфт / разведка / данные / ядро.");
        CutRow("Канал автообновлений",
            "Подписанный установщик и update/rollback — Gate 3.2–3.3. В «О программе» нет кнопки «Проверить обновления».");

        return Surface(box, new Thickness(0));
    }

    private async Task LoadSettingsAsync()
    {
        if (page != "Настройки" || configHost == null) return;
        if (isVisualPreview)
        {
            ApplyPreviewConfig(previewConfig ?? SampleSettingsConfig(true));
            return;
        }
        if (core.BaseUri == null)
        {
            settingsLoadError = "Ядро не подключено — настройки недоступны.";
            configHost.Children.Clear();
            configHost.Children.Add(Text(settingsLoadError, 14, color: (Brush)FindResource("ErrorBrush")));
            RefreshSettingsEvidence();
            return;
        }

        settingsLoading = true;
        settingsLoadError = null;
        RefreshSettingsEvidence();
        try
        {
            var cfg = await core.GetAsync("/api/config");
            if (page != "Настройки" || configHost == null) return;
            var value = cfg.At("config");
            if (value == null)
            {
                settingsLoadError = cfg.At("error").S("Конфигурация недоступна");
                configEditors.Clear();
                configHost.Children.Clear();
                configHost.Children.Add(Text(settingsLoadError, 14, color: (Brush)FindResource("ErrorBrush")));
                RefreshSettingsEvidence();
                return;
            }
            RenderConfigSections(value);
            if (settingsPathLine != null)
            {
                var path = value.At("path").S(core.DataDirectory);
                var exists = value.At("exists").B();
                settingsPathLine.Text = exists
                    ? $"Файл: {path}"
                    : $"Файл: {path} (появится при сохранении)";
            }
            HideSaveBanner();
        }
        catch (Exception ex)
        {
            settingsLoadError = ex.Message;
            if (configHost != null)
            {
                configHost.Children.Clear();
                configHost.Children.Add(Text("Настройки: " + ex.Message, 14, color: (Brush)FindResource("ErrorBrush")));
            }
            toast.Text = "Настройки: " + ex.Message;
        }
        finally
        {
            settingsLoading = false;
            RefreshSettingsEvidence();
        }
    }

    private void ApplyPreviewConfig(JsonObject value)
    {
        if (configHost == null) return;
        settingsLoading = false;
        settingsLoadError = null;
        RenderConfigSections(value);
        if (settingsPathLine != null)
            settingsPathLine.Text = "SAMPLE · " + value.At("path").S("config.yaml");
        RefreshSettingsEvidence();
        RefreshAboutFacts();
    }

    private void RenderConfigSections(JsonNode value)
    {
        if (configHost == null) return;
        configEditors.Clear();
        configHost.Children.Clear();
        var note = value.At("note").S();
        if (!string.IsNullOrWhiteSpace(note))
        {
            var n = Text(note, 12, color: muted);
            n.Margin = new Thickness(0, 0, 0, 12);
            configHost.Children.Add(n);
        }
        foreach (var section in value.At("sections").Items())
        {
            var name = section.At("name").S();
            var box = new StackPanel { Margin = new Thickness(0, 0, 0, 12) };
            Header(box, section.At("title").S(string.IsNullOrEmpty(name) ? "Общие" : name));
            foreach (var field in section.At("fields").Items())
            {
                var key = field.At("key").S();
                if (key.StartsWith("alert-", StringComparison.Ordinal)) continue;
                var type = field.At("type").S();
                var editor = new ConfigField { Section = name, Key = key, Type = type };
                var row = new Grid { Margin = new Thickness(0, 6, 0, 6) };
                row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(32) });
                row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(220) });
                row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
                row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });

                var enabled = new CheckBox
                {
                    IsChecked = field.At("set").B(),
                    VerticalAlignment = VerticalAlignment.Center,
                    ToolTip = "Записать значение в config.yaml"
                };
                System.Windows.Automation.AutomationProperties.SetName(enabled, "Записать " + key);
                editor.Enabled = enabled;
                row.Children.Add(enabled);

                var labelCol = new StackPanel { VerticalAlignment = VerticalAlignment.Center };
                labelCol.Children.Add(Text(key, 13, FontWeights.SemiBold));
                var help = field.At("help").S();
                if (!string.IsNullOrWhiteSpace(help))
                {
                    var h = Text(help, 11, color: muted);
                    h.TextWrapping = TextWrapping.Wrap;
                    labelCol.Children.Add(h);
                }
                Grid.SetColumn(labelCol, 1);
                row.Children.Add(labelCol);

                FrameworkElement control;
                var err = Text("", 11, color: (Brush)FindResource("ErrorBrush"));
                err.Visibility = Visibility.Collapsed;
                editor.ErrorLabel = err;
                if (type == "bool")
                {
                    var checkbox = new CheckBox
                    {
                        IsChecked = field.At("value").B(),
                        VerticalAlignment = VerticalAlignment.Center
                    };
                    checkbox.Checked += (_, _) => enabled.IsChecked = true;
                    checkbox.Unchecked += (_, _) => enabled.IsChecked = true;
                    editor.Control = checkbox;
                    control = checkbox;
                }
                else
                {
                    var secret = field.At("secret").B();
                    var input = new TextBox
                    {
                        Text = field.At("value").S(),
                        Padding = new Thickness(7),
                        ToolTip = help,
                        MinWidth = 130
                    };
                    if (secret) input.Text = field.At("value").S();
                    input.TextChanged += (_, _) =>
                    {
                        enabled.IsChecked = true;
                        ClearFieldError(editor);
                    };
                    editor.Control = input;
                    control = input;
                }
                Grid.SetColumn(control, 2);
                row.Children.Add(control);
                Grid.SetColumn(err, 3);
                row.Children.Add(err);
                box.Children.Add(row);
                configEditors.Add(editor);
            }
            if (box.Children.Count > 1)
                configHost.Children.Add(Surface(box, new Thickness(0, 0, 0, 8)));
        }
        if (configEditors.Count == 0 && string.IsNullOrEmpty(settingsLoadError))
        {
            configHost.Children.Add(Text(
                "Нет доступных полей конфигурации (или все ключи скрыты cut-list).",
                13, color: muted));
        }
    }

    private static void ClearFieldError(ConfigField editor)
    {
        if (editor.ErrorLabel == null) return;
        editor.ErrorLabel.Text = "";
        editor.ErrorLabel.Visibility = Visibility.Collapsed;
        if (editor.Control is TextBox tb)
            tb.BorderBrush = null;
    }

    private async Task SaveSettingsAsync()
    {
        if (isVisualPreview)
        {
            ShowSaveBanner("SAMPLE · настройки «сохранены» (fixture, без записи на диск).");
            toast.Text = "SAMPLE DATA · settings-save";
            return;
        }
        if (core.BaseUri == null)
        {
            toast.Text = "Сохранение недоступно: ядро офлайн.";
            RefreshSettingsEvidence();
            return;
        }

        var invalid = false;
        foreach (var f in configEditors)
        {
            ClearFieldError(f);
            if (f.Enabled?.IsChecked != true) continue;
            if (!f.TryValidate(out var message))
            {
                invalid = true;
                if (f.ErrorLabel != null)
                {
                    f.ErrorLabel.Text = message;
                    f.ErrorLabel.Visibility = Visibility.Visible;
                }
                if (f.Control is TextBox tb)
                    tb.BorderBrush = (Brush)FindResource("ErrorBrush");
            }
        }
        if (invalid)
        {
            toast.Text = "Исправьте подсвеченные поля перед сохранением.";
            settingsLoadError = null;
            lastSettingsEvidence = EvidenceKind.Error;
            SetEvidenceBanner(settingsEvidenceBanner, EvidenceKind.Error,
                "Проверка не пройдена",
                "Числа и длительности должны быть корректны. Ничего не записано.",
                forceShow: true);
            return;
        }

        var sections = configEditors.GroupBy(x => x.Section).Select(g => new
        {
            name = g.Key,
            fields = g.Select(f => new
            {
                key = f.Key,
                set = f.Enabled?.IsChecked == true,
                value = f.Value()
            }).ToArray()
        }).ToArray();
        await RunAction("/api/config/save", new { sections });
        await LoadSettingsAsync();
        ShowSaveBanner("Сохранено в config.yaml. Часть параметров применяется только после перезапуска ядра.");
        toast.Text = "Настройки сохранены. Для части изменений требуется перезапуск.";
    }

    private void ShowSaveBanner(string message)
    {
        if (settingsSaveBanner == null) return;
        settingsSaveBanner.Text = "✓  " + message;
        settingsSaveBanner.Visibility = Visibility.Visible;
    }

    private void HideSaveBanner()
    {
        if (settingsSaveBanner == null) return;
        settingsSaveBanner.Visibility = Visibility.Collapsed;
        settingsSaveBanner.Text = "";
    }

    private void RefreshSettingsEvidence()
    {
        if (settingsEvidenceBanner == null || page != "Настройки") return;
        lastSettingsEvidence = ResolveSettingsEvidence(out var title, out var detail);
        SetEvidenceBanner(settingsEvidenceBanner, lastSettingsEvidence, title, detail,
            forceShow: lastSettingsEvidence != EvidenceKind.Ready);
        RefreshAboutFacts();
    }

    private EvidenceKind ResolveSettingsEvidence(out string title, out string detail)
    {
        title = "";
        detail = "";
        if (!coreOnline)
        {
            title = "Ядро недоступно";
            detail = "Редактирование config.yaml невозможно, пока Go-ядро офлайн. Показанное — не подтверждённое сохранение.";
            return EvidenceKind.Offline;
        }
        if (settingsLoading)
        {
            title = "Загрузка настроек";
            detail = "Читаю схему и значения через /api/config.";
            return EvidenceKind.Loading;
        }
        if (!string.IsNullOrWhiteSpace(settingsLoadError))
        {
            title = "Настройки недоступны";
            detail = settingsLoadError;
            return EvidenceKind.Error;
        }
        if (settingsTab == "app" && configEditors.Count == 0)
        {
            title = "Нет полей для правки";
            detail = "Схема пуста или все ключи скрыты cut-list. Это пустое состояние, не успешное сохранение.";
            return EvidenceKind.Empty;
        }
        title = "Настройки готовы";
        if (settingsTab == "gsi")
        {
            title = lastGsiEvidence switch
            {
                EvidenceKind.Empty => "GSI не готов",
                EvidenceKind.Partial => "GSI частично готов",
                EvidenceKind.Loading => "Ожидание события GSI",
                EvidenceKind.Offline => "GSI недоступен",
                EvidenceKind.Error => "Ошибка GSI",
                _ => "GSI"
            };
            detail = gsiStatusDetail?.Text ?? "Статус конфига и слушателя.";
            return lastGsiEvidence == EvidenceKind.Ready ? EvidenceKind.Ready : lastGsiEvidence;
        }
        detail = settingsTab switch
        {
            "about" => "Версия, путь данных и статус модели. Автообновление — Gate 3.",
            "deferred" => "Cut-list без мёртвых контролов: Discord / Alerts / Rules / updates.",
            _ => $"Редактируемых полей: {configEditors.Count}. Сохранение с проверкой типов."
        };
        return EvidenceKind.Ready;
    }

    internal static JsonObject SampleSettingsConfig(bool withFields)
    {
        var sections = new JsonArray();
        if (withFields)
        {
            sections.Add(new JsonObject
            {
                ["name"] = "",
                ["title"] = "Общие",
                ["fields"] = new JsonArray(
                    new JsonObject
                    {
                        ["key"] = "window-days",
                        ["type"] = "int",
                        ["set"] = true,
                        ["value"] = 31,
                        ["help"] = "SAMPLE · окно свежести матчей в днях"
                    },
                    new JsonObject
                    {
                        ["key"] = "token",
                        ["type"] = "string",
                        ["set"] = false,
                        ["value"] = "",
                        ["secret"] = true,
                        ["help"] = "SAMPLE · необязательный токен источника"
                    },
                    new JsonObject
                    {
                        ["key"] = "gsi",
                        ["type"] = "bool",
                        ["set"] = true,
                        ["value"] = false,
                        ["help"] = "SAMPLE · приём Game State Integration"
                    })
            });
        }
        return new JsonObject
        {
            ["path"] = "SAMPLE/config.yaml",
            ["exists"] = withFields,
            ["note"] = "SAMPLE DATA · изменения не пишутся на диск в --capture-ui.",
            ["sections"] = sections
        };
    }

    private sealed class ConfigField
    {
        public string Section { get; init; } = "";
        public string Key { get; init; } = "";
        public string Type { get; init; } = "";
        public CheckBox? Enabled { get; set; }
        public FrameworkElement? Control { get; set; }
        public TextBlock? ErrorLabel { get; set; }

        public bool TryValidate(out string message)
        {
            message = "";
            if (Control is CheckBox) return true;
            var input = (Control as TextBox)?.Text ?? "";
            if (Type == "int")
            {
                if (!int.TryParse(input.Trim(), NumberStyles.Integer, CultureInfo.InvariantCulture, out _))
                {
                    message = "Нужно целое число";
                    return false;
                }
                return true;
            }
            if (Type == "float")
            {
                if (!double.TryParse(input.Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out _))
                {
                    message = "Нужно число (точка)";
                    return false;
                }
                return true;
            }
            if (Type == "duration")
            {
                // Accept Go-style durations or plain seconds.
                var t = input.Trim();
                if (int.TryParse(t, NumberStyles.Integer, CultureInfo.InvariantCulture, out _))
                    return true;
                if (t.Length >= 2 && char.IsDigit(t[0]))
                    return true;
                message = "Длительность: 30s / 5m / число секунд";
                return false;
            }
            return true;
        }

        public object Value()
        {
            if (Control is CheckBox c) return c.IsChecked == true;
            var input = (Control as TextBox)?.Text ?? "";
            if (Type == "int")
                return int.TryParse(input.Trim(), NumberStyles.Integer, CultureInfo.InvariantCulture, out var n) ? n : 0;
            if (Type == "float")
                return double.TryParse(input.Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out var d) ? d : 0d;
            return input;
        }
    }
}
