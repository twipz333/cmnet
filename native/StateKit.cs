using System;
using System.Linq;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;

namespace Cmnet.Native;

/// <summary>
/// Whole-app evidence state kit. Empty / loading / offline / partial / error /
/// stale must never share success (teal/green) chrome. Ready is the only
/// success-looking tone; degraded states use warning / danger / muted.
/// </summary>
public enum EvidenceKind
{
    Ready,
    Empty,
    Loading,
    Offline,
    Partial,
    Error,
    Stale
}

public sealed partial class NativeWindow
{
    private Border? scoutEvidenceBanner;
    private Border? draftEvidenceBanner;
    // dataEvidenceBanner lives with DataView fields; resolved here.
    private bool coreOnline = true;
    private string connectionMessage = "●  Соединение…";
    private EvidenceKind lastScoutEvidence = EvidenceKind.Ready;
    private EvidenceKind lastDraftEvidence = EvidenceKind.Ready;

    private (string Label, Brush Tone, Brush Border) EvidenceChrome(EvidenceKind kind) => kind switch
    {
        EvidenceKind.Ready => ("ГОТОВО", good, ThemeBrush("PrimaryBrush")),
        EvidenceKind.Empty => ("ПУСТО", muted, border),
        EvidenceKind.Loading => ("ЗАГРУЗКА", accent, ThemeBrush("PrimaryBrush")),
        EvidenceKind.Offline => ("ОФЛАЙН", gold, gold),
        EvidenceKind.Partial => ("ЧАСТИЧНО", gold, gold),
        EvidenceKind.Error => ("ОШИБКА", (Brush)FindResource("ErrorBrush"), (Brush)FindResource("ErrorBrush")),
        EvidenceKind.Stale => ("УСТАРЕЛО", gold, gold),
        _ => ("СОСТОЯНИЕ", muted, border)
    };

    private Border BuildEvidenceBanner(string name)
    {
        var pill = Text("—", 10, FontWeights.Bold, muted);
        pill.Name = name + "Kind";
        var title = Text("", 12, FontWeights.SemiBold);
        title.Name = name + "Title";
        title.TextWrapping = TextWrapping.Wrap;
        var detail = Text("", 11, color: muted);
        detail.Name = name + "Detail";
        detail.TextWrapping = TextWrapping.Wrap;
        detail.Margin = new Thickness(0, 2, 0, 0);

        var copy = new StackPanel();
        copy.Children.Add(title);
        copy.Children.Add(detail);

        var row = new DockPanel { LastChildFill = true };
        var badge = new Border
        {
            Child = pill,
            Background = ThemeBrush("RaisedBrush"),
            BorderBrush = border,
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(99),
            Padding = new Thickness(10, 3, 10, 3),
            Margin = new Thickness(0, 0, 10, 0),
            VerticalAlignment = VerticalAlignment.Top
        };
        DockPanel.SetDock(badge, Dock.Left);
        row.Children.Add(badge);
        row.Children.Add(copy);

        return new Border
        {
            Name = name,
            Child = row,
            Background = ThemeBrush("RaisedBrush"),
            BorderBrush = border,
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(8),
            Padding = new Thickness(12, 8, 12, 8),
            Margin = new Thickness(0, 0, 0, 10),
            Visibility = Visibility.Collapsed,
            Tag = new EvidenceBannerParts(pill, title, detail, badge)
        };
    }

    private sealed record EvidenceBannerParts(TextBlock Pill, TextBlock Title, TextBlock Detail, Border Badge);

