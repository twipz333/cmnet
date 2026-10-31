using System;
using System.Diagnostics;
using System.IO;

namespace Cmnet.Native;

/// <summary>
/// Gate 3.4 — local diagnostics live under %APPDATA%\cmnet\diagnostics
/// so NSIS uninstall (install tree only) preserves them with user data.
/// </summary>
internal static class DiagnosticsPaths
{
    public const string FolderName = "diagnostics";
    public const string CrashesSubfolder = "crashes";
    public const string ReadmeFileName = "README.txt";

    public static string Root() => Path.Combine(DesktopSettings.AppDataRoot(), FolderName);

    public static string CrashesDir() => Path.Combine(Root(), CrashesSubfolder);

    public static string ReadmePath() => Path.Combine(Root(), ReadmeFileName);

    /// <summary>
    /// Ensure diagnostics folder + honest README exist. Safe to call often.
    /// </summary>
    public static string EnsureLayout()
    {
        var root = Root();
        Directory.CreateDirectory(root);
        Directory.CreateDirectory(CrashesDir());
        WriteReadmeIfMissing();
        return root;
    }

    public static void WriteReadmeIfMissing()
    {
        var path = ReadmePath();
        if (File.Exists(path)) return;
        File.WriteAllText(path, BuildReadmeText());
    }

    public static string BuildReadmeText() =>
        "CMNET local diagnostics (Gate 3.4)\r\n" +
        "=================================\r\n" +
        "\r\n" +
        "This folder holds crash breadcrumbs and diagnostic notes written by the\r\n" +
        "native Windows app. Nothing here is uploaded to a paid crash cloud.\r\n" +
        "\r\n" +
        "Location: %APPDATA%\\cmnet\\diagnostics\r\n" +
        "Crashes:  crashes\\crash-YYYYMMDD-HHMMSS-fff.txt\r\n" +
        "\r\n" +
        "Secrets (tokens, API keys, IPC bearer, labeled account ids) are redacted\r\n" +
        "before write. Still treat this folder as private — do not paste into\r\n" +
        "public chats without reviewing.\r\n" +
        "\r\n" +
        "Uninstall of «CMNET (native)» removes the install dir only.\r\n" +
        "%APPDATA%\\cmnet (including this diagnostics folder and your data) is\r\n" +
        "preserved by design (Gate 3.2 / 3.4 contract).\r\n" +
        "\r\n" +
        "Open from the app: Settings → О программе → «Папка диагностики».\r\n" +
        "Status: preview — not Released.\r\n";

    /// <summary>
    /// Open Explorer on the diagnostics root. Returns false if Explorer fails.
    /// </summary>
    public static bool TryOpenInExplorer(out string detail)
    {
        try
        {
            var root = EnsureLayout();
            Process.Start(new ProcessStartInfo("explorer.exe", $"\"{root}\"")
            {
                UseShellExecute = true
            });
            detail = root;
            return true;
        }
        catch (Exception ex)
        {
            detail = ex.Message;
            return false;
        }
    }
}
