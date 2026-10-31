using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Linq;
using System.Text;
using System.Text.Json.Nodes;
using System.Threading.Tasks;

namespace Cmnet.Native;

/// <summary>
/// Electron↔native migration helpers. Filesystem inspection never invents data.
/// Lock probe and mutate operations shell out to <c>advisor migrate</c> and must
/// run when Electron is quit; while this app's Go core holds cmnet.db, Settings
/// shows filesystem facts only and points at the CLI for backup/migrate/rollback.
/// </summary>
internal static class DataMigrate
{
    public sealed record Status(
        string SourceDir,
        string DestDir,
        bool SameFolder,
        bool NeedsMigrate,
        bool DbLocked,
        string LockError,
        string Advice,
        string[] Backups,
        bool SourceHasDb,
        bool DestHasDb,
        bool SourceHasBundle,
        bool CoreHoldsDb);

    public static string AdvisorPath() =>
        Path.Combine(AppContext.BaseDirectory, "advisor.exe");

    /// <summary>
    /// Local inspection without a second SQLite open (safe while Go core is live).
    /// </summary>
    public static Status InspectFilesystem(string activeDataDir, bool coreOnline)
    {
        var nativeDefault = DesktopSettings.DefaultDataDirectory();
        var desktop = DesktopSettings.Load()["dataDir"]?.GetValue<string>();
        var source = !string.IsNullOrWhiteSpace(desktop) && Directory.Exists(desktop)
            ? Path.GetFullPath(desktop)
            : Path.GetFullPath(activeDataDir);
        var dest = Path.GetFullPath(nativeDefault);
        var srcInfo = DesktopSettings.Describe(source);
        var dstInfo = DesktopSettings.Describe(dest);
        var same = string.Equals(source, dest, StringComparison.OrdinalIgnoreCase);
        var backups = ListBackupDirs(activeDataDir)
            .Concat(same ? Array.Empty<string>() : ListBackupDirs(dest))
            .Distinct(StringComparer.OrdinalIgnoreCase)
            .OrderByDescending(x => x)
            .ToArray();
        var needs = !same && srcInfo != null && (dstInfo == null || dstInfo.DbBytes == 0);
        string advice;
        if (coreOnline)
            advice = "Ядро native сейчас держит cmnet.db — это нормально. Для advisor migrate backup|run|rollback закройте это окно и Electron, затем выполните команду.";
        else if (needs)
            advice = "Найдены данные Electron и пустая native-папка. Закройте Electron → advisor migrate run --update-desktop.";
        else if (same)
            advice = "Electron и native на одной папке. Не открывайте оба клиента сразу. Перед сменой клиента: advisor migrate backup.";
        else
            advice = "Папки готовы. Backup/migrate/rollback — через CLI при закрытых клиентах (см. NOTES.md).";

        return new Status(
            SourceDir: source,
            DestDir: dest,
            SameFolder: same,
            NeedsMigrate: needs,
            DbLocked: false,
            LockError: "",
            Advice: advice,
            Backups: backups,
            SourceHasDb: srcInfo != null,
            DestHasDb: dstInfo != null,
            SourceHasBundle: srcInfo?.HasBundle == true,
            CoreHoldsDb: coreOnline);
    }

    public static string[] ListBackupDirs(string dataDir)
    {
        var root = Path.Combine(dataDir, "migration-backup");
        if (!Directory.Exists(root)) return Array.Empty<string>();
        var list = new List<string>();
        foreach (var dir in Directory.EnumerateDirectories(root))
        {
            if (File.Exists(Path.Combine(dir, "MANIFEST.json")) ||
                File.Exists(Path.Combine(dir, "cmnet.db")))
                list.Add(Path.GetFullPath(dir));
        }
        list.Sort(StringComparer.OrdinalIgnoreCase);
        list.Reverse();
        return list.ToArray();
    }

    public static async Task EnsureNotLockedAsync(string dataDir)
    {
        var db = Path.Combine(dataDir, "cmnet.db");
        if (!File.Exists(db)) return;
        if (!File.Exists(AdvisorPath()))
            throw new FileNotFoundException("Нет advisor.exe рядом с Cmnet.Native.exe — нельзя проверить блокировку базы.");
        var (code, stdout, stderr) = await RunAsync("status", dataDir, dataDir, json: true);
        if (code != 0)
            throw new InvalidOperationException(string.IsNullOrWhiteSpace(stderr) ? stdout : stderr);
        var json = JsonNode.Parse(stdout) as JsonObject
            ?? throw new InvalidOperationException("migrate status returned empty JSON");
        if (json["dbLocked"]?.GetValue<bool>() == true)
        {
            var detail = json["lockError"]?.GetValue<string>() ?? json["advice"]?.GetValue<string>() ?? "";
            throw new InvalidOperationException(
                "cmnet.db занята другим процессом (Electron или второй cmnet). Закройте Electron и повторите. " + detail);
        }
    }