    private void SetEvidenceBanner(Border? banner, EvidenceKind kind, string title, string detail, bool forceShow = false)
    {
        if (banner?.Tag is not EvidenceBannerParts parts) return;
        var (label, tone, edge) = EvidenceChrome(kind);
        parts.Pill.Text = label;
        parts.Pill.Foreground = tone;
        parts.Badge.BorderBrush = edge;
        parts.Title.Text = title;
        parts.Title.Foreground = kind == EvidenceKind.Ready ? foreground : tone;
        parts.Detail.Text = detail;
        parts.Detail.Visibility = compactScout && banner == scoutEvidenceBanner
            ? Visibility.Collapsed
            : Visibility.Visible;
        banner.BorderBrush = edge;
        banner.Padding = compactScout && banner == scoutEvidenceBanner
            ? new Thickness(10, 4, 10, 4)
            : new Thickness(12, 8, 12, 8);
        banner.Margin = compactScout && banner == scoutEvidenceBanner
            ? new Thickness(0, 0, 0, 4)
            : new Thickness(0, 0, 0, 10);
        // Ready is silent on happy path unless forceShow. On compact Scout
        // viewports the dedicated banner yields height to opponent/intel panels
        // and the kind is mirrored into the activity row instead.
        bool hideForCompact = compactScout && banner == scoutEvidenceBanner && kind is EvidenceKind.Ready or EvidenceKind.Partial;
        banner.Visibility = (kind == EvidenceKind.Ready && !forceShow) || hideForCompact
            ? Visibility.Collapsed
            : Visibility.Visible;
    }

    private EvidenceKind ResolveScoutEvidence(out string title, out string detail)
    {
        title = "";
        detail = "";
        if (!coreOnline)
        {
            title = "Ядро недоступно";
            detail = "Разведка не обновляется. Показано только то, что уже в локальном кэше — не свежий результат.";
            return EvidenceKind.Offline;
        }

        var active = state?.At("ops", "active").Items()
            .Where(j => j.At("kind").S() is "scout" or "scout-allies")
            .ToList() ?? [];
        if (active.Count > 0)
        {
            title = "Разведка выполняется";
            detail = string.Join(" · ", active.Select(j => j.At("lastLine").S("ожидание источника")));
            return EvidenceKind.Loading;
        }

        var ids = state?.At("team", "enemy").Items().Select(x => x.L()).Where(id => id > 0).ToArray() ?? [];
        if (ids.Length == 0)
        {
            title = "Состав противников пуст";
            detail = "Добавь до пяти игроков через «Управление составами». Пустой экран — не успешная разведка.";
            return EvidenceKind.Empty;
        }

        var reports = stats?.At("stats", "enemyPlayers").Items().ToArray() ?? [];
        int ready = 0, hidden = 0, errors = 0, stale = 0, pending = 0;
        foreach (var id in ids)
        {
            var report = reports.FirstOrDefault(p => p.At("accountId").L() == id);
            var source = report?.At("scoutStatus").S("pending") ?? "pending";
            switch (source)
            {
                case "ok": ready++; break;
                case "error": errors++; break;
                case "stale": stale++; break;
                case "no_stats":
                case "unresolved":
                    hidden++; break;
                default: pending++; break;
            }
        }

        if (errors > 0 && ready == 0)
        {
            title = "Источник разведки недоступен";
            detail = errors + " ошибка(и). Карточки с прежними героями — кэш, не свежий успех.";
            return EvidenceKind.Error;
        }
        if (stale > 0 && ready == 0 && errors == 0)
        {
            title = "Данные устарели";
            detail = "Повтори разведку. Устаревшие WR/пулы помечены и не выдаются за актуальные.";
            return EvidenceKind.Stale;
        }
        if (errors > 0 || hidden > 0 || stale > 0 || pending > 0 || ready < ids.Length)
        {
            title = "Разведка неполная";
            detail = $"Готово {ready}/{ids.Length}" +
                     (hidden + stale > 0 ? $" · скрыто/устарело {hidden + stale}" : "") +
                     (errors > 0 ? $" · ошибок {errors}" : "") +
                     (pending > 0 ? $" · ожидает {pending}" : "") +
                     ". Частичный результат — не полный успех.";
            return EvidenceKind.Partial;
        }

        title = "Разведка готова";
        detail = $"Подтверждённые данные по {ready} игрокам.";
        return EvidenceKind.Ready;
    }

