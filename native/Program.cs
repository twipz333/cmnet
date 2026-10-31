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
        // Optional Gate1 honesty PNGs (partial/stale/offline + draft states). Off by
        // default so CI --capture-ui stays inside the 180s WaitForExit budget.
        var captureHonesty = Array.Exists(args, a => a == "--capture-honesty");
        // Gate 2.1 Data surface PNGs — also outside geometry.json (Count stays 33).
        var captureData = Array.Exists(args, a => a == "--capture-data");
        // Fast path: Data honesty kit only (skips Draft/Scout matrix). geometry.json
        // is still written with Count==0 here; use default --capture-ui for CI 33.
        var captureDataOnly = Array.Exists(args, a => a == "--capture-data-only");
        if (captureDataOnly) captureData = true;
        // Gate 2.2 Settings + first-run PNGs — outside geometry.json (Count stays 33).
        var captureSettings = Array.Exists(args, a => a == "--capture-settings");
        var captureSettingsOnly = Array.Exists(args, a => a == "--capture-settings-only");
        if (captureSettingsOnly) captureSettings = true;
        // Gate 2.3 GSI install + live/readonly Draft chrome — outside geometry.json.
        var captureGsi = Array.Exists(args, a => a == "--capture-gsi");
        var captureGsiOnly = Array.Exists(args, a => a == "--capture-gsi-only");
        if (captureGsiOnly) captureGsi = true;
        // Gate 2.4 After-match history — outside geometry.json (Count stays 33).
        var captureHistory = Array.Exists(args, a => a == "--capture-history");
        var captureHistoryOnly = Array.Exists(args, a => a == "--capture-history-only");
        if (captureHistoryOnly) captureHistory = true;
        // Gate 2.5 Friends local file exchange — outside geometry.json (Count stays 33).
        var captureFriends = Array.Exists(args, a => a == "--capture-friends");
        var captureFriendsOnly = Array.Exists(args, a => a == "--capture-friends-only");
        if (captureFriendsOnly) captureFriends = true;
        // Gate 2.6 Electron→native migration — outside geometry.json (Count stays 33).
        var captureMigration = Array.Exists(args, a => a == "--capture-migration");
        var captureMigrationOnly = Array.Exists(args, a => a == "--capture-migration-only");
        if (captureMigrationOnly) captureMigration = true;
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
            var destination = capture + 1 < args.Length && !args[capture + 1].StartsWith("--", StringComparison.Ordinal)
                ? args[capture + 1]
                : Path.Combine(Environment.CurrentDirectory, "native", "ui-snapshots");
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
                if (!captureDataOnly && !captureSettingsOnly && !captureGsiOnly && !captureHistoryOnly && !captureFriendsOnly && !captureMigrationOnly)
                {
                    window.AssertPreviewInteractions();
                    foreach (var screen in new[] { "Разведка", "Драфт" })
                    {
                        window.FreezePreviewCatalogColumns(false);
                        window.LoadVisualFixture(screen);
                        window.UpdateLayout();
                        // One CDN/cache fill per screen; size/scale variants only settle.
                        // Budgets tightened vs Gate1 (25/15): windows-latest still timed
                        // out mid-Draft matrix on Gate 2.1 when gallery rebound on resize.
                        window.WarmPreviewArtwork(screen == "Драфт" ? 15 : 10);
                        window.FreezePreviewCatalogColumns(true);
                        var prefix = screen == "Разведка" ? "scout" : "draft";
                        foreach (var size in new[] { (1920,1080), (1600,900), (1440,900), (1366,768), (1280,720) })
                        foreach (var scale in new[] { 1d, 1.25d, 1.5d })
                        {
                            var filename = $"{prefix}-{size.Item1}x{size.Item2}-layout{scale * 100:0}.png";
                            reports.Add(window.CaptureVisualPreview(
                                Path.Combine(destination, filename), size.Item1, size.Item2, scale,
                                artworkWarmSeconds: 0));
                            if (size == (1440,900) && scale == 1) File.Copy(Path.Combine(destination, filename), Path.Combine(destination, prefix + ".png"), true);
                        }
                    }
                    window.FreezePreviewCatalogColumns(false);
                    // Historical geometry matrix keeps scout empty/loading/error (33 entries total).
                    // Artwork already cached from the Scout pass — do not burn another 10s×3.
                    foreach (var scenario in new[] { "empty", "loading", "error" })
                    {
                        window.LoadPreviewScenario(scenario);
                        reports.Add(window.CaptureVisualPreview(
                            Path.Combine(destination, "scout-" + scenario + ".png"),
                            artworkWarmSeconds: 0));
                    }
                    // Gate1 close-out honesty PNGs are opt-in (--capture-honesty). They are
                    // review evidence only and must NOT enter geometry.json (CI Count==33).
                    // Default CI path omits them so WarmPreviewArtwork on Draft states cannot
                    // push total wall clock past the 180s WaitForExit in native.yml.
                    if (captureHonesty)
                    {
                        foreach (var scenario in new[] { "partial", "stale", "offline" })
                        {
                            window.LoadPreviewScenario(scenario);
                            window.CaptureVisualPreview(
                                Path.Combine(destination, "scout-" + scenario + ".png"),
                                artworkWarmSeconds: 10);
                        }
                        foreach (var scenario in new[] { "empty", "loading", "error", "partial", "stale", "offline" })
                        {
                            window.LoadDraftPreviewScenario(scenario);
                            window.CaptureVisualPreview(
                                Path.Combine(destination, "draft-" + scenario + ".png"),
                                artworkWarmSeconds: 15);
                        }
                    }
                }
                // Gate 2.1 Data — review evidence only; must NOT enter geometry.json.
                if (captureData)
                {
                    foreach (var scenario in new[] { "ready", "empty", "loading", "error", "partial", "stale", "offline" })
                    {
                        window.LoadDataPreviewScenario(scenario);
                        window.CaptureVisualPreview(
                            Path.Combine(destination, "data-" + scenario + ".png"),
                            artworkWarmSeconds: 0);
                        if (scenario == "ready")
                        {
                            File.Copy(
                                Path.Combine(destination, "data-ready.png"),
                                Path.Combine(destination, "data.png"), true);
                        }
                    }
                }
                // Gate 2.2 Settings + welcome — review evidence only; must NOT enter geometry.json.
                if (captureSettings)
                {
                    foreach (var scenario in new[] { "ready", "empty", "loading", "error", "offline", "about", "deferred", "welcome" })
                    {
                        window.LoadSettingsPreviewScenario(scenario);
                        window.CaptureVisualPreview(
                            Path.Combine(destination, "settings-" + scenario + ".png"),
                            artworkWarmSeconds: 0);
                        if (scenario == "ready")
                        {
                            File.Copy(
                                Path.Combine(destination, "settings-ready.png"),
                                Path.Combine(destination, "settings.png"), true);
                        }
                    }
                }
                // Gate 2.3 GSI — review evidence only; must NOT enter geometry.json.
                if (captureGsi)
                {
                    foreach (var scenario in new[] { "missing", "installed", "waiting", "live", "offline" })
                    {
                        window.LoadGsiPreviewScenario(scenario);
                        window.CaptureVisualPreview(
                            Path.Combine(destination, "gsi-" + scenario + ".png"),
                            artworkWarmSeconds: 0);
                        if (scenario == "installed")
                        {
                            File.Copy(
                                Path.Combine(destination, "gsi-installed.png"),
                                Path.Combine(destination, "gsi.png"), true);
                        }
                    }
                    foreach (var scenario in new[] { "draft-manual", "draft-waiting", "draft-live" })
                    {
                        window.LoadGsiPreviewScenario(scenario);
                        window.CaptureVisualPreview(
                            Path.Combine(destination, "gsi-" + scenario + ".png"),
                            artworkWarmSeconds: 0);
                    }
                }
                // Gate 2.4 History — review evidence only; must NOT enter geometry.json.
                if (captureHistory)
                {
                    foreach (var scenario in new[] { "ready", "empty", "partial", "loading", "error", "offline", "detail" })
                    {
                        window.LoadHistoryPreviewScenario(scenario);
                        window.CaptureVisualPreview(
                            Path.Combine(destination, "history-" + scenario + ".png"),
                            artworkWarmSeconds: 0);
                        if (scenario == "ready")
                        {
                            File.Copy(
                                Path.Combine(destination, "history-ready.png"),
                                Path.Combine(destination, "history.png"), true);
                        }
                    }
                }
                // Gate 2.5 Friends — review evidence only; must NOT enter geometry.json.
                if (captureFriends)
                {
                    foreach (var scenario in new[] { "ready", "empty", "partial", "loading", "error", "offline", "imported" })
                    {
                        window.LoadFriendsPreviewScenario(scenario);
                        window.CaptureVisualPreview(
                            Path.Combine(destination, "friends-" + scenario + ".png"),
                            artworkWarmSeconds: 0);
                        if (scenario == "ready")
                        {
                            File.Copy(
                                Path.Combine(destination, "friends-ready.png"),
                                Path.Combine(destination, "friends.png"), true);
                        }
                    }
                }
                // Gate 2.6 Migration — review evidence only; must NOT enter geometry.json.
                if (captureMigration)
                {
                    foreach (var scenario in new[] { "ready", "empty", "partial", "error", "offline" })
                    {
                        window.LoadMigratePreviewScenario(scenario);
                        window.CaptureVisualPreview(
                            Path.Combine(destination, "migration-" + scenario + ".png"),
                            artworkWarmSeconds: 0);
                        if (scenario == "ready")
                        {
                            File.Copy(
                                Path.Combine(destination, "migration-ready.png"),
                                Path.Combine(destination, "migration.png"), true);
                        }
                    }
                }
                // Data/Settings/GSI/History/Friends/Migration-only must not wipe a prior CI matrix geometry.json (Count==33).
                if (!captureDataOnly && !captureSettingsOnly && !captureGsiOnly && !captureHistoryOnly && !captureFriendsOnly && !captureMigrationOnly)
                {
                    File.WriteAllText(Path.Combine(destination, "geometry.json"), reports.ToJsonString(new JsonSerializerOptions { WriteIndented = true }));
                }
                var checks = captureMigrationOnly
                    ? "Gate 2.6 Migration-only capture (--capture-migration-only). Left geometry.json untouched; CI matrix requires default --capture-ui (Count==33)."
                    : captureFriendsOnly
                    ? "Gate 2.5 Friends-only capture (--capture-friends-only). Left geometry.json untouched; CI matrix requires default --capture-ui (Count==33)."
                    : captureHistoryOnly
                    ? "Gate 2.4 History-only capture (--capture-history-only). Left geometry.json untouched; CI matrix requires default --capture-ui (Count==33)."
                    : captureGsiOnly
                    ? "Gate 2.3 GSI-only capture (--capture-gsi-only). Left geometry.json untouched; CI matrix requires default --capture-ui (Count==33)."
                    : captureSettingsOnly
                    ? "Gate 2.2 Settings-only capture (--capture-settings-only). Left geometry.json untouched; CI matrix requires default --capture-ui (Count==33)."
                    : captureDataOnly
                    ? "Gate 2.1 Data-only capture (--capture-data-only). Left geometry.json untouched; CI matrix requires default --capture-ui (Count==33)."
                    : "PASS: hero/player selection, retained draft selection, taken hero, model readiness, three columns and Apply viewport containment.\nDPI cases simulate logical viewport/raster scaling; native monitor DPI requires separate validation.\nActual WPF SAMPLE DATA fixtures; no Go process or user database.\ngeometry.json Count==33 (Draft×5×3 + Scout×5×3 + scout empty/loading/error).";
                if (captureHonesty && !captureDataOnly && !captureSettingsOnly && !captureGsiOnly && !captureHistoryOnly && !captureFriendsOnly && !captureMigrationOnly)
                    checks += "\nGate1 honesty kit (--capture-honesty): scout partial/stale/offline + draft empty/loading/error/partial/stale/offline (outside geometry.json).";
                if (captureData)
                    checks += "\nGate 2.1 Data kit (--capture-data): data ready/empty/loading/error/partial/stale/offline (outside geometry.json).";
                if (captureSettings)
                    checks += "\nGate 2.2 Settings kit (--capture-settings): settings ready/empty/loading/error/offline/about/deferred/welcome (outside geometry.json).";
                if (captureGsi)
                    checks += "\nGate 2.3 GSI kit (--capture-gsi): settings missing/installed/waiting/live/offline + draft-manual/waiting/live (outside geometry.json).";
                if (captureHistory)
                    checks += "\nGate 2.4 History kit (--capture-history): history ready/empty/partial/loading/error/offline/detail (outside geometry.json).";
                if (captureFriends)
                    checks += "\nGate 2.5 Friends kit (--capture-friends): friends ready/empty/partial/loading/error/offline/imported (outside geometry.json).";
                if (captureMigration)
                    checks += "\nGate 2.6 Migration kit (--capture-migration): migration ready/empty/partial/error/offline (outside geometry.json).";
                File.WriteAllText(Path.Combine(destination, "checks.txt"), checks);
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

