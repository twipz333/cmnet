using System;
using System.Collections.Concurrent;
using System.IO;
using System.Net.Http;
using System.Text.RegularExpressions;
using System.Threading;
using System.Threading.Tasks;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Media.Imaging;

namespace Cmnet.Native;

/// <summary>
/// Loads Valve's current hero artwork once into the SAME assets/heroes cache
/// used by the existing Go application. The UI never blocks on the network,
/// and cached portraits work with no internet connection.
/// </summary>
internal sealed class HeroArtwork
{
    private readonly Func<string> dataDirectory;
    private readonly ConcurrentDictionary<int, Task<ImageSource?>> inflight = new();
    private static readonly HttpClient http = new() { Timeout = TimeSpan.FromSeconds(12) };
    private static readonly SemaphoreSlim parallel = new(4, 4);
    private static readonly Regex internalHeroName = new(
        "^npc_dota_hero_([a-z0-9_]+)$", RegexOptions.Compiled | RegexOptions.CultureInvariant);

    public HeroArtwork(Func<string> path) => dataDirectory = path;

    public void Bind(Image control, int heroId, string internalName)
    {
        if (heroId <= 0) return;
        // An existing disk cache should paint on the first WPF frame.
        // Network loading is only the fallback and never blocks the UI.
        var cached = TryReadDiskCache(heroId);
        if (cached != null)
        {
            control.Source = cached;
            return;
        }
        _ = BindAsync(control, heroId, internalName);
    }

    private ImageSource? TryReadDiskCache(int id)
    {
        try
        {
            var root = dataDirectory();
            var userCache = string.IsNullOrWhiteSpace(root) ? null :
                Path.Combine(root, "assets", "heroes", id + ".png");
            if (userCache != null && File.Exists(userCache))
            {
                var result = TryDecode(File.ReadAllBytes(userCache));
                if (result != null) return result;
            }
            // For legally sourced, user-provided local portrait packs.
            var local = Path.Combine(AppContext.BaseDirectory, "assets", "heroes", id + ".png");
            return File.Exists(local) ? TryDecode(File.ReadAllBytes(local)) : null;
        }
        catch (IOException) { return null; }
        catch (UnauthorizedAccessException) { return null; }
    }

    private async Task BindAsync(Image target, int id, string name)
    {
        try
        {
            var art = await inflight.GetOrAdd(id, _ => LoadAsync(id, name));
            if (art != null && target.Dispatcher is { HasShutdownStarted: false })
                await target.Dispatcher.InvokeAsync(() => target.Source = art);
        }
        catch (Exception)
        {
            // Missing new-hero artwork is nonfatal; text labels remain visible.
        }
    }

    private async Task<ImageSource?> LoadAsync(int id, string name)
    {
        var dir = Path.Combine(dataDirectory(), "assets", "heroes");
        var target = Path.Combine(dir, $"{id}.png");
        if (File.Exists(target))
        {
            var cached = TryDecode(await File.ReadAllBytesAsync(target));
            if (cached != null) return cached;
        }

        var match = internalHeroName.Match(name);
        if (!match.Success) return null;

        await parallel.WaitAsync();
        try
        {
            if (File.Exists(target))
            {
                var cached = TryDecode(await File.ReadAllBytesAsync(target));
                if (cached != null) return cached;
            }
            // Never compose a URL from a user-supplied name or arbitrary host.
            var uri = new Uri("https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/heroes/"
                              + match.Groups[1].Value + ".png");
            using var response = await http.GetAsync(uri, HttpCompletionOption.ResponseHeadersRead);
            response.EnsureSuccessStatusCode();
            if (response.Content.Headers.ContentLength > 2_000_000) return null;
            var bytes = await response.Content.ReadAsByteArrayAsync();
            if (bytes.Length > 2_000_000) return null;
            var bitmap = TryDecode(bytes);
            if (bitmap == null) return null;
            Directory.CreateDirectory(dir);
            var temporary = target + "." + Guid.NewGuid().ToString("N") + ".tmp";
            try
            {
                await File.WriteAllBytesAsync(temporary, bytes);
                File.Move(temporary, target, true);
            }
            finally
            {
                if (File.Exists(temporary)) File.Delete(temporary);
            }
            return bitmap;
        }
        catch (Exception)
        {
            return null;
        }
        finally { parallel.Release(); }
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
            image.StreamSource = stream;
            image.EndInit();
            image.Freeze();
            return image;
        }
        catch (Exception) { return null; }
    }
}