    private EvidenceKind ResolveDraftEvidence(out string title, out string detail)
    {
        title = "";
        detail = "";
        if (!coreOnline)
        {
            title = "Ядро недоступно";
            detail = "Рекомендации не пересчитываются. Доска может быть локальным снимком — не живой успех.";
            return EvidenceKind.Offline;
        }

        var active = state?.At("ops", "active").Items()
            .Where(j => j.At("kind").S() is "recommend" or "draft" or "train")
            .ToList() ?? [];
        if (active.Count > 0)
        {
            title = "Идёт пересчёт";
            detail = string.Join(" · ", active.Select(j => j.At("lastLine").S("ожидание модели")));
            return EvidenceKind.Loading;
        }

        if (state != null && !state.At("meta", "draftReady").B())
        {
            title = "Модель не готова";
            detail = state.At("meta", "draftError").S("Модель ещё не обучена. Открой раздел «Данные».");
            return EvidenceKind.Error;
        }

        var picks = new[] { "radiantPicks", "direPicks", "radiantBans", "direBans" }
            .SelectMany(k => state?.At("board", k).Items().Select(x => x.I()) ?? Array.Empty<int>())
            .Count(id => id > 0);
        var recCount = state?.At("result", "recs").Items().Count(r => r.At("heroId").I() > 0) ?? 0;
        var gsiMode = state?.At("meta", "gsiMode").B() == true;
        var gsiSeen = state?.At("meta", "gsiSeen").B() == true;
        if (gsiMode && !gsiSeen)
        {
            title = "GSI · ожидание события";
            detail = "Доска только для чтения. Это не live-успех, пока Dota не прислала первый POST.";
            return EvidenceKind.Loading;
        }

        if (picks == 0 && recCount == 0)
        {
            title = "Драфт ещё не начат";
            detail = gsiMode
                ? "GSI активен, но пиков/банов ещё нет — пустая доска, не выдуманный драфт."
                : "Пустая доска и нет рекомендаций — это пустое состояние, не «модель ничего не нашла».";
            return EvidenceKind.Empty;
        }

        if (state?.At("modelUpdatePending").B() == true)
        {
            title = "Есть более новая модель";
            detail = "Текущие рекомендации могут быть устаревшими до следующего драфта.";
            return EvidenceKind.Stale;
        }

        var enemyIds = state?.At("team", "enemy").Items().Select(x => x.L()).Where(id => id > 0).ToArray() ?? [];
        if (enemyIds.Length > 0)
        {
            var reports = stats?.At("stats", "enemyPlayers").Items().ToArray() ?? [];
            int ready = enemyIds.Count(id =>
                reports.FirstOrDefault(p => p.At("accountId").L() == id)?.At("scoutStatus").S() == "ok");
            if (ready < enemyIds.Length)
            {
                title = "Разведка на драфте неполная";
                detail = $"Сигналы противников: {ready}/{enemyIds.Length} подтверждено. Недостающее — пробел, не выдуманный интеллект.";
                return EvidenceKind.Partial;
            }
        }

        if (gsiMode && gsiSeen)
        {
            title = "GSI live · только чтение";
            detail = recCount > 0
                ? $"Доска из Dota · рекомендаций: {recCount}."
                : "Доска из Dota; список рекомендаций пуст для текущего шага.";
            return EvidenceKind.Ready;
        }

        title = "Драфт готов";
        detail = recCount > 0
            ? $"Ручной режим · рекомендаций: {recCount}."
            : "Модель готова; список рекомендаций пуст для текущего шага.";
        return EvidenceKind.Ready;
    }

