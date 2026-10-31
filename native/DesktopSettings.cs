using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Linq;
using System.Text.Json;
using System.Text.Json.Nodes;

namespace Cmnet.Native;

/// <summary>
/// Electron-compatible %APPDATA%\cmnet\desktop.json. First-run welcome and
/// data-dir discovery share this file so native and Electron stay interoperable.
/// </summary>
internal static class DesktopSettings
{
    private static readonly HashSet<string> SkipDirs = new(StringComparer.OrdinalIgnoreCase)
    {
        "windows", "program files", "program files (x86)", "programdata", "$recycle.bin",
        "system volume information", "appdata", "node_modules", ".git", "steamapps",
        "recovery", "perflogs", "msocache", "$windows.~bt", "$windows.~ws"
    };

    public static string AppDataRoot()
    {
        var appData = Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData);
        return Path.Combine(appData, "cmnet");
    }

    public static string SettingsPath() => Path.Combine(AppDataRoot(), "desktop.json");

    public static string DefaultDataDirectory() => Path.Combine(AppDataRoot(), "data");

    public static JsonObject Load()
    {
        var path = SettingsPath();
        if (!File.Exists(path)) return new JsonObject();
        try
        {
            return JsonNode.Parse(File.ReadAllText(path)) as JsonObject ?? new JsonObject();
        }
        catch
        {
            return new JsonObject();
        }
    }

    public static void Save(JsonObject settings)
    {
        Directory.CreateDirectory(AppDataRoot());
        var tmp = SettingsPath() + ".tmp";
        File.WriteAllText(tmp, settings.ToJsonString(new JsonSerializerOptions { WriteIndented = true }));
        File.Copy(tmp, SettingsPath(), true);
        File.Delete(tmp);
    }

    public static void SaveChoice(string dataDir)
    {
        var s = Load();
        s["dataDir"] = dataDir;
        s["setupDone"] = true;
        Save(s);
    }

    /// <summary>
    /// Mirror Electron needsSetup: show welcome when nobody has chosen a folder
    /// yet and the default folder has no trained model (bundle.json). An empty
    /// cmnet.db alone is not enough — the core creates one on first start.
    /// </summary>
    public static bool NeedsSetup()
    {
        var s = Load();
        if (s["setupDone"]?.GetValue<bool>() == true) return false;
        var selected = s["dataDir"]?.GetValue<string>();
        if (!string.IsNullOrWhiteSpace(selected)) return false;
        return !File.Exists(Path.Combine(DefaultDataDirectory(), "bundle.json"));
    }

    public sealed record FoundDataDir(string Dir, long DbBytes, DateTime ModifiedUtc, bool HasBundle);

    public static FoundDataDir? Describe(string dir)
    {
        try
        {
            var db = Path.Combine(dir, "cmnet.db");
            var info = new FileInfo(db);
            if (!info.Exists) return null;
            return new FoundDataDir(
                Path.GetFullPath(dir),
                info.Length,
                info.LastWriteTimeUtc,
                File.Exists(Path.Combine(dir, "bundle.json")));
        }
        catch
        {
            return null;
        }
    }

    /// <summary>
    /// Bounded scan of common user locations for folders that already hold cmnet.db.
    /// </summary>
    public static List<FoundDataDir> Scan(IEnumerable<string>? extra = null, int budgetMs = 4000)
    {
        var deadline = Stopwatch.StartNew();
        var found = new Dictionary<string, FoundDataDir>(StringComparer.OrdinalIgnoreCase);
        var home = Environment.GetFolderPath(Environment.SpecialFolder.UserProfile);
        var roots = new List<(string Dir, int Depth)>();
        if (extra != null)
        {
            foreach (var d in extra)
                if (!string.IsNullOrWhiteSpace(d)) roots.Add((d, 0));
        }
        foreach (var name in new[] { "Desktop", "Downloads", "Documents", "Games",
                     Path.Combine("OneDrive", "Desktop"), Path.Combine("OneDrive", "Documents") })
            roots.Add((Path.Combine(home, name), 3));
        roots.Add((home, 1));
        roots.Add((DefaultDataDirectory(), 0));
        roots.Add((AppDataRoot(), 1));
        foreach (var letter in "CDEFGHIJ")
        {
            var drive = letter + ":\\";
            if (Directory.Exists(drive)) roots.Add((drive, 2));
        }

        foreach (var (dir, depth) in roots)
        {
            if (deadline.ElapsedMilliseconds > budgetMs) break;
            Walk(dir, depth, found, deadline, budgetMs);
        }

        return found.Values.OrderByDescending(x => x.ModifiedUtc).ToList();
    }

    private static void Walk(string root, int depth, Dictionary<string, FoundDataDir> found, Stopwatch sw, int budgetMs)
    {
        if (sw.ElapsedMilliseconds > budgetMs || depth < 0 || string.IsNullOrWhiteSpace(root)) return;
        if (!Directory.Exists(root)) return;
        try
        {
            if (File.Exists(Path.Combine(root, "cmnet.db")))
            {
                var d = Describe(root);
                if (d != null) found[d.Dir] = d;
            }
            if (depth == 0) return;
            foreach (var entry in Directory.EnumerateDirectories(root))
            {
                if (sw.ElapsedMilliseconds > budgetMs) return;
                var name = Path.GetFileName(entry);
                if (string.IsNullOrEmpty(name) || name.StartsWith('.') || SkipDirs.Contains(name))
                    continue;
                Walk(entry, depth - 1, found, sw, budgetMs);
            }
        }
        catch
        {
            /* permission / IO — skip branch */
        }
    }

    public static string FormatSize(long bytes)
    {
        if (bytes > 1_000_000_000) return (bytes / 1_000_000_000d).ToString("0.0") + " ГБ";
        return Math.Max(1, (int)Math.Round(bytes / 1_000_000d)) + " МБ";
    }

    public static string FormatAgo(DateTime modifiedUtc)
    {
        var days = (int)Math.Floor((DateTime.UtcNow - modifiedUtc).TotalDays);
        if (days <= 0) return "обновлено сегодня";
        if (days == 1) return "обновлено вчера";
        return $"обновлено {days} дн. назад";
    }

    public static string AppVersionLabel() => ProductIdentity.AppVersionLabel();

    public static string CoreVersionLabel() => ProductIdentity.CoreVersionLabel();
}
