using System;
using System.Diagnostics;
using System.IO;
using System.Net;
using System.Net.Http;
using System.Net.Sockets;
using System.Text;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;

namespace Cmnet.Native;

public static class Program
{
    [STAThread]
    public static void Main(string[] args)
    {
        // Explicit override for users whose Electron data folder is elsewhere.
        // The default still reads the existing Electron desktop.json dataDir.
        string? dataDir = null;
        if (args.Length == 2 && args[0] == "--data-dir")
            dataDir = args[1];
        var smoke = Array.Exists(args, a => a == "--smoke-ui");
        var capture = Array.IndexOf(args, "--capture-ui");
        var app = new Application
        {
            ShutdownMode = smoke || capture >= 0 ? ShutdownMode.OnExplicitShutdown : ShutdownMode.OnMainWindowClose
        };
        app.Resources.MergedDictionaries.Add(new ResourceDictionary
        {
            Source = new Uri("pack://application:,,,/Theme.xaml", UriKind.Absolute)
        });
        if (smoke)
        {
            // No Show(), no Go process, no local user data. Exercise page
            // construction on the Windows STA thread and fail CI on exceptions.
            CoreSnapshotGate.SmokeTest();
            new NativeWindow().AssertNavigationWorks();
            app.Shutdown();
            return;
        }
        if (capture >= 0)
        {
            var destination = capture + 1 < args.Length
                ? args[capture + 1] : Path.Combine(Environment.CurrentDirectory, "native", "ui-snapshots");
            Directory.CreateDirectory(destination);
            var window = new NativeWindow(null, visualPreview: true);
            window.WindowStyle = WindowStyle.None;
            window.ResizeMode = ResizeMode.NoResize;
            window.MinWidth = 0; window.MinHeight = 0;
            window.Width = 1440; window.Height = 900;
            window.WindowStartupLocation = WindowStartupLocation.Manual;
            window.Left = 0; window.Top = 0;
            window.Show();
            var reports = new JsonArray();
            try
            {
                window.AssertPreviewInteractions();
                foreach (var screen in new[] { "Разведка", "Драфт" })
                {
                    window.LoadVisualFixture(screen);
                    window.UpdateLayout();
                    window.WarmPreviewArtwork();
                    var prefix = screen == "Разведка" ? "scout" : "draft";
                    foreach (var size in new[] { (1920,1080), (1600,900), (1440,900), (1366,768), (1280,720) })
                    foreach (var scale in new[] { 1d, 1.25d, 1.5d })
                    {
                        var filename = $"{prefix}-{size.Item1}x{size.Item2}-layout{scale * 100:0}.png";
                        reports.Add(window.CaptureVisualPreview(Path.Combine(destination, filename), size.Item1, size.Item2, scale));
                        if (size == (1440,900) && scale == 1) File.Copy(Path.Combine(destination, filename), Path.Combine(destination, prefix + ".png"), true);
                    }
                }
                foreach (var scenario in new[] { "empty", "loading", "error" })
                {
                    window.LoadPreviewScenario(scenario);
                    reports.Add(window.CaptureVisualPreview(Path.Combine(destination, "scout-" + scenario + ".png")));
                }
                // Draft honesty fixtures for Gate1 review evidence only. Do NOT add them to
                // geometry.json — native.yml requires the historical 33-entry matrix
                // (Draft×5×3 + Scout×5×3 + scout empty/loading/error).
                foreach (var scenario in new[] { "empty", "loading", "error" })
                {
                    window.LoadDraftPreviewScenario(scenario);
                    window.CaptureVisualPreview(Path.Combine(destination, "draft-" + scenario + ".png"));
                }
                File.WriteAllText(Path.Combine(destination, "geometry.json"), reports.ToJsonString(new JsonSerializerOptions { WriteIndented = true }));
                File.WriteAllText(Path.Combine(destination, "checks.txt"), "PASS: hero/player selection, retained draft selection, taken hero, model readiness, three columns and Apply viewport containment.\nDPI cases simulate logical viewport/raster scaling; native monitor DPI requires separate validation.\nActual WPF SAMPLE DATA fixtures; no Go process or user database.");
            }
            catch (Exception ex)
            {
                File.WriteAllText(Path.Combine(destination, "capture-error.txt"), ex.ToString());
                Environment.ExitCode = 1;
            }
            finally
            {
                window.Close();
                app.Shutdown(Environment.ExitCode);
            }
            return;
        }
        app.Run(new NativeWindow(dataDir));
    }
}

