using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Net.Http;
using System.Runtime.CompilerServices;
using System.Text.RegularExpressions;
using System.Threading;
using System.Threading.Tasks;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Media.Imaging;

namespace Cmnet.Native;

/// <summary>Asynchronous Valve portraits, sharing the Go assets/heroes disk cache.</summary>
internal sealed class HeroArtwork
{
    private const int MemoryCapacity = 192;
    private const int MaximumBytes = 2_000_000;
    private readonly Func<string> dataDirectory;
    private readonly object gate = new();
    private int bindingCount;
    private readonly Dictionary<string, Task<ImageSource?>> inflight = new();
    private readonly Dictionary<string, ImageSource> memory = new();
    private readonly LinkedList<string> recency = new();
    private readonly ConditionalWeakTable<Image, Binding> bindings = new();
    private static readonly HttpClient http = new() { Timeout = TimeSpan.FromSeconds(12) };
    private static readonly SemaphoreSlim parallel = new(4, 4);
    private static readonly Regex internalHeroName = new(
        "^npc_dota_hero_([a-z0-9_]+)$", RegexOptions.Compiled | RegexOptions.CultureInvariant);
    private sealed class Binding { public long Version; }

    public HeroArtwork(Func<string> path) => dataDirectory = path;

    public void Bind(Image control, int heroId, string internalName)
    {
        var binding = bindings.GetValue(control, _ => new Binding());
        var version = ++binding.Version;
        control.Source = null;
        if (heroId <= 0) return;
        // Capture the application setting on its owning thread. No disk I/O or
        // bitmap decoding takes place on WPF's dispatcher, including cache hits.
        var root = dataDirectory();
        var key = Path.Combine(root, "assets", "heroes", heroId + ".png");
        lock (gate)
        {
            if (memory.TryGetValue(key, out var cached))
            {
                recency.Remove(key);
                recency.AddLast(key);
                control.Source = cached;
                return;
            }
        }
        Interlocked.Increment(ref bindingCount);
        _ = BindAsync(control, binding, version, key, root, heroId, internalName);
    }

    // Await on the dispatcher, then allow a dispatcher render pass before capturing.
    // Includes assignment to controls, not merely completion of HTTP requests.
    public async Task WaitForIdleAsync()
    {
        while (true)
        {
            Task<ImageSource?>[] pending;
            lock (gate) pending = inflight.Values.ToArray();
            if (pending.Length == 0 && Volatile.Read(ref bindingCount) == 0) return;
            await Task.WhenAll(pending);
            await Task.Delay(10);
        }
    }

    private async Task BindAsync(Image target, Binding binding, long version,
        string key, string root, int id, string name)
    {
        Task<ImageSource?> pending;
        lock (gate)
        {
            if (!inflight.TryGetValue(key, out pending!))
            {
                pending = Task.Run(() => LoadAsync(root, id, name));
                inflight.Add(key, pending);
            }
        }
        try
        {
            var art = await pending;
            lock (gate)
            {
                if (art != null)
                {
                    memory[key] = art;
                    recency.Remove(key);
                    recency.AddLast(key);
                    while (recency.Count > MemoryCapacity)
                    {
                        memory.Remove(recency.First!.Value);
                        recency.RemoveFirst();
                    }
                }
            }
            if (art != null && !target.Dispatcher.HasShutdownStarted)
                await target.Dispatcher.InvokeAsync(() =>
                {
                    // Recycled/rebound controls must never receive an old portrait.
                    if (binding.Version == version) target.Source = art;
                });
        }
        catch (Exception) { /* Text and the view's fallback remain available. */ }
        finally
        {
            lock (gate)
                if (inflight.TryGetValue(key, out var current) && ReferenceEquals(current, pending))
                    inflight.Remove(key);
            Interlocked.Decrement(ref bindingCount);
            // Failures are not memoized: the next view opening can recover online.
        }
    }

    private async Task<ImageSource?> LoadAsync(string root, int id, string name)
    {
        var target = string.IsNullOrWhiteSpace(root) ? null :
            Path.Combine(root, "assets", "heroes", id + ".png");
        var cached = await ReadCachedAsync(target);
        if (cached != null) return cached;
        cached = await ReadCachedAsync(Path.Combine(AppContext.BaseDirectory, "assets", "heroes", id + ".png"));
        if (cached != null) return cached;
        var match = internalHeroName.Match(name);
        if (!match.Success) return null;

        await parallel.WaitAsync();
        try
        {
            for (var attempt = 0; attempt < 2; attempt++)
            {
                try
                {
                    // Only validated Valve hero names are sent to the fixed CDN.
                    var uri = new Uri("https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/heroes/"
                        + match.Groups[1].Value + ".png");
                    using var response = await http.GetAsync(uri, HttpCompletionOption.ResponseHeadersRead);
                    response.EnsureSuccessStatusCode();
                    if (response.Content.Headers.ContentLength > MaximumBytes) return null;
                    using var deadline = new CancellationTokenSource(TimeSpan.FromSeconds(12));
                    await using var source = await response.Content.ReadAsStreamAsync(deadline.Token);
                    using var bytes = new MemoryStream();
                    var buffer = new byte[16_384];
                    int count;
                    while ((count = await source.ReadAsync(buffer, deadline.Token)) > 0)
                    {
                        if (bytes.Length + count > MaximumBytes) return null;
                        bytes.Write(buffer, 0, count);
                    }
                    var data = bytes.ToArray();
                    var bitmap = TryDecode(data);
                    if (bitmap == null) return null;
                    if (target != null) await SaveCacheAsync(target, data);
                    return bitmap;
                }
                catch (HttpRequestException) { }
                catch (OperationCanceledException) { }
                catch (IOException) { }
                if (attempt == 0) await Task.Delay(350);
            }
            return null;
        }
        finally { parallel.Release(); }
    }

    private static async Task<ImageSource?> ReadCachedAsync(string? path)
    {
        try
        {
            if (path == null || !File.Exists(path) || new FileInfo(path).Length > MaximumBytes) return null;
            return TryDecode(await File.ReadAllBytesAsync(path));
        }
        catch (IOException) { return null; }
        catch (UnauthorizedAccessException) { return null; }
    }

    private static async Task SaveCacheAsync(string target, byte[] bytes)
    {
        var temporary = target + "." + Guid.NewGuid().ToString("N") + ".tmp";
        try
        {
            Directory.CreateDirectory(Path.GetDirectoryName(target)!);
            await File.WriteAllBytesAsync(temporary, bytes);
            File.Move(temporary, target, true);
        }
        catch (IOException) { }
        catch (UnauthorizedAccessException) { }
        finally
        {
            try { if (File.Exists(temporary)) File.Delete(temporary); }
            catch (IOException) { }
            catch (UnauthorizedAccessException) { }
        }
        // A read-only cache does not prevent a downloaded image being displayed.
    }

    private static ImageSource? TryDecode(byte[] bytes)
    {
        try
        {
            if (bytes.Length < 8 || bytes[0] != 0x89 || bytes[1] != 0x50 ||
                bytes[2] != 0x4e || bytes[3] != 0x47) return null;
            using var stream = new MemoryStream(bytes);
            var image = new BitmapImage();
            image.BeginInit();
            image.CacheOption = BitmapCacheOption.OnLoad;
            image.DecodePixelWidth = 256;
            image.StreamSource = stream;
            image.EndInit();
            image.Freeze();
            return image;
        }
        catch (Exception) { return null; }
    }
}


