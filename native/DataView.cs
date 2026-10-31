using System;
using System.Collections.Generic;
using System.Globalization;
using System.Linq;
using System.Text.Json.Nodes;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Threading;

namespace Cmnet.Native;

/// <summary>
/// Gate 2.1 Maintain / Data surface. Human corpus + model status, sync/train
/// progress, cancel/retry via existing ops APIs, readable log strip. No raw
/// JSON admin dump. Evidence states share StateKit with Scout/Draft.
/// </summary>
public sealed partial class NativeWindow
{
    private Border? dataEvidenceBanner;
    private TextBlock? modelHeadline;
    private TextBlock? modelDetail;
    private TextBlock? modelFacts;
    private TextBlock? autoPhaseLine;
    private Button? syncDataButton;
    private Button? trainDataButton;
    private Button? cancelDataButton;
    private StackPanel? dataJobsHost;
    private ListBox? dataLogList;
    private TextBlock? dataLogTitle;
    private ListBox? dataTopPicks;
    private TextBlock? corpusPeriod;
    private long dataCorpusMatches = -1;
    private long dataLogJobId;
    private readonly List<string> dataLogLines = new();
    private DispatcherTimer? dataLogTimer;
    private JsonObject? previewCorpus;
    private EvidenceKind lastDataEvidence = EvidenceKind.Ready;

    private static string JobKindLabel(string kind) => kind switch
    {
        "sync" => "Сбор матчей",
        "train" => "Обучение модели",
        "scout" => "Разведка соперников",
        "scout-allies" => "Разведка союзников",
        "recommend" or "draft" => "Пересчёт драфта",
        _ => string.IsNullOrWhiteSpace(kind) ? "Задача" : kind
    };

    private static string JobStateLabel(string state) => state switch
    {
        "pending" => "в очереди",
        "running" => "выполняется",
        "done" => "готово",
        "failed" => "ошибка",
        "cancelled" => "отменено",
        _ => state
    };

    private static string FormatDurationMs(ulong ms)
    {
        if (ms < 1000) return ms + " мс";
        var sec = (long)(ms / 1000);
        if (sec < 60) return sec + " с";
        var min = sec / 60;
        sec %= 60;
        return min + " м " + sec.ToString("00", CultureInfo.InvariantCulture) + " с";
    }

