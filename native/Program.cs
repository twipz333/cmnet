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
        var app = new Application { ShutdownMode = ShutdownMode.OnMainWindowClose };
        app.Resources.MergedDictionaries.Add(new ResourceDictionary
        {
            Source = new Uri("pack://application:,,,/Theme.xaml", UriKind.Absolute)
        });
        app.Run(new NativeWindow(dataDir));
    }
}

// Reuses EXACTLY the existing Go state API and existing cmnet data folder.
// There is no WebView2, Chromium or browser embedding in this process.
public sealed class AdvisorCore : IAsyncDisposable
{
    private readonly HttpClient http = new() { Timeout = TimeSpan.FromSeconds(15) };
    private Process? child;
    private CancellationTokenSource? streamCancellation;
    private Task? streamTask;
    public Uri? BaseUri { get; private set; }
    public event Action<JsonObject>? StateReceived;
    public event Action<string>? ConnectionChanged;

    public string DataDirectory { get; private set; } = FindDataDirectory();

    public static string FindDataDirectory()
    {
        var appData = Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData);
        foreach (var dir in new[] { "cmnet", "cmnet-desktop" })
        {
            var root = Path.Combine(appData, dir);
            var settingsPath = Path.Combine(root, "desktop.json");
            if (File.Exists(settingsPath))
            {
                try
                {
                    var json = JsonNode.Parse(File.ReadAllText(settingsPath));
                    var selected = json?["dataDir"]?.GetValue<string>();
                    if (!string.IsNullOrWhiteSpace(selected) && Directory.Exists(selected))
                        return selected;
                }
                catch (Exception) { /* Bad settings must not lose a valid data folder. */ }
            }
            var data = Path.Combine(root, "data");
            if (File.Exists(Path.Combine(data, "cmnet.db")) || File.Exists(Path.Combine(data, "bundle.json")))
                return data;
        }
        return Path.Combine(appData, "cmnet", "data");
    }

    public async Task StartAsync(string? dataDirectory = null)
    {
        if (child is { HasExited: false })
            throw new InvalidOperationException("Go core is already running");
        DataDirectory = dataDirectory ?? DataDirectory;
        Directory.CreateDirectory(DataDirectory);
        var executable = Path.Combine(AppContext.BaseDirectory, "advisor.exe");
        if (!File.Exists(executable))
            throw new FileNotFoundException("Missing advisor.exe next to Cmnet.Native.exe. Install the complete native build.", executable);
        int port;
        using (var listener = new TcpListener(IPAddress.Loopback, 0))
        {
            listener.Start();
            port = ((IPEndPoint)listener.LocalEndpoint).Port;
        }
        BaseUri = new Uri($"http://127.0.0.1:{port}/");
        child = Process.Start(new ProcessStartInfo(executable)
        {
            Arguments = $"--web-addr 127.0.0.1:{port} --open=false --discord=false",
            WorkingDirectory = DataDirectory,
            UseShellExecute = false,
            CreateNoWindow = true,
        }) ?? throw new InvalidOperationException("Cannot start advisor.exe");
        ConnectionChanged?.Invoke("Запуск локального Go-ядра…");
        using var deadline = new CancellationTokenSource(TimeSpan.FromSeconds(35));
        while (!deadline.IsCancellationRequested)
        {
            if (child.HasExited)
                throw new InvalidOperationException($"advisor.exe exited: {child.ExitCode}");
            try
            {
                using var resp = await http.GetAsync(new Uri(BaseUri, "api/state"), deadline.Token);
                resp.EnsureSuccessStatusCode();
                break;
            }
            catch (Exception ex) when (ex is HttpRequestException or TaskCanceledException)
            {
                try { await Task.Delay(250, deadline.Token); } catch (OperationCanceledException) { break; }
            }
        }
        if (deadline.IsCancellationRequested)
            throw new TimeoutException("Go core did not answer within 35 seconds");
        streamCancellation = new CancellationTokenSource();
        streamTask = Task.Run(() => ReadEventsAsync(streamCancellation.Token));
    }

    public async Task<JsonObject> GetAsync(string path, CancellationToken ct = default)
    {
        using var response = await http.GetAsync(new Uri(BaseUri ?? throw new InvalidOperationException("Core is not ready"), path.TrimStart('/')), ct);
        response.EnsureSuccessStatusCode();
        return JsonNode.Parse(await response.Content.ReadAsStringAsync(ct)) as JsonObject ?? new JsonObject();
    }

    public async Task<string> PostAsync(string path, object request, CancellationToken ct = default)
    {
        var json = JsonSerializer.Serialize(request);
        using var resp = await http.PostAsync(new Uri(BaseUri ?? throw new InvalidOperationException("Core is not ready"), path.TrimStart('/')),
            new StringContent(json, Encoding.UTF8, "application/json"), ct);
        var body = await resp.Content.ReadAsStringAsync(ct);
        if (!resp.IsSuccessStatusCode)
            throw new HttpRequestException($"{(int)resp.StatusCode}: {body}");
        // The Go API returns the entire current state on POST.
        try
        {
            var node = JsonNode.Parse(body) as JsonObject;
            if (node?.ContainsKey("meta") == true) StateReceived?.Invoke(node);
        }
        catch (JsonException) { /* Some endpoints return an acknowledgment only. */ }
        return body;
    }

    private async Task ReadEventsAsync(CancellationToken ct)
    {
        while (!ct.IsCancellationRequested)
        {
            try
            {
                using var req = new HttpRequestMessage(HttpMethod.Get, new Uri(BaseUri!, "api/events"));
                using var resp = await http.SendAsync(req, HttpCompletionOption.ResponseHeadersRead, ct);
                resp.EnsureSuccessStatusCode();
                ConnectionChanged?.Invoke("Go-ядро подключено");
                await using var stream = await resp.Content.ReadAsStreamAsync(ct);
                using var reader = new StreamReader(stream);
                while (!ct.IsCancellationRequested)
                {
                    var line = await reader.ReadLineAsync(ct);
                    if (line is null) break;
                    if (!line.StartsWith("data: ", StringComparison.Ordinal)) continue;
                    if (JsonNode.Parse(line.AsSpan(6).ToString()) is JsonObject state)
                        StateReceived?.Invoke(state);
                }
            }
            catch (OperationCanceledException) when (ct.IsCancellationRequested) { break; }
            catch (Exception ex) { ConnectionChanged?.Invoke("Соединение потеряно: " + ex.Message); }
            try { await Task.Delay(1250, ct); } catch (OperationCanceledException) { break; }
        }
    }

    // Window shutdown must kill the child synchronously: an async fire-and-forget
    // cleanup can be abandoned when the WPF process exits.
    public void Stop()
    {
        streamCancellation?.Cancel();
        if (child is { HasExited: false })
        {
            try { child.Kill(entireProcessTree: true); } catch (Exception) { }
        }
    }

    public async ValueTask DisposeAsync()
    {
        Stop();
        if (streamTask != null)
        {
            try { await streamTask.WaitAsync(TimeSpan.FromSeconds(2)); } catch (Exception) { }
        }
        streamCancellation?.Dispose();
        if (child is { HasExited: false })
        {
            try { child.Kill(entireProcessTree: true); } catch (Exception) { }
            try { await child.WaitForExitAsync().WaitAsync(TimeSpan.FromSeconds(3)); } catch (Exception) { }
        }
        child?.Dispose();
        http.Dispose();
    }
}

public static class J
{
    public static JsonNode? At(this JsonNode? n, params string[] keys)
    {
        foreach (var key in keys)
        {
            if (n is not JsonObject obj || !obj.TryGetPropertyValue(key, out n))
                return null;
        }
        return n;
    }
    public static string S(this JsonNode? n, string fallback = "")
    {
        try { return n?.ToString() ?? fallback; } catch { return fallback; }
    }
    public static bool B(this JsonNode? n)
    {
        try { return n?.GetValue<bool>() ?? false; } catch { return false; }
    }
    public static int I(this JsonNode? n)
    {
        try { return n?.GetValue<int>() ?? 0; } catch { return 0; }
    }
    public static long L(this JsonNode? n)
    {
        try { return n?.GetValue<long>() ?? 0; } catch { return 0; }
    }
    public static double D(this JsonNode? n)
    {
        try { return n?.GetValue<double>() ?? 0; } catch { return 0; }
    }
    public static IEnumerable<JsonNode?> Items(this JsonNode? n) => (n as JsonArray) ?? new JsonArray();
}
