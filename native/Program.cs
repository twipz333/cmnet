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
        var app = new Application
        {
            ShutdownMode = smoke ? ShutdownMode.OnExplicitShutdown : ShutdownMode.OnMainWindowClose
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
        app.Run(new NativeWindow(dataDir));
    }
}