    private EvidenceKind ResolveDataEvidence(out string title, out string detail)
    {
        title = "";
        detail = "";
        if (!coreOnline)
        {
            title = "Ядро недоступно";
            detail = "Статус sync/train и корпус не обновляются. Показанное — локальный снимок или SAMPLE, не живой успех.";
            return EvidenceKind.Offline;
        }

        var active = state?.At("ops", "active").Items()
            .Where(j => j.At("kind").S() is "sync" or "train")
            .ToList() ?? [];
        if (active.Count > 0)
        {
            title = "Идёт работа с данными";
            detail = string.Join(" · ", active.Select(j =>
                JobKindLabel(j.At("kind").S()) + ": " + j.At("lastLine").S(j.At("phase").S("ожидание"))));
            return EvidenceKind.Loading;
        }

        var recentFail = state?.At("ops", "recent").Items()
            .FirstOrDefault(j => (j.At("kind").S() is "sync" or "train") && j.At("state").S() == "failed");
        if (recentFail != null && state?.At("meta", "draftReady").B() != true)
        {
            title = "Последняя задача завершилась ошибкой";
            detail = recentFail.At("error").S("Смотри журнал задачи. Повтор — кнопкой «Повторить», без выдуманных метрик.");
            return EvidenceKind.Error;
        }

        var draftReady = state?.At("meta", "draftReady").B() == true;
        if (!draftReady)
        {
            var err = state?.At("meta", "draftError").S();
            if (!string.IsNullOrWhiteSpace(err))
            {
                title = "Модель недоступна";
                detail = err;
                return EvidenceKind.Error;
            }
            if (dataCorpusMatches == 0)
            {
                title = "Корпус пуст";
                detail = "Нет сохранённых матчей и нет обученной модели. Это пустое состояние, не готовность.";
                return EvidenceKind.Empty;
            }
            title = "Модель ещё не обучена";
            detail = dataCorpusMatches > 0
                ? $"В кэше ~{dataCorpusMatches:N0} матчей, но бандл не готов. Запусти обучение."
                : "Собери матчи, затем обучи модель. Пустой статус — не «всё хорошо».";
            return dataCorpusMatches > 0 ? EvidenceKind.Partial : EvidenceKind.Empty;
        }

        if (state?.At("modelUpdatePending").B() == true)
        {
            title = "Есть более новая модель";
            detail = "Обучение завершилось; текущий драфт может ещё держать прежний бандл до следующего /api/new-draft.";
            return EvidenceKind.Stale;
        }

        if (recentFail != null)
        {
            title = "Модель есть, последняя задача с ошибкой";
            detail = recentFail.At("error").S("Частичный статус: бандл на месте, но недавний sync/train упал.");
            return EvidenceKind.Partial;
        }

        if (dataCorpusMatches == 0)
        {
            title = "Модель есть, корпус не посчитан";
            detail = "Метрики кэша пусты или ещё не загружены. Не принимай «—» за нулевой успех сбора.";
            return EvidenceKind.Partial;
        }

        title = "Данные в порядке";
        detail = $"Модель готова · в окне корпуса ~{dataCorpusMatches:N0} матчей.";
        return EvidenceKind.Ready;
    }

    private void RefreshEvidenceBanners(bool forceShowReady = false)
    {
        if (page == "Разведка" && scoutEvidenceBanner != null)
        {
            lastScoutEvidence = ResolveScoutEvidence(out var title, out var detail);
            SetEvidenceBanner(scoutEvidenceBanner, lastScoutEvidence, title, detail, forceShowReady);
        }
        if (page == "Драфт" && draftEvidenceBanner != null)
        {
            lastDraftEvidence = ResolveDraftEvidence(out var title, out var detail);
            SetEvidenceBanner(draftEvidenceBanner, lastDraftEvidence, title, detail, forceShowReady);
            EnsureDraftRecPlaceholder(lastDraftEvidence, title, detail);
        }
        if (page == "Данные" && dataEvidenceBanner != null)
        {
            lastDataEvidence = ResolveDataEvidence(out var title, out var detail);
            SetEvidenceBanner(dataEvidenceBanner, lastDataEvidence, title, detail, forceShowReady);
        }
        if (page == "История" && historyEvidenceBanner != null)
        {
            lastHistoryEvidence = ResolveHistoryEvidence(out var title, out var detail);
            SetEvidenceBanner(historyEvidenceBanner, lastHistoryEvidence, title, detail,
                forceShow: forceShowReady || lastHistoryEvidence != EvidenceKind.Ready);
        }
        if (page == "Настройки" && settingsEvidenceBanner != null)
        {
            lastSettingsEvidence = ResolveSettingsEvidence(out var title, out var detail);
            SetEvidenceBanner(settingsEvidenceBanner, lastSettingsEvidence, title, detail,
                forceShow: forceShowReady || lastSettingsEvidence != EvidenceKind.Ready);
        }
        if (page == "Друзья" && friendsEvidenceBanner != null)
        {
            // Friends owns its banner copy via RefreshFriendsView (file exchange state).
            RefreshFriendsView();
        }
    }

