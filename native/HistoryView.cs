using System;
using System.Collections.Generic;
using System.Globalization;
using System.Linq;
using System.Text.Json.Nodes;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;

namespace Cmnet.Native;

/// <summary>
/// Gate 2.4 After-match / decision history. Lists local drafts from
/// /api/declog/history and shows suggested-vs-applied steps with provenance.
/// Never invents confidence, win/loss, or reasons — empty/partial are explicit.
/// </summary>
public sealed partial class NativeWindow
{
    private Border? historyEvidenceBanner;
    private ListBox? historyDraftList;
    private StackPanel? historyDetailHost;
    private TextBlock? historyListHint;
    private TextBlock? historyDetailTitle;
    private string? selectedHistoryDraftId;
    private JsonArray? previewHistoryDrafts;
    private JsonObject? previewHistoryDetail;
    private EvidenceKind lastHistoryEvidence = EvidenceKind.Empty;
    private string historyLoadError = "";
    private bool historyLoading;

    private UIElement BuildHistoryView()
    {
        historyEvidenceBanner = BuildEvidenceBanner("HistoryEvidenceBanner");
        historyDraftList = new ListBox
        {
            Name = "HistoryDraftList",
            MinHeight = 120,
            Background = ThemeBrush("RaisedBrush"),
            BorderBrush = border,
            BorderThickness = new Thickness(1),
            Padding = new Thickness(4)
        };
        historyDraftList.SelectionChanged += (_, _) =>
        {
            if (historyDraftList.SelectedItem is FrameworkElement fe &&
                fe.Tag is string id && !string.IsNullOrWhiteSpace(id))
            {
                _ = SelectHistoryDraftAsync(id);
            }
        };
        historyDetailHost = new StackPanel { Name = "HistoryDetailHost" };
        historyListHint = Text("", 12, color: muted);
        historyDetailTitle = Text("Выбери драфт слева", 14, FontWeights.SemiBold);
        selectedHistoryDraftId = null;

        var host = new StackPanel { Name = "HistoryWorkspace" };

        var heading = new DockPanel { LastChildFill = true, Margin = new Thickness(0, 0, 0, 10) };
        var actions = new StackPanel
        {
            Orientation = Orientation.Horizontal,
            VerticalAlignment = VerticalAlignment.Center
        };
        var refresh = Btn("Обновить", LoadHistoryAsync, true);
        refresh.ToolTip = "Перечитать локальный журнал /api/declog/history";
        System.Windows.Automation.AutomationProperties.SetName(refresh, "Обновить историю решений");
        actions.Children.Add(refresh);
        DockPanel.SetDock(actions, Dock.Right);
        heading.Children.Add(actions);

        var titleCol = new StackPanel();
        titleCol.Children.Add(Text("История решений", 18, FontWeights.Bold));
        titleCol.Children.Add(Text(
            "Сохранённые драфты · что советовали · что взяли · без выдуманной статистики",
            11, color: muted));
        heading.Children.Add(titleCol);
        host.Children.Add(heading);
        host.Children.Add(historyEvidenceBanner);

        var split = new Grid { Margin = new Thickness(0, 0, 0, 8) };
        split.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(0.9, GridUnitType.Star) });
        split.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1.4, GridUnitType.Star) });

        var listCol = new StackPanel();
        listCol.Children.Add(Text("ДРАФТЫ", 10, FontWeights.Bold, muted));
        historyListHint.Margin = new Thickness(0, 6, 0, 8);
        historyListHint.TextWrapping = TextWrapping.Wrap;
        historyListHint.Text = "Загрузка локального журнала…";
        listCol.Children.Add(historyListHint);
        listCol.Children.Add(historyDraftList);
        var listCard = Surface(listCol, new Thickness(0, 0, 8, 0));
        Grid.SetColumn(listCard, 0);
        split.Children.Add(listCard);

        var detailCol = new StackPanel();
        detailCol.Children.Add(Text("РЕШЕНИЯ", 10, FontWeights.Bold, muted));
        historyDetailTitle.Margin = new Thickness(0, 6, 0, 10);
        historyDetailTitle.TextWrapping = TextWrapping.Wrap;
        detailCol.Children.Add(historyDetailTitle);
        var detailScroll = new ScrollViewer
        {
            Name = "HistoryDetailScroll",
            VerticalScrollBarVisibility = ScrollBarVisibility.Auto,
            HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled,
            MaxHeight = 560,
            Content = historyDetailHost
        };
        detailCol.Children.Add(detailScroll);
        var detailCard = Surface(detailCol, new Thickness(8, 0, 0, 0));
        Grid.SetColumn(detailCard, 1);
        split.Children.Add(detailCard);

        host.Children.Add(split);

        var footnote = Text(
            "Источник: decisions.jsonl рядом с кэшем. Исход матча и причины появляются только если их записали (GSI/опрос).",
            11, color: muted);
        footnote.Margin = new Thickness(0, 4, 0, 0);
        footnote.TextWrapping = TextWrapping.Wrap;
        host.Children.Add(footnote);

        return Scroll(host);
    }

    private async Task LoadHistoryAsync()
    {
        if (historyDraftList == null || historyDetailHost == null) return;

        if (isVisualPreview)
        {
            ApplyHistoryPreviewPayload();
            return;
        }

        historyLoading = true;
        historyLoadError = "";
        RefreshEvidenceBanners();
        if (historyListHint != null)
            historyListHint.Text = "Читаю /api/declog/history…";

        try
        {
            var data = await core.GetAsync("/api/declog/history?last=50");
            ApplyHistoryListPayload(data);
            if (!string.IsNullOrWhiteSpace(selectedHistoryDraftId))
                await SelectHistoryDraftAsync(selectedHistoryDraftId);
            else if (historyDraftList.Items.Count > 0 &&
                     historyDraftList.Items[0] is FrameworkElement first &&
                     first.Tag is string firstId)
            {
                historyDraftList.SelectedIndex = 0;
                await SelectHistoryDraftAsync(firstId);
            }
            else
            {
                RenderHistoryDetailEmpty("Нет выбранного драфта.");
            }
        }
        catch (Exception ex)
        {
            historyLoadError = ex.Message;
            historyDraftList.Items.Clear();
            if (historyListHint != null)
                historyListHint.Text = "Не удалось загрузить историю.";
            RenderHistoryDetailEmpty("Ошибка загрузки журнала.");
            RefreshEvidenceBanners(forceShowReady: true);
            toast.Text = "История: " + ShortErr(ex.Message);
        }
        finally
        {
            historyLoading = false;
            RefreshEvidenceBanners();
        }
    }

    private async Task SelectHistoryDraftAsync(string id)
    {
        selectedHistoryDraftId = id;
        if (historyDetailHost == null || historyDetailTitle == null) return;

        if (isVisualPreview)
        {
            ApplyHistoryPreviewDetail(id);
            return;
        }

        try
        {
            historyDetailTitle.Text = "Загрузка драфта…";
            historyDetailHost.Children.Clear();
            var data = await core.GetAsync("/api/declog/history?id=" + Uri.EscapeDataString(id));
            ApplyHistoryDetailPayload(data);
        }
        catch (Exception ex)
        {
            historyLoadError = ex.Message;
            RenderHistoryDetailEmpty("Не удалось открыть драфт: " + ShortErr(ex.Message));
            RefreshEvidenceBanners(forceShowReady: true);
        }
    }

    private void ApplyHistoryListPayload(JsonNode? data)
    {
        if (historyDraftList == null) return;
        historyDraftList.Items.Clear();
        var drafts = data?.At("drafts") as JsonArray ?? new JsonArray();
        var evidence = data.At("evidence").S("empty");
        var note = data.At("note").S("");
        var count = data.At("count").I();
        if (historyListHint != null)
        {
            historyListHint.Text = count == 0
                ? (string.IsNullOrWhiteSpace(note) ? "Драфтов в журнале нет." : note)
                : $"Показано {count} · источник локальный" +
                  (data.At("logging").B() ? "" : " · запись новых решений выключена");
        }

        foreach (var d in drafts)
        {
            if (d is not JsonObject) continue;
            var id = d.At("id").S();
            if (string.IsNullOrWhiteSpace(id)) continue;
            var when = FormatHistoryTs(d.At("ts").L());
            var outcome = FormatHistoryOutcome(d.At("outcome"));
            var gaps = d.At("provenanceGap")?.Items().Select(x => x.S()).Where(x => x.Length > 0).ToArray() ?? [];
            var gapMark = gaps.Length > 0 ? " · частично" : "";
            var line = $"{when}  ·  {d.At("decisionCount").I()} реш.  ·  {outcome}{gapMark}";
            var sub = new StackPanel { Margin = new Thickness(4, 6, 4, 6) };
            var title = Text(line, 12, FontWeights.SemiBold);
            title.TextWrapping = TextWrapping.Wrap;
            sub.Children.Add(title);
            var meta = new List<string>();
            var src = d.At("source").S();
            if (!string.IsNullOrWhiteSpace(src)) meta.Add(src);
            var patch = d.At("patchTag").S();
            if (!string.IsNullOrWhiteSpace(patch)) meta.Add("патч " + patch);
            var bundle = d.At("bundleVersion").S();
            if (!string.IsNullOrWhiteSpace(bundle)) meta.Add("бандл " + Truncate(bundle, 18));
            else meta.Add("бандл неизвестен");
            var took = d.At("tookTop").I();
            var off = d.At("offRec").I();
            var reasons = d.At("withReason").I();
            meta.Add($"#1={took}");
            if (off > 0) meta.Add($"вне списка={off}");
            if (reasons > 0) meta.Add($"причин={reasons}");
            var metaLine = Text(string.Join(" · ", meta), 11, color: muted);
            metaLine.TextWrapping = TextWrapping.Wrap;
            metaLine.Margin = new Thickness(0, 2, 0, 0);
            sub.Children.Add(metaLine);
            var row = new Border
            {
                Child = sub,
                Tag = id,
                Background = Brushes.Transparent,
                Padding = new Thickness(2)
            };
            System.Windows.Automation.AutomationProperties.SetName(row, "История драфт " + id);
            historyDraftList.Items.Add(row);
        }

        lastHistoryEvidence = evidence switch
        {
            "ready" => EvidenceKind.Ready,
            "partial" => EvidenceKind.Partial,
            "unavailable" => EvidenceKind.Error,
            _ => EvidenceKind.Empty
        };
        if (!coreOnline) lastHistoryEvidence = EvidenceKind.Offline;
        if (!string.IsNullOrWhiteSpace(historyLoadError)) lastHistoryEvidence = EvidenceKind.Error;
        RefreshEvidenceBanners(forceShowReady: lastHistoryEvidence != EvidenceKind.Ready);
    }

    private void ApplyHistoryDetailPayload(JsonNode? data)
    {
        if (historyDetailHost == null || historyDetailTitle == null) return;
        historyDetailHost.Children.Clear();
        if (!data.At("found").B())
        {
            RenderHistoryDetailEmpty(data.At("note").S("Драфт не найден."));
            return;
        }
        var draft = data.At("draft");
        if (draft == null)
        {
            RenderHistoryDetailEmpty("Пустой ответ детализации.");
            return;
        }

        var when = FormatHistoryTs(draft.At("ts").L());
        historyDetailTitle.Text = when + " · " + draft.At("id").S();

        var head = new StackPanel { Margin = new Thickness(0, 0, 0, 12) };
        head.Children.Add(Text(
            $"Исход: {FormatHistoryOutcome(draft.At("outcome"))} · сторона {OrDash(draft.At("ourTeam").S())} · first pick {OrDash(draft.At("firstPick").S())} · {OrDash(draft.At("source").S())}",
            12));
        var prov = new List<string>();
        var bundle = draft.At("bundleVersion").S();
        prov.Add(string.IsNullOrWhiteSpace(bundle) ? "бандл: нет в записи" : "бандл: " + bundle);
        var patch = draft.At("patchTag").S();
        prov.Add(string.IsNullOrWhiteSpace(patch) ? "патч: нет в записи" : "патч: " + patch);
        var snap = draft.At("snapshotHash").S();
        prov.Add(string.IsNullOrWhiteSpace(snap) ? "снапшот: нет в записи" : "снапшот: " + Truncate(snap, 16));
        var gaps = draft.At("provenanceGap")?.Items().Select(x => x.S()).Where(x => x.Length > 0).ToArray() ?? [];
        if (gaps.Length > 0)
            prov.Add("пробелы: " + string.Join(", ", gaps));
        var provLine = Text(string.Join(" · ", prov), 11, color: muted);
        provLine.TextWrapping = TextWrapping.Wrap;
        provLine.Margin = new Thickness(0, 4, 0, 0);
        head.Children.Add(provLine);
        historyDetailHost.Children.Add(head);

        var decisions = draft.At("decisions") as JsonArray ?? new JsonArray();
        if (decisions.Count == 0)
        {
            historyDetailHost.Children.Add(Text(
                "В этом драфте нет записанных наших шагов (только заголовок / исход).",
                12, color: muted));
            return;
        }

        foreach (var raw in decisions)
        {
            if (raw is not JsonObject dec) continue;
            historyDetailHost.Children.Add(BuildHistoryDecisionCard(dec));
        }
    }

    private Border BuildHistoryDecisionCard(JsonObject dec)
    {
        var action = dec.At("action").S("step");
        var actRu = action == "ban" ? "бан" : action == "pick" ? "пик" : action;
        var chosen = dec.At("chosen").I();
        var chosenName = HistoryHeroLabel(chosen, dec.At("chosenName").S());
        var rank = dec.At("chosenRank").I();
        var rankTxt = rank == 1 ? "#1" : rank > 0 ? "#" + rank : "вне списка";
        var accepted = dec.At("accepted").B();

        var box = new StackPanel { Margin = new Thickness(0, 0, 0, 10) };
        var head = Text(
            $"Ход {dec.At("seq").I() + 1} · {actRu} · взяли {chosenName} ({rankTxt})",
            13, FontWeights.SemiBold,
            accepted ? good : (rank == 0 ? (Brush)FindResource("ErrorBrush") : gold));
        head.TextWrapping = TextWrapping.Wrap;
        box.Children.Add(head);

        var topHero = dec.At("topHero").I();
        if (topHero > 0)
        {
            var topName = HistoryHeroLabel(topHero, dec.At("topName").S());
            var topWhy = dec.At("topWhy").S();
            var topScore = dec.At("topScore");
            var scorePart = topScore != null ? $" · score {topScore.D():0.###}" : "";
            var topLine = Text(
                $"Совет #1: {topName}{scorePart}" + (string.IsNullOrWhiteSpace(topWhy) ? "" : " — " + topWhy),
                12, color: muted);
            topLine.TextWrapping = TextWrapping.Wrap;
            topLine.Margin = new Thickness(0, 4, 0, 0);
            box.Children.Add(topLine);
        }
        else
        {
            var miss = Text("В записи нет списка рекомендаций для этого шага.", 11, color: muted);
            miss.Margin = new Thickness(0, 4, 0, 0);
            box.Children.Add(miss);
        }

        var reason = dec.At("reason").S();
        if (!string.IsNullOrWhiteSpace(reason))
        {
            var why = Text("Почему отошли: " + reason, 12, FontWeights.Medium);
            why.TextWrapping = TextWrapping.Wrap;
            why.Margin = new Thickness(0, 6, 0, 0);
            box.Children.Add(why);
        }
        else if (!accepted)
        {
            var noWhy = Text("Причина отказа не записана.", 11, color: muted);
            noWhy.Margin = new Thickness(0, 6, 0, 0);
            box.Children.Add(noWhy);
        }

        if (dec.At("strength") != null)
        {
            var str = Text($"Сила доски до хода (из лога): {dec.At("strength").D():0.###}", 11, color: muted);
            str.Margin = new Thickness(0, 4, 0, 0);
            box.Children.Add(str);
        }

        var preds = dec.At("enemyPred") as JsonArray;
        if (preds is { Count: > 0 })
        {
            var bits = preds.Take(3).Select(p =>
            {
                var hid = p.At("heroId").I();
                var nm = HistoryHeroLabel(hid, p.At("name").S());
                var src = p.At("source").S();
                return $"{nm} {p.At("prob").D():0.##}" + (string.IsNullOrWhiteSpace(src) ? "" : $" ({src})");
            });
            var predLine = Text("Предикт врага (из лога): " + string.Join(", ", bits), 11, color: muted);
            predLine.TextWrapping = TextWrapping.Wrap;
            predLine.Margin = new Thickness(0, 4, 0, 0);
            box.Children.Add(predLine);
        }

        return new Border
        {
            Child = box,
            Background = ThemeBrush("RaisedBrush"),
            BorderBrush = border,
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(8),
            Padding = new Thickness(12, 10, 12, 10),
            Margin = new Thickness(0, 0, 0, 8)
        };
    }

    private void RenderHistoryDetailEmpty(string message)
    {
        if (historyDetailHost == null || historyDetailTitle == null) return;
        historyDetailHost.Children.Clear();
        historyDetailTitle.Text = "Нет детализации";
        var line = Text(message, 12, color: muted);
        line.TextWrapping = TextWrapping.Wrap;
        historyDetailHost.Children.Add(line);
    }

    private string HistoryHeroLabel(int id, string fromApi)
    {
        if (!string.IsNullOrWhiteSpace(fromApi) && fromApi != id.ToString(CultureInfo.InvariantCulture))
            return fromApi;
        if (id <= 0) return "—";
        var fromState = state?.At("names", id.ToString(CultureInfo.InvariantCulture)).S();
        if (!string.IsNullOrWhiteSpace(fromState) && fromState != "Неизвестный герой")
            return fromState;
        return "Герой " + id;
    }

    private static string FormatHistoryTs(long unix)
    {
        if (unix <= 0) return "без даты";
        try
        {
            return DateTimeOffset.FromUnixTimeSeconds(unix).ToLocalTime().ToString("yyyy-MM-dd HH:mm");
        }
        catch
        {
            return unix.ToString(CultureInfo.InvariantCulture);
        }
    }

    private static string FormatHistoryOutcome(JsonNode? outcome)
    {
        if (outcome is null) return "исход не записан";
        if (outcome is JsonValue jv)
        {
            try
            {
                if (jv.GetValueKind() == System.Text.Json.JsonValueKind.Null)
                    return "исход не записан";
            }
            catch
            {
                return "исход не записан";
            }
        }
        if (outcome is not JsonObject) return "исход не записан";
        return outcome.At("win").B() ? "победа" : "поражение";
    }

    private static string OrDash(string s) => string.IsNullOrWhiteSpace(s) ? "—" : s;

    private static string Truncate(string s, int n) =>
        string.IsNullOrEmpty(s) ? s : s.Length <= n ? s : s[..n] + "…";

    private static string ShortErr(string msg) =>
        msg.Length <= 120 ? msg : msg[..117] + "…";

    private EvidenceKind ResolveHistoryEvidence(out string title, out string detail)
    {
        title = "";
        detail = "";
        if (!coreOnline)
        {
            title = "Ядро недоступно";
            detail = "История читается из локального файла через ядро. Пока офлайн — показан только SAMPLE/кэш превью, не живой журнал.";
            return EvidenceKind.Offline;
        }
        if (historyLoading)
        {
            title = "Читаю журнал";
            detail = "Запрос /api/declog/history…";
            return EvidenceKind.Loading;
        }
        if (!string.IsNullOrWhiteSpace(historyLoadError))
        {
            title = "Ошибка журнала";
            detail = historyLoadError;
            return EvidenceKind.Error;
        }
        if (lastHistoryEvidence == EvidenceKind.Empty)
        {
            title = "Истории пока нет";
            detail = "Сыграй драфт с включённым decision log — записи появятся в decisions.jsonl. Пустой экран ≠ успех.";
            return EvidenceKind.Empty;
        }
        if (lastHistoryEvidence == EvidenceKind.Partial)
        {
            title = "История частичная";
            detail = "Есть драфты без исхода или без bundle/patch/snapshot в записи. Пробелы не заполняются выдумкой.";
            return EvidenceKind.Partial;
        }
        if (lastHistoryEvidence == EvidenceKind.Error)
        {
            title = "Журнал недоступен";
            detail = "Путь к decisions.jsonl неизвестен или файл не читается.";
            return EvidenceKind.Error;
        }
        title = "История готова";
        detail = "Локальные драфты и шаги «совет vs выбор» из decisions.jsonl.";
        return EvidenceKind.Ready;
    }

    private void ApplyHistoryPreviewPayload()
    {
        coreOnline = !(state?.At("meta", "previewOffline").B() ?? false);
        historyLoadError = state?.At("meta", "historyError").S() ?? "";
        historyLoading = state?.At("meta", "historyLoading").B() ?? false;
        var drafts = previewHistoryDrafts ?? SampleHistoryDrafts();
        var list = new JsonObject
        {
            ["path"] = "SAMPLE/decisions.jsonl",
            ["exists"] = drafts.Count > 0,
            ["logging"] = true,
            ["count"] = drafts.Count,
            ["drafts"] = drafts.DeepClone(),
            ["note"] = drafts.Count == 0
                ? "SAMPLE · журнал пуст"
                : "SAMPLE DATA · локальный журнал (fixture)",
            ["evidence"] = drafts.Count == 0
                ? "empty"
                : drafts.Any(d => (d.At("provenanceGap") as JsonArray)?.Count > 0)
                    ? "partial"
                    : "ready"
        };
        if (!string.IsNullOrWhiteSpace(historyLoadError))
        {
            list["evidence"] = "unavailable";
            list["note"] = historyLoadError;
            list["drafts"] = new JsonArray();
            list["count"] = 0;
        }
        if (!coreOnline)
        {
            list["note"] = "SAMPLE · офлайн";
        }
        ApplyHistoryListPayload(list);
        if (drafts.Count > 0 && historyDraftList != null)
        {
            historyDraftList.SelectedIndex = 0;
            var id = drafts[0]?.At("id").S() ?? "sample-1";
            ApplyHistoryPreviewDetail(id);
        }
        else
        {
            RenderHistoryDetailEmpty("SAMPLE · нет драфтов для детализации.");
        }
        RefreshEvidenceBanners(forceShowReady: true);
    }

    private void ApplyHistoryPreviewDetail(string id)
    {
        var detail = previewHistoryDetail ?? SampleHistoryDetail(id);
        ApplyHistoryDetailPayload(detail);
    }

    private static JsonArray SampleHistoryDrafts(bool empty = false)
    {
        if (empty) return new JsonArray();
        return new JsonArray(
            new JsonObject
            {
                ["id"] = "sample-1",
                ["ts"] = DateTimeOffset.UtcNow.AddHours(-5).ToUnixTimeSeconds(),
                ["ourTeam"] = "radiant",
                ["firstPick"] = "radiant",
                ["source"] = "gsi",
                ["bundleVersion"] = "sample-bundle",
                ["patchTag"] = "7.39",
                ["snapshotHash"] = "deadbeefcafe",
                ["enemyCount"] = 5,
                ["allyCount"] = 5,
                ["decisionCount"] = 3,
                ["tookTop"] = 2,
                ["offRec"] = 1,
                ["withReason"] = 1,
                ["finalPresent"] = true,
                ["outcome"] = new JsonObject { ["win"] = true },
                ["provenanceGap"] = new JsonArray()
            },
            new JsonObject
            {
                ["id"] = "sample-2",
                ["ts"] = DateTimeOffset.UtcNow.AddDays(-1).ToUnixTimeSeconds(),
                ["ourTeam"] = "dire",
                ["firstPick"] = "radiant",
                ["source"] = "manual",
                ["bundleVersion"] = "",
                ["patchTag"] = "",
                ["snapshotHash"] = "",
                ["enemyCount"] = 0,
                ["allyCount"] = 0,
                ["decisionCount"] = 2,
                ["tookTop"] = 0,
                ["offRec"] = 1,
                ["withReason"] = 0,
                ["finalPresent"] = false,
                ["outcome"] = null,
                ["provenanceGap"] = new JsonArray("bundleVersion", "patchTag", "snapshotHash", "outcome")
            });
    }

    private static JsonObject SampleHistoryDetail(string id)
    {
        var complete = id != "sample-2";
        return new JsonObject
        {
            ["path"] = "SAMPLE/decisions.jsonl",
            ["logging"] = true,
            ["found"] = true,
            ["note"] = "SAMPLE DATA · детализация",
            ["evidence"] = complete ? "ready" : "partial",
            ["draft"] = new JsonObject
            {
                ["id"] = id,
                ["ts"] = DateTimeOffset.UtcNow.AddHours(-5).ToUnixTimeSeconds(),
                ["ourTeam"] = complete ? "radiant" : "dire",
                ["firstPick"] = "radiant",
                ["source"] = complete ? "gsi" : "manual",
                ["bundleVersion"] = complete ? "sample-bundle" : "",
                ["patchTag"] = complete ? "7.39" : "",
                ["snapshotHash"] = complete ? "deadbeefcafe" : "",
                ["decisionCount"] = 2,
                ["tookTop"] = complete ? 1 : 0,
                ["offRec"] = 1,
                ["withReason"] = complete ? 1 : 0,
                ["finalPresent"] = complete,
                ["outcome"] = complete ? new JsonObject { ["win"] = true } : null,
                ["provenanceGap"] = complete
                    ? new JsonArray()
                    : new JsonArray("bundleVersion", "patchTag", "snapshotHash", "outcome"),
                ["decisions"] = new JsonArray(
                    new JsonObject
                    {
                        ["seq"] = 0,
                        ["action"] = "ban",
                        ["chosen"] = 74,
                        ["chosenName"] = "Invoker",
                        ["chosenRank"] = 2,
                        ["accepted"] = false,
                        ["reason"] = complete ? "тиммейт захотел" : "",
                        ["topHero"] = 11,
                        ["topName"] = "Shadow Fiend",
                        ["topWhy"] = "сильный мид в текущей мете",
                        ["topScore"] = 0.55,
                        ["recs"] = new JsonArray(
                            new JsonObject { ["heroId"] = 11, ["name"] = "Shadow Fiend", ["score"] = 0.55, ["why"] = "сильный мид в текущей мете" },
                            new JsonObject { ["heroId"] = 74, ["name"] = "Invoker", ["score"] = 0.50 })
                    },
                    new JsonObject
                    {
                        ["seq"] = 2,
                        ["action"] = "pick",
                        ["chosen"] = 8,
                        ["chosenName"] = "Juggernaut",
                        ["chosenRank"] = 1,
                        ["accepted"] = true,
                        ["reason"] = "",
                        ["topHero"] = 8,
                        ["topName"] = "Juggernaut",
                        ["topWhy"] = "сейфлейн фарм",
                        ["topScore"] = 0.58,
                        ["strength"] = 0.52,
                        ["enemyPred"] = new JsonArray(
                            new JsonObject { ["heroId"] = 1, ["name"] = "Anti-Mage", ["prob"] = 0.22, ["source"] = "meta" })
                    })
            }
        };
    }

    /// <summary>
    /// Gate 2.4 History honesty fixtures. Outside geometry.json (CI Count==33).
    /// </summary>
    internal void LoadHistoryPreviewScenario(string scenario)
    {
        LoadVisualFixture("История");
        state!["meta"]!["previewOffline"] = false;
        state["meta"]!.AsObject().Remove("historyError");
        state["meta"]!.AsObject().Remove("historyLoading");
        coreOnline = true;
        ApplyConnectionChrome("Go-ядро подключено");
        historyLoading = false;
        historyLoadError = "";
        previewHistoryDrafts = SampleHistoryDrafts();
        previewHistoryDetail = null;

        if (scenario is "ready" or "happy")
        {
            // Only the complete draft so StateKit reads Ready (not Partial).
            previewHistoryDrafts = new JsonArray(SampleHistoryDrafts()[0]!.DeepClone());
        }
        else if (scenario == "empty")
        {
            previewHistoryDrafts = SampleHistoryDrafts(empty: true);
        }
        else if (scenario == "partial")
        {
            previewHistoryDrafts = new JsonArray(SampleHistoryDrafts()[1]!.DeepClone());
        }
        else if (scenario == "loading")
        {
            historyLoading = true;
            state["meta"]!["historyLoading"] = true;
            previewHistoryDrafts = SampleHistoryDrafts(empty: true);
        }
        else if (scenario == "error")
        {
            historyLoadError = "SAMPLE · не удалось прочитать decisions.jsonl";
            state["meta"]!["historyError"] = historyLoadError;
            previewHistoryDrafts = SampleHistoryDrafts(empty: true);
        }
        else if (scenario == "offline")
        {
            state["meta"]!["previewOffline"] = true;
            coreOnline = false;
            ApplyConnectionChrome("SAMPLE · офлайн");
        }
        else if (scenario == "detail")
        {
            previewHistoryDrafts = SampleHistoryDrafts();
        }

        ApplyHistoryPreviewPayload();
        toast.Text = "SAMPLE DATA · history-" + scenario;
    }
}
