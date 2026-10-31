using System;
using System.IO;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Threading;

namespace Cmnet.Native;

/// <summary>
/// Gate 3.4 — write redacted crash breadcrumbs under %APPDATA%\cmnet\diagnostics.
/// No paid crash SaaS. Failures while writing must never re-enter the crash loop.
/// </summary>
internal static class CrashBreadcrumbs
{
    private static int installed;
    private static int writing;

    public static void InstallHandlers()
    {
        if (Interlocked.Exchange(ref installed, 1) == 1) return;
        try
        {
            DiagnosticsPaths.EnsureLayout();
        }
        catch
        {
            // Best-effort only.
        }

        AppDomain.CurrentDomain.UnhandledException += (_, e) =>
        {
            try
            {
                Write("AppDomain.UnhandledException", e.ExceptionObject as Exception
                    ?? new Exception(e.ExceptionObject?.ToString() ?? "unknown"),
                    isTerminating: e.IsTerminating);
            }
            catch { /* never throw from crash path */ }
        };

        TaskScheduler.UnobservedTaskException += (_, e) =>
        {
            try
            {
                Write("TaskScheduler.UnobservedTaskException", e.Exception, isTerminating: false);
                e.SetObserved();
            }
            catch { /* never throw from crash path */ }
        };
    }

    /// <summary>
    /// Hook WPF dispatcher after Application exists.
    /// </summary>
    public static void InstallDispatcher(Application app)
    {
        if (app == null) return;
        app.DispatcherUnhandledException += OnDispatcherUnhandled;
    }

    private static void OnDispatcherUnhandled(object sender, DispatcherUnhandledExceptionEventArgs e)
    {
        try
        {
            Write("DispatcherUnhandledException", e.Exception, isTerminating: false);
        }
        catch { /* swallow */ }
        // Leave e.Handled = false so default shutdown still occurs for fatal UI faults;
        // breadcrumb is already on disk.
    }

    public static string Write(string source, Exception? ex, bool isTerminating)
    {
        if (Interlocked.CompareExchange(ref writing, 1, 0) != 0)
            return ""; // nested fault while writing — bail
        try
        {
            DiagnosticsPaths.EnsureLayout();
            var stamp = DateTime.UtcNow.ToString("yyyyMMdd-HHmmss-fff");
            var path = Path.Combine(DiagnosticsPaths.CrashesDir(), $"crash-{stamp}.txt");
            var raw = BuildRawReport(source, ex, isTerminating);
            var safe = SecretRedactor.Redact(raw);
            // Atomic-ish: write temp then move.
            var tmp = path + ".tmp";
            File.WriteAllText(tmp, safe, new UTF8Encoding(encoderShouldEmitUTF8Identifier: false));
            File.Move(tmp, path, overwrite: true);
            return path;
        }
        catch
        {
            return "";
        }
        finally
        {
            Interlocked.Exchange(ref writing, 0);
        }
    }

    /// <summary>
    /// Manual diagnostic note (Settings / Help). Always redacted.
    /// </summary>
    public static string WriteNote(string title, string body)
    {
        try
        {
            DiagnosticsPaths.EnsureLayout();
            var stamp = DateTime.UtcNow.ToString("yyyyMMdd-HHmmss-fff");
            var path = Path.Combine(DiagnosticsPaths.Root(), $"note-{stamp}.txt");
            var raw = new StringBuilder();
            raw.AppendLine("CMNET diagnostic note");
            raw.AppendLine("utc=" + DateTime.UtcNow.ToString("o"));
            raw.AppendLine("title=" + title);
            raw.AppendLine("---");
            raw.AppendLine(body ?? "");
            File.WriteAllText(path, SecretRedactor.Redact(raw.ToString()),
                new UTF8Encoding(encoderShouldEmitUTF8Identifier: false));
            return path;
        }
        catch
        {
            return "";
        }
    }

    private static string BuildRawReport(string source, Exception? ex, bool isTerminating)
    {
        var sb = new StringBuilder(4096);
        sb.AppendLine("CMNET crash breadcrumb (local-only, Gate 3.4)");
        sb.AppendLine("utc=" + DateTime.UtcNow.ToString("o"));
        sb.AppendLine("source=" + source);
        sb.AppendLine("terminating=" + isTerminating);
        sb.AppendLine("app=" + ProductIdentity.ShortName + " " + ProductIdentity.AppVersionLabel());
        sb.AppendLine("core=" + ProductIdentity.CoreVersionLabel());
        sb.AppendLine("os=" + Environment.OSVersion);
        sb.AppendLine("runtime=" + Environment.Version);
        sb.AppendLine("baseDir=" + AppContext.BaseDirectory);
        sb.AppendLine("dataHint=%APPDATA%\\cmnet (path omitted if it embeds secrets)");
        sb.AppendLine("---");
        if (ex == null)
        {
            sb.AppendLine("(no exception object)");
        }
        else
        {
            sb.AppendLine(ex.ToString());
        }
        sb.AppendLine("---");
        sb.AppendLine(SecretRedactor.DescribeRules());
        return sb.ToString();
    }
}
