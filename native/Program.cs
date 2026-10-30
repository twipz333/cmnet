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
            window.Width = 1440;
            window.Height = 900;
            window.WindowStartupLocation = WindowStartupLocation.Manual;
            window.Left = 0;
            window.Top = 0;
            window.Show();
            try
            {
                foreach (var screen in new[] { "Разведка", "Драфт" })
                {
                    window.LoadVisualFixture(screen);
                    window.UpdateLayout();
                    window.Dispatcher.Invoke(() => { },
                        System.Windows.Threading.DispatcherPriority.ApplicationIdle);
                    window.CaptureVisualPreview(Path.Combine(destination,
                        screen == "Разведка" ? "scout.png" : "draft.png"));
                }
            }
            finally
            {
                window.Close();
                app.Shutdown();
            }
            return;
        }
        app.Run(new NativeWindow(dataDir));
    }
}