    public static Task<string> BackupAsync(string? from = null, string? to = null) =>
        RunSubcommandAsync("backup", from, to);

    public static Task<string> RunMigrateAsync(string? from = null, string? to = null, bool updateDesktop = true) =>
        RunSubcommandAsync("run", from, to, updateDesktop: updateDesktop);

    public static Task<string> RollbackAsync(string backupDir, string? to = null) =>
        RunSubcommandAsync("rollback", to: to, backup: backupDir);

    private static async Task<string> RunSubcommandAsync(
        string sub,
        string? from = null,
        string? to = null,
        bool updateDesktop = false,
        string? backup = null)
    {
        var (code, stdout, stderr) = await RunAsync(sub, from, to, updateDesktop: updateDesktop, backup: backup);
        if (code != 0)
            throw new InvalidOperationException(string.IsNullOrWhiteSpace(stderr) ? stdout : stderr);
        return string.IsNullOrWhiteSpace(stdout) ? "ok" : stdout.Trim();
    }

    private static async Task<(int Code, string Stdout, string Stderr)> RunAsync(
        string sub,
        string? from = null,
        string? to = null,
        bool json = false,
        bool updateDesktop = false,
        string? backup = null)
    {
        var exe = AdvisorPath();
        if (!File.Exists(exe))
            throw new FileNotFoundException("Missing advisor.exe next to Cmnet.Native.exe.", exe);
        var psi = new ProcessStartInfo(exe)
        {
            UseShellExecute = false,
            CreateNoWindow = true,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
        };
        psi.ArgumentList.Add("migrate");
        psi.ArgumentList.Add(sub);
        if (json) psi.ArgumentList.Add("--json");
        if (!string.IsNullOrWhiteSpace(from))
        {
            psi.ArgumentList.Add("--from");
            psi.ArgumentList.Add(from);
        }
        if (!string.IsNullOrWhiteSpace(to))
        {
            psi.ArgumentList.Add("--to");
            psi.ArgumentList.Add(to);
        }
        if (updateDesktop) psi.ArgumentList.Add("--update-desktop");
        if (!string.IsNullOrWhiteSpace(backup))
        {
            psi.ArgumentList.Add("--backup");
            psi.ArgumentList.Add(backup);
        }
        using var proc = Process.Start(psi) ?? throw new InvalidOperationException("Cannot start advisor.exe");
        var stdoutTask = proc.StandardOutput.ReadToEndAsync();
        var stderrTask = proc.StandardError.ReadToEndAsync();
        await proc.WaitForExitAsync();
        return (proc.ExitCode, await stdoutTask, await stderrTask);
    }

    public static string FormatStatus(Status st)
    {
        var sb = new StringBuilder();
        sb.AppendLine($"Источник\t{st.SourceDir}");
        sb.AppendLine($"  cmnet.db\t{(st.SourceHasDb ? "есть" : "нет")} · bundle\t{(st.SourceHasBundle ? "есть" : "нет")}");
        sb.AppendLine($"Назначение\t{st.DestDir}");
        sb.AppendLine($"  cmnet.db\t{(st.DestHasDb ? "есть" : "нет")}");
        sb.AppendLine($"Одна папка\t{(st.SameFolder ? "да" : "нет")}");
        sb.AppendLine($"Нужен copy\t{(st.NeedsMigrate ? "да" : "нет")}");
        sb.AppendLine($"Ядро держит БД\t{(st.CoreHoldsDb ? "да (закройте приложение перед migrate CLI)" : "нет")}");
        if (st.DbLocked) sb.AppendLine("База занята чужим процессом\tДА");
        if (st.Backups.Length == 0) sb.AppendLine("Backup\t(нет)");
        else
        {
            sb.AppendLine("Backup:");
            foreach (var b in st.Backups)
                sb.AppendLine("  " + b);
        }
        sb.Append("Совет\t").Append(st.Advice);
        return sb.ToString();
    }
}