    private void EnsureDraftRecPlaceholder(EvidenceKind kind, string title, string detail)
    {
        if (draftRecommendations == null) return;
        // When the model list is empty for a degraded reason, leave an explicit
        // non-success row so a blank ListBox never reads as "nothing worth picking".
        if (kind is EvidenceKind.Ready or EvidenceKind.Partial)
        {
            // Drop prior placeholders if real rows or quiet ready with rows exist.
            if (draftRecommendations.Items.Count == 1 &&
                draftRecommendations.Items[0] is FrameworkElement fe &&
                fe.Tag as string == "evidence-placeholder")
            {
                if (kind == EvidenceKind.Ready) draftRecommendations.Items.Clear();
            }
            return;
        }

        var hasReal = false;
        foreach (var item in draftRecommendations.Items)
        {
            if (item is FrameworkElement el && el.Tag as string != "evidence-placeholder")
            {
                hasReal = true;
                break;
            }
        }
        if (hasReal) return;

        draftRecommendations.Items.Clear();
        var (_, tone, edge) = EvidenceChrome(kind);
        var body = new StackPanel();
        body.Children.Add(Text(title, 12, FontWeights.SemiBold, tone));
        var line = Text(detail, 11, color: muted);
        line.TextWrapping = TextWrapping.Wrap;
        line.Margin = new Thickness(0, 4, 0, 0);
        body.Children.Add(line);
        var card = new Border
        {
            Tag = "evidence-placeholder",
            Child = body,
            Background = ThemeBrush("RaisedBrush"),
            BorderBrush = edge,
            BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(6),
            Padding = new Thickness(10, 8, 10, 8),
            Margin = new Thickness(0, 4, 0, 4),
            IsHitTestVisible = false
        };
        draftRecommendations.Items.Add(card);
        recommendationKey = "evidence:" + kind + ":" + title;
    }

    private void ApplyConnectionChrome(string message)
    {
        connectionMessage = message;
        // Preview / SAMPLE can force offline via meta.previewOffline.
        var previewOffline = state?.At("meta", "previewOffline").B() == true;
        if (previewOffline) coreOnline = false;
        else if (message.Contains("подключено", StringComparison.OrdinalIgnoreCase))
            coreOnline = true;
        else if (message.Contains("потерян", StringComparison.OrdinalIgnoreCase) ||
                 message.Contains("ошибк", StringComparison.OrdinalIgnoreCase) ||
                 message.Contains("недоступ", StringComparison.OrdinalIgnoreCase) ||
                 message.Contains("офлайн", StringComparison.OrdinalIgnoreCase) ||
                 message.Contains("offline", StringComparison.OrdinalIgnoreCase))
            coreOnline = false;

        if (!coreOnline)
        {
            connectionBadge.Text = previewOffline
                ? "●  SAMPLE · офлайн"
                : (message.StartsWith('●') ? message : "●  " + message);
            connectionBadge.Foreground = gold;
            return;
        }

        if (message.Contains("подключено", StringComparison.OrdinalIgnoreCase))
        {
            connectionBadge.Text = "●  Локально · онлайн";
            connectionBadge.Foreground = good;
            return;
        }

        // Boot / connecting — not success.
        connectionBadge.Text = message.StartsWith('●') ? message : "●  " + message;
        connectionBadge.Foreground = accent;
    }
}