    private UIElement BuildDataView()
    {
        corpusMatches = new TextBlock();
        corpusDrafts = new TextBlock();
        corpusCoverage = new TextBlock();
        corpusHeroes = new TextBlock();
        corpusPeriod = new TextBlock();
        modelHeadline = new TextBlock();
        modelDetail = new TextBlock();
        modelFacts = new TextBlock();
        autoPhaseLine = new TextBlock();
        dataJobsHost = new StackPanel();
        dataLogLines.Clear();
        dataLogJobId = 0;
        StopDataLogTimer();

        var host = new StackPanel { Name = "DataWorkspace" };

        var heading = new DockPanel { LastChildFill = true, Margin = new Thickness(0, 0, 0, 10) };
        var actions = new StackPanel
        {
            Orientation = Orientation.Horizontal,
            VerticalAlignment = VerticalAlignment.Center
        };
        syncDataButton = Btn("↻  Собрать матчи", async () =>
        {
            await RunAction("/api/ops/sync");
            await LoadDataAsync();
        });
        syncDataButton.Margin = new Thickness(0, 0, 8, 0);
        syncDataButton.ToolTip = "Пополнить локальный кэш матчей. Модель не меняется.";
        System.Windows.Automation.AutomationProperties.SetName(syncDataButton, "Собрать матчи");
        actions.Children.Add(syncDataButton);

        trainDataButton = Btn("◇  Обучить модель", async () =>
        {
            await RunAction("/api/ops/train");
            await LoadDataAsync();
        });
        trainDataButton.Margin = new Thickness(0, 0, 8, 0);
        trainDataButton.ToolTip = "Пересобрать модель из кэша. Драфт подхватит её без перезапуска.";
        System.Windows.Automation.AutomationProperties.SetName(trainDataButton, "Обучить модель");
        actions.Children.Add(trainDataButton);

        cancelDataButton = Btn("✕  Остановить", async () =>
        {
            var active = state?.At("ops", "active").Items().FirstOrDefault();
            var id = active?.At("id").L() ?? 0;
            if (id > 0) await RunAction("/api/ops/cancel", new { id });
            else await RunAction("/api/ops/cancel", new { });
            await LoadDataAsync();
        }, true);
        cancelDataButton.Margin = new Thickness(0, 0, 8, 0);
        cancelDataButton.IsEnabled = false;
        cancelDataButton.Opacity = 0.45;
        cancelDataButton.ToolTip = "Отменить активную задачу sync/train через /api/ops/cancel";
        System.Windows.Automation.AutomationProperties.SetName(cancelDataButton, "Остановить задачу данных");
        actions.Children.Add(cancelDataButton);

        var refresh = Btn("Обновить", LoadDataAsync, true);
        refresh.ToolTip = "Обновить метрики корпуса";
        System.Windows.Automation.AutomationProperties.SetName(refresh, "Обновить метрики корпуса");
        actions.Children.Add(refresh);
        DockPanel.SetDock(actions, Dock.Right);
        heading.Children.Add(actions);

        var titleCol = new StackPanel();
        titleCol.Children.Add(Text("Данные и обучение", 18, FontWeights.Bold));
        titleCol.Children.Add(Text("Статус корпуса и модели · задачи с прогрессом · без сырого JSON", 11, color: muted));
        heading.Children.Add(titleCol);
        host.Children.Add(heading);

        dataEvidenceBanner = BuildEvidenceBanner("DataEvidenceBanner");
        host.Children.Add(dataEvidenceBanner);

        var split = new Grid { Margin = new Thickness(0, 0, 0, 12) };
        split.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
        split.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });

        var modelBox = new StackPanel();
        modelBox.Children.Add(Text("МОДЕЛЬ", 10, FontWeights.Bold, muted));
        modelHeadline.FontSize = 16;
        modelHeadline.FontWeight = FontWeights.SemiBold;
        modelHeadline.Margin = new Thickness(0, 8, 0, 4);
        modelHeadline.Text = "—";
        modelBox.Children.Add(modelHeadline);
        modelDetail.FontSize = 12;
        modelDetail.Foreground = muted;
        modelDetail.TextWrapping = TextWrapping.Wrap;
        modelDetail.Text = "Ожидание снимка ядра…";
        modelBox.Children.Add(modelDetail);
        modelFacts.FontSize = 12;
        modelFacts.Margin = new Thickness(0, 10, 0, 0);
        modelFacts.Foreground = foreground;
        modelFacts.TextWrapping = TextWrapping.Wrap;
        modelFacts.Text = "";
        modelBox.Children.Add(modelFacts);
        autoPhaseLine.FontSize = 11;
        autoPhaseLine.Foreground = muted;
        autoPhaseLine.Margin = new Thickness(0, 8, 0, 0);
        autoPhaseLine.TextWrapping = TextWrapping.Wrap;
        autoPhaseLine.Visibility = Visibility.Collapsed;
        modelBox.Children.Add(autoPhaseLine);
        var modelCard = Surface(modelBox, new Thickness(0, 0, 8, 0));
        Grid.SetColumn(modelCard, 0);
        split.Children.Add(modelCard);

        var corpusBox = new StackPanel();
        corpusBox.Children.Add(Text("КОРПУС", 10, FontWeights.Bold, muted));
        var tiles = new WrapPanel { Margin = new Thickness(0, 10, 0, 0) };
        tiles.Children.Add(MetricTile("Матчей", corpusMatches, accent));
        tiles.Children.Add(MetricTile("Драфтов CM", corpusDrafts, gold));
        tiles.Children.Add(MetricTile("С позициями", corpusCoverage, good));
        tiles.Children.Add(MetricTile("Героев", corpusHeroes, muted));
        corpusBox.Children.Add(tiles);
        corpusPeriod.FontSize = 11;
        corpusPeriod.Foreground = muted;
        corpusPeriod.Margin = new Thickness(0, 8, 0, 0);
        corpusPeriod.Text = "Период: —";
        corpusBox.Children.Add(corpusPeriod);
        var note = Text("Сбор пополняет кэш. Обучение пересобирает модель из кэша — цифры ниже не выдумываются.", 11, color: muted);
        note.Margin = new Thickness(0, 8, 0, 0);
        corpusBox.Children.Add(note);
        var corpusCard = Surface(corpusBox, new Thickness(8, 0, 0, 0));
        Grid.SetColumn(corpusCard, 1);
        split.Children.Add(corpusCard);
        host.Children.Add(split);

        Header(host, "Задачи");
        var jobsHint = Text("Активные и недавние sync/train. Остановка и повтор — через существующие /api/ops/*.", 12, color: muted);
        jobsHint.Margin = new Thickness(0, -8, 0, 10);
        host.Children.Add(jobsHint);
        dataActivity = Text("Активных заданий нет", 13, color: muted);
        dataActivity.Margin = new Thickness(0, 0, 0, 8);
        host.Children.Add(dataActivity);
        host.Children.Add(dataJobsHost);

        Header(host, "Журнал задачи");
        dataLogTitle = Text("Выбери «Журнал» у задачи — строки с /api/jobs/log.", 12, color: muted);
        dataLogTitle.Margin = new Thickness(0, -8, 0, 8);
        host.Children.Add(dataLogTitle);
        dataLogList = new ListBox
        {
            Name = "DataJobLog",
            MaxHeight = 180,
            MinHeight = 72,
            FontFamily = new FontFamily("Cascadia Mono, Consolas, Courier New"),
            FontSize = 11,
            Background = ThemeBrush("RaisedBrush"),
            BorderBrush = border,
            BorderThickness = new Thickness(1),
            Padding = new Thickness(4)
        };
        VirtualizingStackPanel.SetIsVirtualizing(dataLogList, true);
        host.Children.Add(dataLogList);

        Header(host, "Популярные герои в корпусе");
        var picksHint = Text("Топ пиков по сохранённым матчам — не рейтинг рекомендаций драфта.", 12, color: muted);
        picksHint.Margin = new Thickness(0, -8, 0, 10);
        host.Children.Add(picksHint);
        dataTopPicks = new ListBox { Name = "CorpusPicks", MaxHeight = 280, MinHeight = 80 };
        VirtualizingStackPanel.SetIsVirtualizing(dataTopPicks, true);
        host.Children.Add(dataTopPicks);

        UpdateDataView();
        return Scroll(host);
    }

    private void UpdateDataView()
    {
        if (page != "Данные" || dataJobsHost == null) return;

        var meta = state?.At("meta");
        var draftReady = meta?.At("draftReady").B() == true;
        if (modelHeadline != null && modelDetail != null && modelFacts != null)
        {
            if (draftReady)
            {
                modelHeadline.Text = "Модель готова к драфту";
                modelHeadline.Foreground = good;
                modelDetail.Text = "Драфт использует локальный бандл. Новый train не рвёт текущий матч — подхват на следующем драфте.";
                var patch = meta?.At("patchTag").S("—") ?? "—";
                var ver = meta?.At("bundleVersion").S("") ?? "";
                if (ver.Length > 12) ver = ver[..12];
                var matrices = meta?.At("hasMatrices").B() == true ? "парные матрицы есть" : "парных матриц пока нет";
                modelFacts.Text = $"Патч {patch}  ·  версия {ver}  ·  {matrices}";
            }
            else
            {
                modelHeadline.Text = "Модель ещё не обучена";
                modelHeadline.Foreground = gold;
                var err = meta?.At("draftError").S("");
                modelDetail.Text = string.IsNullOrWhiteSpace(err)
                    ? "Собери матчи и запусти обучение. Пустой статус — не «всё в порядке»."
                    : err;
                modelFacts.Text = "Бандл отсутствует или не прошёл проверку версии.";
            }
        }

        if (autoPhaseLine != null)
        {
            var phase = state?.At("auto", "phase").S("off") ?? "off";
            if (phase is "" or "off")
            {
                autoPhaseLine.Visibility = Visibility.Collapsed;
            }
            else
            {
                var label = phase switch
                {
                    "idle" => "Фоновый сбор ждёт",
                    "sync" => "Фоновый сбор идёт",
                    "train" => "Фоновое переобучение",
                    "paused" => "Фоновый сбор на паузе",
                    _ => phase
                };
                var tail = state?.At("auto", "line").S() ?? state?.At("auto", "last").S() ?? "";
                var next = state?.At("auto", "nextInSeconds").L() ?? 0;
                autoPhaseLine.Text = label
                    + (string.IsNullOrWhiteSpace(tail) ? "" : ": " + tail)
                    + (phase == "idle" && next > 0 ? $" · следующий проход через {next} с" : "");
                var autoErr = state?.At("auto", "error").S();
                if (!string.IsNullOrWhiteSpace(autoErr))
                    autoPhaseLine.Text += " · " + autoErr;
                autoPhaseLine.Foreground = string.IsNullOrWhiteSpace(autoErr) ? muted : (Brush)FindResource("ErrorBrush");
                autoPhaseLine.Visibility = Visibility.Visible;
            }
        }

        var active = state?.At("ops", "active").Items().ToList() ?? [];
        var dataJobs = active.Where(j => j.At("kind").S() is "sync" or "train").ToList();
        var syncBusy = dataJobs.Any(j => j.At("kind").S() == "sync");
        var trainBusy = dataJobs.Any(j => j.At("kind").S() == "train");
        var canSync = meta == null || meta.At("canSync").B(true);
        var canTrain = meta == null || meta.At("canTrain").B(true);

        if (syncDataButton != null)
        {
            syncDataButton.Content = syncBusy ? "Сбор матчей идёт…" : "↻  Собрать матчи";
            syncDataButton.IsEnabled = canSync && !syncBusy && coreOnline;
            syncDataButton.Opacity = syncDataButton.IsEnabled ? 1 : 0.45;
        }
        if (trainDataButton != null)
        {
            trainDataButton.Content = trainBusy ? "Обучение идёт…" : "◇  Обучить модель";
            trainDataButton.IsEnabled = canTrain && !trainBusy && coreOnline;
            trainDataButton.Opacity = trainDataButton.IsEnabled ? 1 : 0.45;
        }
        if (cancelDataButton != null)
        {
            var any = dataJobs.Count > 0;
            cancelDataButton.IsEnabled = any && coreOnline;
            cancelDataButton.Opacity = cancelDataButton.IsEnabled ? 1 : 0.45;
        }

        if (dataActivity != null)
        {
            if (!coreOnline)
            {
                dataActivity.Text = "Ядро офлайн — задачи не обновляются.";
                dataActivity.Foreground = gold;
            }
            else if (dataJobs.Count == 0)
            {
                dataActivity.Text = "Активных sync/train нет.";
                dataActivity.Foreground = muted;
            }
            else
            {
                dataActivity.Text = string.Join("  ·  ", dataJobs.Select(j =>
                    JobKindLabel(j.At("kind").S()) + ": " + j.At("lastLine").S(j.At("phase").S("ожидание"))));
                dataActivity.Foreground = accent;
            }
        }

        dataJobsHost.Children.Clear();
        if (dataJobs.Count == 0 && (state?.At("ops", "recent").Items().Count() ?? 0) == 0)
        {
            dataJobsHost.Children.Add(Text("Очередь пуста. Запусти сбор или обучение — прогресс появится здесь.", 12, color: muted));
        }
        else
        {
            foreach (var job in dataJobs)
            {
                if (job != null) dataJobsHost.Children.Add(BuildJobRow(job, live: true));
            }
            var recent = state?.At("ops", "recent").Items()
                .Where(j => j != null && j.At("kind").S() is "sync" or "train")
                .Take(6)
                .ToList() ?? [];
            if (recent.Count > 0)
            {
                var recentLabel = Text("Недавние", 13, FontWeights.SemiBold);
                recentLabel.Margin = new Thickness(0, 12, 0, 6);
                dataJobsHost.Children.Add(recentLabel);
                foreach (var job in recent)
                {
                    if (job != null) dataJobsHost.Children.Add(BuildJobRow(job, live: false));
                }
            }
        }

        lastDataEvidence = ResolveDataEvidence(out var title, out var detail);
        SetEvidenceBanner(dataEvidenceBanner, lastDataEvidence, title, detail);
    }

    private Border BuildJobRow(JsonNode job, bool live)
    {
        var kind = job.At("kind").S();
        var stateName = job.At("state").S(live ? "running" : "");
        var column = new StackPanel();
        var top = new DockPanel { LastChildFill = true };
        var actions = new StackPanel { Orientation = Orientation.Horizontal };
        var logBtn = Btn("Журнал", async () => await OpenDataJobLogAsync(job.At("id").L()), true);
        logBtn.Margin = new Thickness(0, 0, 6, 0);
        logBtn.Padding = new Thickness(8, 4, 8, 4);
        logBtn.MinHeight = 28;
        actions.Children.Add(logBtn);
        if (live)
        {
            var stop = Btn("Стоп", async () =>
            {
                var id = job.At("id").L();
                if (id > 0) await RunAction("/api/ops/cancel", new { id });
                else await RunAction("/api/ops/cancel", new { });
            }, true);
            stop.Padding = new Thickness(8, 4, 8, 4);
            stop.MinHeight = 28;
            stop.Foreground = (Brush)FindResource("ErrorBrush");
            actions.Children.Add(stop);
        }
        else if (stateName == "failed")
        {
            var retryPath = kind == "train" ? "/api/ops/train" : "/api/ops/sync";
            var retry = Btn("Повторить", async () =>
            {
                await RunAction(retryPath);
                await LoadDataAsync();
            }, true);
            retry.Padding = new Thickness(8, 4, 8, 4);
            retry.MinHeight = 28;
            actions.Children.Add(retry);
        }
        DockPanel.SetDock(actions, Dock.Right);
        top.Children.Add(actions);
        var title = Text(JobKindLabel(kind), 13, FontWeights.SemiBold);
        top.Children.Add(title);
        column.Children.Add(top);

        var meta = new WrapPanel { Margin = new Thickness(0, 4, 0, 0) };
        meta.Children.Add(Text(JobStateLabel(stateName), 11, FontWeights.SemiBold,
            stateName == "failed" ? (Brush)FindResource("ErrorBrush")
            : stateName is "done" ? good
            : live ? accent : muted));
        var running = job.At("runningMs").L();
        if (running > 0)
            meta.Children.Add(Text("  ·  " + FormatDurationMs((ulong)running), 11, color: muted));
        column.Children.Add(meta);

        if (live)
        {
            var phase = job.At("phase").S();
            if (!string.IsNullOrWhiteSpace(phase))
                column.Children.Add(Text(phase, 11, color: muted));
            var line = job.At("lastLine").S();
            if (!string.IsNullOrWhiteSpace(line))
            {
                var lineBlock = Text(line, 12);
                lineBlock.Margin = new Thickness(0, 2, 0, 0);
                column.Children.Add(lineBlock);
            }
            var waiting = job.At("waitingOn").S();
            if (!string.IsNullOrWhiteSpace(waiting))
                column.Children.Add(Text("ждём ответа: " + waiting, 11, color: gold));
        }
        else
        {
            var err = job.At("error").S();
            var result = job.At("result").S();
            if (!string.IsNullOrWhiteSpace(err))
                column.Children.Add(Text(err, 12, color: (Brush)FindResource("ErrorBrush")));
            else if (!string.IsNullOrWhiteSpace(result))
                column.Children.Add(Text(result, 12, color: muted));
        }

        return new Border
        {
            Child = column,
            Background = ThemeBrush("RaisedBrush"),
            BorderBrush = live ? ThemeBrush("PrimaryBrush") : border,
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(8),
            Padding = new Thickness(12, 10, 12, 10),
            Margin = new Thickness(0, 0, 0, 8)
        };
    }

    private async Task OpenDataJobLogAsync(long jobId)
    {
        if (jobId <= 0)
        {
            if (isVisualPreview)
            {
                dataLogJobId = jobId;
                ApplyPreviewJobLog(jobId);
                return;
            }
            toast.Text = "У задачи нет id для журнала.";
            return;
        }
        dataLogJobId = jobId;
        dataLogLines.Clear();
        if (dataLogList != null) dataLogList.ItemsSource = null;
        if (dataLogTitle != null) dataLogTitle.Text = "Журнал задачи #" + jobId;
        await PullDataJobLogAsync();
    }

    private void ApplyPreviewJobLog(long jobId)
    {
        dataLogLines.Clear();
        dataLogLines.AddRange(new[]
        {
            "SAMPLE · старт задачи",
            "SAMPLE · фаза sync/cm · запрос к источнику",
            "SAMPLE · получено 40 матчей (fixture)",
            "SAMPLE · без обращения к сети / БД пользователя"
        });
        if (dataLogTitle != null)
            dataLogTitle.Text = "SAMPLE · журнал задачи" + (jobId > 0 ? " #" + jobId : "");
        if (dataLogList != null)
            dataLogList.ItemsSource = dataLogLines.ToList();
    }

    private async Task PullDataJobLogAsync()
    {
        if (dataLogJobId <= 0 || page != "Данные") return;
        if (core.BaseUri == null)
        {
            if (isVisualPreview) ApplyPreviewJobLog(dataLogJobId);
            return;
        }
        try
        {
            var after = 0L;
            // Request incremental page; UI keeps a capped text tail.
            var path = $"/api/jobs/log?id={dataLogJobId}&after={after}&limit=200";
            if (dataLogLines.Count > 0)
            {
                // Prefer seq from last successful payload stored in Tag when available.
                after = dataLogList?.Tag as long? ?? 0;
                path = $"/api/jobs/log?id={dataLogJobId}&after={after}&limit=200";
            }
            var data = await core.GetAsync(path);
            var lines = data.At("lines").Items().ToList();
            foreach (var line in lines)
            {
                var seq = line.At("seq").L();
                if (dataLogList != null) dataLogList.Tag = seq;
                var at = line.At("at").L();
                var stamp = at > 0
                    ? DateTimeOffset.FromUnixTimeMilliseconds(at).LocalDateTime.ToString("HH:mm:ss")
                    : "--:--:--";
                var text = line.At("text").S();
                var level = line.At("level").S("info");
                dataLogLines.Add($"{stamp}  [{level}]  {text}");
            }
            if (dataLogLines.Count > 400)
                dataLogLines.RemoveRange(0, dataLogLines.Count - 400);
            if (page == "Данные" && dataLogList != null)
                dataLogList.ItemsSource = dataLogLines.ToList();

            var jobState = data.At("job", "state").S();
            var running = jobState is "running" or "pending";
            StopDataLogTimer();
            if (running && page == "Данные")
            {
                dataLogTimer = new DispatcherTimer { Interval = TimeSpan.FromMilliseconds(1500) };
                dataLogTimer.Tick += async (_, _) =>
                {
                    StopDataLogTimer();
                    await PullDataJobLogAsync();
                };
                dataLogTimer.Start();
            }
        }
        catch (Exception ex)
        {
            if (page == "Данные")
                toast.Text = "Журнал: " + ex.Message;
        }
    }

    private void StopDataLogTimer()
    {
        if (dataLogTimer == null) return;
        dataLogTimer.Stop();
        dataLogTimer = null;
    }

    private async Task LoadDataAsync()
    {
        if (page != "Данные") return;
        if (core.BaseUri == null)
        {
            if (isVisualPreview)
            {
                ApplyCorpusPayload(previewCorpus ?? SampleCorpusFixture());
                UpdateDataView();
            }
            else
            {
                dataCorpusMatches = -1;
                if (corpusMatches != null) corpusMatches.Text = "—";
                if (corpusDrafts != null) corpusDrafts.Text = "—";
                if (corpusCoverage != null) corpusCoverage.Text = "—";
                if (corpusHeroes != null) corpusHeroes.Text = "—";
                if (corpusPeriod != null) corpusPeriod.Text = "Ядро не подключено — метрики корпуса недоступны.";
                UpdateDataView();
            }
            return;
        }
        try
        {
            var result = await core.GetAsync("/api/corpus");
            if (page != "Данные") return;
            if (result.At("unavailable").B() || result.At("error") != null || result.At("corpus") == null)
            {
                dataCorpusMatches = 0;
                corpusMatches.Text = "—";
                corpusDrafts.Text = "—";
                corpusCoverage.Text = "—";
                corpusHeroes.Text = "—";
                if (corpusPeriod != null)
                {
                    corpusPeriod.Text = result.At("unavailable").B()
                        ? "Статистика кэша недоступна в этой сборке."
                        : result.At("error").S("Кэш пуст или ещё не посчитан.");
                }
                if (dataTopPicks != null) dataTopPicks.ItemsSource = null;
                toast.Text = result.At("error").S("Корпус пока недоступен");
                UpdateDataView();
                return;
            }
            ApplyCorpusPayload(result);
            UpdateDataView();
        }
        catch (Exception ex)
        {
            if (page == "Данные")
            {
                toast.Text = ex is TimeoutException
                    ? "Данные временно заняты. Повтори обновление метрик."
                    : "Корпус: " + ex.Message;
                UpdateDataView();
            }
        }
    }

    private void ApplyCorpusPayload(JsonObject result)
    {
        var corpus = result.At("corpus") ?? result;
        dataCorpusMatches = corpus.At("matches").L();
        corpusMatches.Text = dataCorpusMatches.ToString("N0");
        corpusDrafts.Text = corpus.At("cmDrafts").L().ToString("N0");
        corpusCoverage.Text = corpus.At("withPositions").L().ToString("N0");
        corpusHeroes.Text = corpus.At("heroes").L().ToString("N0");
        if (corpusPeriod != null)
        {
            var oldest = corpus.At("oldestStart").L();
            var newest = corpus.At("newestStart").L();
            if (oldest > 0 && newest > 0)
            {
                var a = DateTimeOffset.FromUnixTimeSeconds(oldest).LocalDateTime.ToString("dd.MM.yyyy");
                var b = DateTimeOffset.FromUnixTimeSeconds(newest).LocalDateTime.ToString("dd.MM.yyyy");
                corpusPeriod.Text = "Период матчей: " + a + " — " + b;
            }
            else corpusPeriod.Text = "Период матчей: не указан";
        }
        if (dataTopPicks != null)
        {
            dataTopPicks.ItemsSource = corpus.At("topPicks").Items().Take(12)
                .Select((h, i) => $"{i + 1:D2}    {h.At("name").S("Герой")}    ·    {h.At("count").L():N0} пиков")
                .ToList();
        }
    }

    private static JsonObject SampleCorpusFixture(long matches = 12840)
    {
        if (matches <= 0)
        {
            return (JsonObject)JsonNode.Parse("""
            {
              "corpus": {
                "matches": 0,
                "cmDrafts": 0,
                "withPositions": 0,
                "heroes": 0,
                "topPicks": []
              }
            }
            """)!;
        }
        var cm = Math.Max(1, matches / 6);
        var withPos = Math.Max(1, matches / 7);
        return (JsonObject)JsonNode.Parse($$"""
        {
          "corpus": {
            "matches": {{matches}},
            "cmDrafts": {{cm}},
            "withPositions": {{withPos}},
            "heroes": 124,
            "oldestStart": 1727740800,
            "newestStart": 1730332800,
            "topPicks": [
              { "name": "Invoker", "count": 842 },
              { "name": "Pudge", "count": 791 },
              { "name": "Pangolier", "count": 640 },
              { "name": "Tusk", "count": 588 },
              { "name": "Queen of Pain", "count": 512 }
            ]
          }
        }
        """)!;
    }
}
