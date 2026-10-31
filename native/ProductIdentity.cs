using System;
using System.Diagnostics;
using System.IO;
using System.Reflection;

namespace Cmnet.Native;

/// <summary>
/// Single source of friend-facing product naming for the native WPF shell.
/// Keep window title, About, Welcome, shell wordmark, and assembly metadata aligned.
/// Do not claim Released here — packaging/signing live in later Gate 3 slices.
/// </summary>
internal static class ProductIdentity
{
    public const string ShortName = "CMNET";
    public const string Tagline = "Match Intelligence";
    public const string WindowTitle = ShortName + " · " + Tagline;
    public const string AboutHeading = WindowTitle;
    public const string ExeFileName = "Cmnet.Native.exe";
    public const string CoreFileName = "advisor.exe";

    /// <summary>Native client version from assembly InformationalVersion / FileVersion.</summary>
    public static string AppVersionLabel()
    {
        try
        {
            var informational = Assembly.GetExecutingAssembly()
                .GetCustomAttribute<AssemblyInformationalVersionAttribute>()
                ?.InformationalVersion;
            if (!string.IsNullOrWhiteSpace(informational))
                return TrimBuildMetadata(informational);

            var path = Environment.ProcessPath;
            if (!string.IsNullOrEmpty(path))
            {
                var info = FileVersionInfo.GetVersionInfo(path);
                if (!string.IsNullOrWhiteSpace(info.ProductVersion))
                    return TrimBuildMetadata(info.ProductVersion);
                if (!string.IsNullOrWhiteSpace(info.FileVersion))
                    return info.FileVersion;
            }
        }
        catch { /* preview / unpackaged */ }
        return "0.4.0-native (dev)";
    }

    /// <summary>
    /// Go sidecar version: prefer sibling advisor.version stamp written at publish,
    /// then PE ProductVersion, then a one-shot --version probe. Honest fallback when unmarked.
    /// </summary>
    public static string CoreVersionLabel()
    {
        try
        {
            var dir = AppContext.BaseDirectory;
            var stamp = Path.Combine(dir, "advisor.version");
            if (File.Exists(stamp))
            {
                var text = File.ReadAllText(stamp).Trim();
                if (!string.IsNullOrWhiteSpace(text))
                    return text;
            }

            var exe = Path.Combine(dir, CoreFileName);
            if (File.Exists(exe))
            {
                var info = FileVersionInfo.GetVersionInfo(exe);
                if (!string.IsNullOrWhiteSpace(info.ProductVersion) &&
                    !string.Equals(info.ProductVersion, "0.0.0.0", StringComparison.Ordinal))
                    return TrimBuildMetadata(info.ProductVersion);
            }

            var probed = ProbeAdvisorVersion(exe);
            if (!string.IsNullOrWhiteSpace(probed))
                return probed!;
        }
        catch { /* missing sidecar in smoke/capture is fine */ }
        return "не размечена (dev / без ldflags)";
    }

    public static string ProductVersionSummary()
        => $"{ShortName} {AppVersionLabel()} · ядро {CoreVersionLabel()}";

    private static string? ProbeAdvisorVersion(string exe)
    {
        if (!File.Exists(exe)) return null;
        try
        {
            var psi = new ProcessStartInfo(exe, "--version")
            {
                UseShellExecute = false,
                CreateNoWindow = true,
                RedirectStandardOutput = true,
                RedirectStandardError = true
            };
            using var p = Process.Start(psi);
            if (p == null) return null;
            if (!p.WaitForExit(2500))
            {
                try { p.Kill(entireProcessTree: true); } catch { /* ignore */ }
                return null;
            }
            var output = (p.StandardOutput.ReadToEnd() + p.StandardError.ReadToEnd()).Trim();
            if (string.IsNullOrWhiteSpace(output)) return null;
            // cobra prints "advisor version X" or just "X"
            const string prefix = "version ";
            var idx = output.LastIndexOf(prefix, StringComparison.OrdinalIgnoreCase);
            if (idx >= 0)
                return output[(idx + prefix.Length)..].Trim();
            var first = output.Split('\n', 2)[0].Trim();
            return string.IsNullOrWhiteSpace(first) ? null : first;
        }
        catch
        {
            return null;
        }
    }

    private static string TrimBuildMetadata(string value)
    {
        // AssemblyInformationalVersion may append "+<commit>" from SourceLink; keep it readable.
        var cut = value.IndexOf('+');
        return cut > 0 ? value[..cut] : value;
    }
}
