using System;
using System.IO;
using System.Net.Http;
using System.Security.Cryptography;
using System.Text;
using System.Threading;
using System.Threading.Tasks;

namespace Cmnet.Native;

/// <summary>
/// Native update check / stage / apply gate (Gate 3.3). Download never replaces
/// the live install in-place: staging + hash + signature, then optional NSIS
/// launch. Failures leave the current install untouched.
/// </summary>
internal static class UpdateChannel
{
    public const string EnvManifestUrl = "CMNET_UPDATE_MANIFEST_URL";

    public enum CheckKind
    {
        UpToDate,
        Available,
        Unavailable,
        Unsigned,
        Rejected,
        Error
    }

    public sealed record CheckResult(
        CheckKind Kind,
        string TitleRu,
        string DetailRu,
        UpdateManifest.Document? Manifest,
        bool CanApply,
        string? CurrentVersion,
        string? RemoteVersion);

    public sealed record StageResult(
        bool Ok,
        string TitleRu,
        string DetailRu,
        string? StagedPath);

    public static string StagingRoot()
    {
        var local = Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData);
        return Path.Combine(local, "CMNET", "updates");
    }

    public static string? ResolveManifestUrl(string? overrideUrl = null)
    {
        if (!string.IsNullOrWhiteSpace(overrideUrl)) return overrideUrl.Trim();
        var env = Environment.GetEnvironmentVariable(EnvManifestUrl);
        if (!string.IsNullOrWhiteSpace(env)) return env.Trim();
        try
        {
            var s = DesktopSettings.Load();
            var fromDesktop = s["nativeUpdateManifestUrl"]?.GetValue<string>();
            if (!string.IsNullOrWhiteSpace(fromDesktop)) return fromDesktop.Trim();
        }
        catch { /* ignore */ }
        return null;
    }

    public static CheckResult Evaluate(
        string currentVersion,
        UpdateManifest.Document? doc,
        UpdateTrust.Material? trust,
        string? fetchError = null)
    {
        if (!string.IsNullOrWhiteSpace(fetchError))
        {
            return new CheckResult(
                CheckKind.Unavailable,
                "Манифест недоступен",
                fetchError!,
                null,
                false,
                currentVersion,
                null);
        }

        if (doc == null)
        {
            return new CheckResult(
                CheckKind.Unavailable,
                "Канал обновлений не настроен",
                "URL манифеста пуст. Задайте " + EnvManifestUrl +
                " или nativeUpdateManifestUrl в desktop.json. Electron desktop.yml / latest.yml — отдельный канал.",
                null,
                false,
                currentVersion,
                null);
        }

        if (doc.Released)
        {
            // Honesty: client still must not claim Released for unsigned Gate 3.3 builds;
            // a remote flag alone does not promote this install.
        }

        if (trust == null)
        {
            return new CheckResult(
                CheckKind.Unsigned,
                "Обновление не применяется (нет ключа доверия)",
                "Публичный ключ не настроен — проверка подписи fail-closed. " +
                UpdateTrust.PlugInInstructionsRu() +
                (doc.Signature == null
                    ? " Манифест также без signature."
                    : " Манифест подписан, но ключ локально отсутствует — apply отключён."),
                doc,
                false,
                currentVersion,
                doc.Version);
        }

        if (doc.Signature == null)
        {
            return new CheckResult(
                CheckKind.Unsigned,
                "Манифест без подписи",
                "Ключ доверия есть, но signature отсутствует. Fail-closed: apply запрещён.",
                doc,
                false,
                currentVersion,
                doc.Version);
        }

        if (!UpdateTrust.VerifyRsaSha256(trust, UpdateManifest.CanonicalBytes(doc), doc.Signature.Value, out var verifyDetail))
        {
            return new CheckResult(
                CheckKind.Rejected,
                "Подпись отклонена",
                verifyDetail,
                doc,
                false,
                currentVersion,
                doc.Version);
        }

        var cmp = UpdateManifest.CompareVersions(doc.Version, StripInformational(currentVersion));
        if (cmp <= 0)
        {
            return new CheckResult(
                CheckKind.UpToDate,
                "Актуальная версия",
                $"Локально {currentVersion}, в манифесте {doc.Version}. Подпись OK. {verifyDetail}",
                doc,
                false,
                currentVersion,
                doc.Version);
        }

        return new CheckResult(
            CheckKind.Available,
            "Доступно обновление " + doc.Version,
            $"Подпись OK. Артефакт {doc.Artifact.FileName} ({doc.Artifact.Kind}). " +
            (doc.Released ? "released=true в манифесте (не путать с локальным статусом Released). " : "released=false. ") +
            (string.IsNullOrWhiteSpace(doc.Notes) ? verifyDetail : doc.Notes + " · " + verifyDetail),
            doc,
            true,
            currentVersion,
            doc.Version);
    }

    public static async Task<CheckResult> CheckAsync(
        string currentVersion,
        string? manifestUrlOrPath = null,
        string? trustPemPath = null,
        HttpMessageHandler? handler = null,
        CancellationToken ct = default)
    {
        var trust = UpdateTrust.TryLoad(trustPemPath);
        UpdateManifest.Document? doc = null;
        string? err = null;

        var target = ResolveManifestUrl(manifestUrlOrPath);
        if (string.IsNullOrWhiteSpace(target) && !string.IsNullOrWhiteSpace(manifestUrlOrPath))
            target = manifestUrlOrPath;

        if (string.IsNullOrWhiteSpace(target))
            return Evaluate(currentVersion, null, trust);

        try
        {
            string json;
            if (LooksLikePath(target!))
            {
                json = await File.ReadAllTextAsync(target!, ct).ConfigureAwait(false);
            }
            else
            {
                using var http = handler != null ? new HttpClient(handler, disposeHandler: false) : new HttpClient();
                http.Timeout = TimeSpan.FromSeconds(20);
                http.DefaultRequestHeaders.UserAgent.ParseAdd("CMNET-Native-Update/1.0");
                json = await http.GetStringAsync(new Uri(target!), ct).ConfigureAwait(false);
            }
            doc = UpdateManifest.Parse(json);
        }
        catch (UpdateManifest.ParseException ex)
        {
            return new CheckResult(
                CheckKind.Rejected,
                "Манифест отклонён",
                ex.Message,
                null,
                false,
                currentVersion,
                null);
        }
        catch (Exception ex)
        {
            err = "Не удалось получить манифест: " + ex.Message;
        }

        return Evaluate(currentVersion, doc, trust, err);
    }

    /// <summary>
    /// Download to staging. Never touches the install directory. On any failure,
    /// deletes the partial file and returns Ok=false.
    /// </summary>
    public static async Task<StageResult> StageDownloadAsync(
        UpdateManifest.Document doc,
        UpdateTrust.Material trust,
        HttpMessageHandler? handler = null,
        CancellationToken ct = default)
    {
        // Re-verify before network work.
        if (doc.Signature == null)
            return new StageResult(false, "Стадия отменена", "Манифест без подписи — fail-closed.", null);
        var sig = doc.Signature;
        if (!UpdateTrust.VerifyRsaSha256(trust, UpdateManifest.CanonicalBytes(doc), sig.Value, out var vdetail))
            return new StageResult(false, "Стадия отменена", "Подпись не прошла повторную проверку. " + vdetail, null);

        var dir = Path.Combine(StagingRoot(), "staging", Sanitize(doc.Version));
        var partial = Path.Combine(dir, doc.Artifact.FileName + ".partial");
        var final = Path.Combine(dir, doc.Artifact.FileName);

        try
        {
            Directory.CreateDirectory(dir);
            if (File.Exists(partial)) File.Delete(partial);
            if (File.Exists(final)) File.Delete(final);

            using var http = handler != null ? new HttpClient(handler, disposeHandler: false) : new HttpClient();
            http.Timeout = TimeSpan.FromMinutes(10);
            http.DefaultRequestHeaders.UserAgent.ParseAdd("CMNET-Native-Update/1.0");

            await using (var net = await http.GetStreamAsync(new Uri(doc.Artifact.Url), ct).ConfigureAwait(false))
            await using (var fs = new FileStream(partial, FileMode.CreateNew, FileAccess.Write, FileShare.None))
            {
                await net.CopyToAsync(fs, ct).ConfigureAwait(false);
            }

            var hash = await Sha256FileAsync(partial, ct).ConfigureAwait(false);
            if (!string.Equals(hash, doc.Artifact.Sha256, StringComparison.OrdinalIgnoreCase))
            {
                TryDelete(partial);
                return new StageResult(
                    false,
                    "Хеш не совпал",
                    $"Ожидали {doc.Artifact.Sha256}, получили {hash}. Файл удалён из staging; установка не тронута.",
                    null);
            }

            var len = new FileInfo(partial).Length;
            if (len != doc.Artifact.SizeBytes)
            {
                TryDelete(partial);
                return new StageResult(
                    false,
                    "Размер не совпал",
                    $"Ожидали {doc.Artifact.SizeBytes} байт, получили {len}. Staging очищен; установка не тронута.",
                    null);
            }

            File.Move(partial, final);
            return new StageResult(
                true,
                "Файл в staging",
                $"Сохранено: {final}. Текущая установка не изменена. Запуск NSIS — отдельный шаг (см. rollback в NOTES).",
                final);
        }
        catch (Exception ex)
        {
            TryDelete(partial);
            TryDelete(final);
            return new StageResult(
                false,
                "Загрузка не удалась",
                ex.Message + " Текущая установка не изменена.",
                null);
        }
    }

    /// <summary>
    /// Launch staged NSIS setup. Does not delete the current install; NSIS / friend's
    /// uninstall path is the recovery. Returns false if trust/path gates fail.
    /// </summary>
    public static bool TryLaunchStagedInstaller(string stagedPath, UpdateManifest.Document doc, UpdateTrust.Material trust, out string detail)
    {
        detail = "";
        if (doc.Signature == null)
        {
            detail = "Apply запрещён: манифест без подписи.";
            return false;
        }
        if (!UpdateTrust.VerifyRsaSha256(trust, UpdateManifest.CanonicalBytes(doc), doc.Signature.Value, out detail))
        {
            detail = "Apply запрещён: подпись не прошла. " + detail;
            return false;
        }

        if (!File.Exists(stagedPath))
        {
            detail = "Staged installer missing.";
            return false;
        }

        try
        {
            var hash = Sha256FileAsync(stagedPath, CancellationToken.None).GetAwaiter().GetResult();
            if (!string.Equals(hash, doc.Artifact.Sha256, StringComparison.OrdinalIgnoreCase))
            {
                detail = "Apply запрещён: хеш staging-файла не совпал.";
                return false;
            }

            var psi = new System.Diagnostics.ProcessStartInfo(stagedPath)
            {
                UseShellExecute = true
            };
            System.Diagnostics.Process.Start(psi);
            detail = "Запущен установщик из staging. При сбое переустановите предыдущий NSIS; %APPDATA%\\cmnet сохраняется.";
            return true;
        }
        catch (Exception ex)
        {
            detail = "Не удалось запустить установщик: " + ex.Message;
            return false;
        }
    }

    public static string FormatCheckLine(CheckResult r)
    {
        var sb = new StringBuilder();
        sb.Append('[').Append(r.Kind).Append("] ").Append(r.TitleRu);
        sb.Append(" · ").Append(r.DetailRu);
        sb.Append(" · canApply=").Append(r.CanApply ? "yes" : "no");
        if (!string.IsNullOrEmpty(r.CurrentVersion))
            sb.Append(" · current=").Append(r.CurrentVersion);
        if (!string.IsNullOrEmpty(r.RemoteVersion))
            sb.Append(" · remote=").Append(r.RemoteVersion);
        return sb.ToString();
    }

    public static EvidenceKind ToEvidence(CheckKind kind) => kind switch
    {
        CheckKind.UpToDate => EvidenceKind.Ready,
        CheckKind.Available => EvidenceKind.Partial,
        CheckKind.Unavailable => EvidenceKind.Offline,
        CheckKind.Unsigned => EvidenceKind.Stale,
        CheckKind.Rejected => EvidenceKind.Error,
        _ => EvidenceKind.Error
    };

    private static string StripInformational(string v)
    {
        // "0.4.0-native" → compare as 0.4.0
        var cut = v.IndexOf('-');
        return cut > 0 ? v[..cut] : v;
    }

    private static bool LooksLikePath(string s)
    {
        if (s.StartsWith("https://", StringComparison.OrdinalIgnoreCase) ||
            s.StartsWith("http://", StringComparison.OrdinalIgnoreCase))
            return false;
        return s.Contains('\\') || s.Contains('/') || File.Exists(s);
    }

    private static string Sanitize(string version)
    {
        var chars = version.ToCharArray();
        for (var i = 0; i < chars.Length; i++)
            if (chars[i] is < '0' or > '9' and not '.')
                chars[i] = '_';
        return new string(chars);
    }

    private static async Task<string> Sha256FileAsync(string path, CancellationToken ct)
    {
        await using var fs = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.Read);
        var hash = await SHA256.HashDataAsync(fs, ct).ConfigureAwait(false);
        return Convert.ToHexString(hash).ToLowerInvariant();
    }

    private static void TryDelete(string path)
    {
        try { if (File.Exists(path)) File.Delete(path); } catch { /* best-effort */ }
    }
}
