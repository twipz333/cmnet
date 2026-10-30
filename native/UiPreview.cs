using System;
using System.IO;
using System.Collections.Generic;
using System.Windows.Controls;
using System.Windows.Threading;
using System.Linq;
using System.Text.Json.Nodes;
using System.Windows;
using System.Windows.Media;
using System.Windows.Media.Imaging;

namespace Cmnet.Native;

// Captures REAL WPF controls on a Windows runner, not HTML approximations.
// Strictly opt-in via --capture-ui. Never reads or changes user data and never
// starts Go. Screenshots are unmistakably marked with SAMPLE DATA.
public sealed partial class NativeWindow
{
    internal void LoadVisualFixture(string target)
    {
        var heroes = new (int Id, string Name, string Internal)[]
        {
            (1, "Anti-Mage", "antimage"),
            (2, "Axe", "axe"),
            (3, "Bane", "bane"),
            (4, "Bloodseeker", "bloodseeker"),
            (5, "Crystal Maiden", "crystal_maiden"),
            (6, "Drow Ranger", "drow_ranger"),
            (7, "Earthshaker", "earthshaker"),
            (8, "Juggernaut", "juggernaut"),
            (9, "Mirana", "mirana"),
            (10, "Morphling", "morphling"),
            (11, "Shadow Fiend", "nevermore"),
            (12, "Phantom Lancer", "phantom_lancer"),
            (13, "Puck", "puck"),
            (14, "Pudge", "pudge"),
            (15, "Razor", "razor"),
            (16, "Sand King", "sand_king"),
            (17, "Storm Spirit", "storm_spirit"),
            (18, "Sven", "sven"),
            (19, "Tiny", "tiny"),
            (20, "Vengeful Spirit", "vengefulspirit"),
            (21, "Windranger", "windrunner"),
            (22, "Zeus", "zuus"),
            (23, "Kunkka", "kunkka"),
            (25, "Lina", "lina"),
            (26, "Lion", "lion"),
            (27, "Shadow Shaman", "shadow_shaman"),
            (29, "Tidehunter", "tidehunter"),
            (30, "Witch Doctor", "witch_doctor"),
            (31, "Lich", "lich"),
            (32, "Riki", "riki"),
            (33, "Enigma", "enigma"),
            (34, "Tinker", "tinker"),
            (35, "Sniper", "sniper"),
            (36, "Necrophos", "necrolyte"),
            (37, "Warlock", "warlock"),
            (38, "Beastmaster", "beastmaster"),
            (39, "Queen of Pain", "queenofpain"),
            (40, "Venomancer", "venomancer"),
            (41, "Faceless Void", "faceless_void"),
            (42, "Wraith King", "skeleton_king"),
            (43, "Death Prophet", "death_prophet"),
            (44, "Phantom Assassin", "phantom_assassin"),
            (45, "Pugna", "pugna"),
            (46, "Templar Assassin", "templar_assassin"),
            (47, "Viper", "viper"),
            (48, "Luna", "luna"),
            (49, "Dragon Knight", "dragon_knight"),
            (50, "Dazzle", "dazzle"),
            (51, "Clockwerk", "rattletrap"),
            (52, "Leshrac", "leshrac"),
            (53, "Nature's Prophet", "furion"),
            (54, "Lifestealer", "life_stealer"),
            (55, "Dark Seer", "dark_seer"),
            (56, "Clinkz", "clinkz"),
            (57, "Omniknight", "omniknight"),
            (58, "Enchantress", "enchantress"),
            (59, "Huskar", "huskar"),
            (60, "Night Stalker", "night_stalker"),
            (61, "Broodmother", "broodmother"),
            (62, "Bounty Hunter", "bounty_hunter"),
            (63, "Weaver", "weaver"),
            (64, "Jakiro", "jakiro"),
            (65, "Batrider", "batrider"),
            (66, "Chen", "chen"),
            (67, "Spectre", "spectre"),
            (68, "Ancient Apparition", "ancient_apparition"),
            (69, "Doom", "doom_bringer"),
            (70, "Ursa", "ursa"),
            (71, "Spirit Breaker", "spirit_breaker"),
            (72, "Gyrocopter", "gyrocopter"),
            (73, "Alchemist", "alchemist"),
            (74, "Invoker", "invoker"),
            (75, "Silencer", "silencer"),
            (76, "Outworld Destroyer", "obsidian_destroyer"),
            (77, "Lycan", "lycan"),
            (78, "Brewmaster", "brewmaster"),
            (79, "Shadow Demon", "shadow_demon"),
            (80, "Lone Druid", "lone_druid"),
            (81, "Chaos Knight", "chaos_knight"),
            (82, "Meepo", "meepo"),
            (83, "Treant Protector", "treant"),
            (84, "Ogre Magi", "ogre_magi"),
            (85, "Undying", "undying"),
            (86, "Rubick", "rubick"),
            (87, "Disruptor", "disruptor"),
            (88, "Nyx Assassin", "nyx_assassin"),
            (89, "Naga Siren", "naga_siren"),
            (90, "Keeper of the Light", "keeper_of_the_light"),
            (91, "Io", "wisp"),
            (92, "Visage", "visage"),
            (93, "Slark", "slark"),
            (94, "Medusa", "medusa"),
            (95, "Troll Warlord", "troll_warlord"),
            (96, "Centaur Warrunner", "centaur"),
            (97, "Magnus", "magnataur"),
            (98, "Timbersaw", "shredder"),
            (99, "Bristleback", "bristleback"),
            (100, "Tusk", "tusk"),
            (101, "Skywrath Mage", "skywrath_mage"),
            (102, "Abaddon", "abaddon"),
            (103, "Elder Titan", "elder_titan"),
            (104, "Legion Commander", "legion_commander"),
            (105, "Techies", "techies"),
            (106, "Ember Spirit", "ember_spirit"),
            (107, "Earth Spirit", "earth_spirit"),
            (108, "Underlord", "abyssal_underlord"),
            (109, "Terrorblade", "terrorblade"),
            (110, "Phoenix", "phoenix"),
            (111, "Oracle", "oracle"),
            (112, "Winter Wyvern", "winter_wyvern"),
            (113, "Arc Warden", "arc_warden"),
            (114, "Monkey King", "monkey_king"),
            (119, "Dark Willow", "dark_willow"),
            (120, "Pangolier", "pangolier")
        };
        var heroArray = new JsonArray();
        var names = new JsonObject();
        foreach (var hero in heroes)
        {
            heroArray.Add(new JsonObject
            {
                ["id"] = hero.Id,
                ["name"] = "npc_dota_hero_" + hero.Internal
            });
            names[hero.Id.ToString()] = hero.Name;
        }
        state = (JsonObject)JsonNode.Parse("""
        {
          "status": "PREVIEW / DEMONSTRATION",
          "meta": {
            "draftReady": true,
            "ourTeam": "radiant",
            "patchTag": "DEMO"
          },
          "step": { "team": "radiant", "action": "pick" },
          "board": {
            "radiantPicks": [13, 100, 120],
            "direPicks": [74, 14],
            "radiantBans": [44, 41],
            "direBans": [8, 67]
          },
          "team": {
            "our": [20001, 20002, 20003, 20004, 20005],
            "enemy": [10001, 10002, 10003, 10004, 10005]
          },
          "result": {
            "recs": [
              { "heroId": 1, "score": 0.923 },
              { "heroId": 2, "score": 0.875 },
              { "heroId": 39, "score": 0.841 },
              { "heroId": 97, "score": 0.817 },
              { "heroId": 110, "score": 0.785 }
            ]
          },
          "ops": { "active": [] },
          "heroes": [],
          "names": {}
        }
        """)!;
        state["heroes"] = heroArray;
        state["names"] = names;
        players = (JsonObject)JsonNode.Parse("""
        {
          "players": [
            { "accountId": 10001, "persona": "NightFall", "role": "Core" },
            { "accountId": 10002, "persona": "gpk", "role": "Mid" },
            { "accountId": 10003, "persona": "Mal1ne", "role": "Mid" },
            { "accountId": 10004, "persona": "kiyotaka", "role": "Mid" },
            { "accountId": 10005, "persona": "MieRo", "role": "Offlane" }
          ]
        }
        """)!;
        stats = (JsonObject)JsonNode.Parse("""
        {
          "stats": {
            "allyPlayers": [],
            "enemyPlayers": [
              { "accountId": 10001, "scoutStatus": "ok",
                "topHeroes": [{ "id": 100, "games": 27, "winRate": 0.78 },
                              { "id": 18, "games": 14, "winRate": 0.64 }] },
              { "accountId": 10002, "scoutStatus": "ok",
                "topHeroes": [{ "id": 13, "games": 22, "winRate": 0.61 },
                              { "id": 74, "games": 12, "winRate": 0.56 }] },
              { "accountId": 10003, "scoutStatus": "no_stats", "topHeroes": [] },
              { "accountId": 10004, "scoutStatus": "ok",
                "topHeroes": [{ "id": 97, "games": 19, "winRate": 0.72 },
                              { "id": 17, "games": 9, "winRate": 0.59 }] },
              { "accountId": 10005, "scoutStatus": "error", "topHeroes": [] }
            ]
          }
        }
        """)!;
        selectedScoutId = 10001;
        selectedScoutSide = "enemy";
        RenderPage(target);
        if (target == "Драфт")
        {
            SelectTacticalHero(39);
            UpdateDraft();
        }
        else
        {
            UpdateScoutRoster();
            RenderScoutDetails();
        }
        // Permanent on-canvas marker: the user must not mistake a fixture for
        // actual scouting data or a real Dota match.
        toast.Text = "SAMPLE DATA · Визуальная проверка";
    }

    private static IEnumerable<FrameworkElement> PreviewElements(DependencyObject root)
    {
        for (int i = 0; i < VisualTreeHelper.GetChildrenCount(root); i++)
        {
            var child = VisualTreeHelper.GetChild(root, i);
            if (child is FrameworkElement element) yield return element;
            foreach (var descendant in PreviewElements(child)) yield return descendant;
        }
    }

    internal void WaitPreviewFrame(int milliseconds)
    {
        var frame = new DispatcherFrame();
        var timer = new DispatcherTimer(DispatcherPriority.Background) { Interval = TimeSpan.FromMilliseconds(milliseconds) };
        timer.Tick += (_, _) => { timer.Stop(); frame.Continue = false; };
        timer.Start();
        Dispatcher.PushFrame(frame);
        UpdateLayout();
    }

    internal void WarmPreviewArtwork(int seconds = 40)
    {
        var deadline = DateTime.UtcNow.AddSeconds(seconds);
        while (DateTime.UtcNow < deadline)
        {
            WaitPreviewFrame(200);
            var images = PreviewElements(this).OfType<Image>().Where(x => x.IsVisible).ToArray();
            if (images.Length > 0 && images.All(x => x.Source != null)) return;
        }
        var visible = PreviewElements(this).OfType<Image>().Where(x => x.IsVisible).ToArray();
        if (visible.Length > 0 && visible.Count(x => x.Source != null) < Math.Ceiling(visible.Length * 0.9))
            throw new InvalidOperationException($"Artwork warmup failed: {visible.Count(x => x.Source != null)}/{visible.Length} images loaded.");
    }

    internal JsonObject CaptureVisualPreview(string path, int width = 1440, int height = 900, double simulatedScale = 1)
    {
        // Logical viewport/raster scaling is not a native monitor DPI switch.
        var root = (FrameworkElement)Content;
        root.Width = width / simulatedScale;
        root.Height = height / simulatedScale;
        MinWidth = 0; MinHeight = 0;
        Width = root.Width; Height = root.Height;
        UpdateLayout();
        root.Measure(new Size(root.Width, root.Height));
        root.Arrange(new Rect(0, 0, root.Width, root.Height));
        root.UpdateLayout();
        WaitPreviewFrame(120);
        WarmPreviewArtwork(12);
        // Pumping asynchronous image/size events can trigger a native window
        // arrange. Settle the final exact client layout before rasterization.
        root.Measure(new Size(root.Width, root.Height));
        root.Arrange(new Rect(0, 0, root.Width, root.Height));
        root.UpdateLayout();
        if (width < 400 || height < 300)
            throw new InvalidOperationException("Preview window did not render at a useful size.");
        var screenshot = new RenderTargetBitmap(width, height,
            96 * simulatedScale, 96 * simulatedScale, PixelFormats.Pbgra32);
        screenshot.Render(root);
        var encoder = new PngBitmapEncoder();
        encoder.Frames.Add(BitmapFrame.Create(screenshot));
        using (var stream = File.Create(path)) encoder.Save(stream);
        if (Math.Abs(root.ActualWidth - root.Width) > 1 || Math.Abs(root.ActualHeight - root.Height) > 1)
            throw new InvalidOperationException($"Capture root size drifted: {root.ActualWidth}x{root.ActualHeight}; expected {root.Width}x{root.Height}");
        var report = new JsonObject {
            ["file"] = Path.GetFileName(path), ["page"] = page,
            ["width"] = width, ["height"] = height, ["simulatedScale"] = simulatedScale,
            ["logicalWidth"] = root.Width, ["logicalHeight"] = root.Height,
            ["nativeDpiX"] = VisualTreeHelper.GetDpi(this).PixelsPerInchX,
            ["dpiMode"] = "simulated layout and raster scale; not native monitor DPI", ["sampleData"] = true
        };
        var bounds = new JsonObject();
        Rect Bounds(FrameworkElement element) => element.TransformToAncestor(root).TransformBounds(new Rect(element.RenderSize));
        void AssertContained(string name, FrameworkElement element)
        {
            var rect = Bounds(element);
            if (!element.IsVisible || rect.Width <= 0 || rect.Height <= 0 || rect.Left < -1 || rect.Top < -1 || rect.Right > root.Width + 1 || rect.Bottom > root.Height + 1)
                throw new InvalidOperationException($"{Path.GetFileName(path)}: {name} outside viewport: {rect}; viewport {root.Width}x{root.Height}");
            bounds[name] = new JsonObject { ["x"] = rect.X, ["y"] = rect.Y, ["width"] = rect.Width, ["height"] = rect.Height };
        }
        ScrollViewer ScrollParent(FrameworkElement element)
        {
            DependencyObject? parent = element;
            while ((parent = VisualTreeHelper.GetParent(parent)) != null)
                if (parent is ScrollViewer viewer) return viewer;
            throw new InvalidOperationException("Expected scroll viewport for " + element.Name);
        }
        void AssertViewport(string name, FrameworkElement? content)
        {
            if (content == null) throw new InvalidOperationException("Missing " + name);
            var viewer = ScrollParent(content);
            AssertContained(name, viewer);
            if (viewer.ViewportHeight < 64 || viewer.ViewportWidth < 180)
                throw new InvalidOperationException($"{Path.GetFileName(path)}: {name} unusable viewport {viewer.ViewportWidth}x{viewer.ViewportHeight}; requires 180x64 DIP.");
        }
        if (page == "Драфт")
        {
            var elements = PreviewElements(root).ToArray();
            FrameworkElement Named(string name) => elements.FirstOrDefault(x => x.Name == name)
                ?? throw new InvalidOperationException("Missing draft component: " + name);
            var left = Named("DraftRecommendations"); var middle = Named("DraftCatalog"); var right = Named("DraftContext");
            AssertContained("DraftRecommendations", left); AssertContained("DraftCatalog", middle); AssertContained("DraftContext", right);
            AssertContained("ApplyHero", Named("ApplyHero"));
            var l = Bounds(left); var m = Bounds(middle); var r = Bounds(right);
            if (l.Right > m.Left + 1 || m.Right > r.Left + 1 || Math.Abs(l.Top - m.Top) > 2 || Math.Abs(m.Top - r.Top) > 2)
                throw new InvalidOperationException("Draft must retain three non-overlapping simultaneous columns.");
            foreach (var button in PreviewElements(left).OfType<Button>().Where(b => b.Content is string))
                AssertContained("History " + button.Content, button);
        }
        else if (page == "Разведка")
        {
            AssertViewport("ScoutAnalyticsViewport", tacticalInsights);
            AssertViewport("ScoutDetailsViewport", scoutDetails);
            if (scoutSignalHost == null) throw new InvalidOperationException("Opponent cards missing.");
            AssertContained("OpponentCards", scoutSignalHost);
            var cards = PreviewElements(scoutSignalHost).OfType<Button>().ToArray();
            if (cards.Length != 5) throw new InvalidOperationException("Scouting must show exactly five opponent cards.");
            foreach (var card in cards) AssertContained("Opponent " + Array.IndexOf(cards, card), card);
        }
        foreach (var item in navigation) AssertContained("Navigation " + item.Key, item.Value);
        // Full text must fit for persistent shell/history/primary actions.
        var checkedButtons = navigation.Values.Concat(PreviewElements(root).OfType<Button>()
            .Where(b => b.Name == "ApplyHero" || b.Content is string text && (text.Contains("Назад") || text.Contains("Вперёд"))));
        foreach (var button in checkedButtons)
        {
            if (button.Content is not string text || string.IsNullOrWhiteSpace(text)) continue;
            var formatted = new FormattedText(text, System.Globalization.CultureInfo.CurrentUICulture, button.FlowDirection,
                new Typeface(button.FontFamily, button.FontStyle, button.FontWeight, button.FontStretch), button.FontSize,
                button.Foreground, VisualTreeHelper.GetDpi(button).PixelsPerDip);
            var available = button.ActualWidth - button.Padding.Left - button.Padding.Right - button.BorderThickness.Left - button.BorderThickness.Right;
            if (formatted.WidthIncludingTrailingWhitespace > available + 1)
                throw new InvalidOperationException($"{Path.GetFileName(path)}: action label clipped: {text}; needs {formatted.WidthIncludingTrailingWhitespace:0.0}, has {available:0.0} DIP.");
        }
        report["bounds"] = bounds;
        var images = PreviewElements(root).OfType<Image>().Where(x => x.IsVisible).ToArray();
        report["loadedImages"] = images.Count(x => x.Source != null); report["visibleImages"] = images.Length;
        return report;
    }

    internal void AssertPreviewInteractions()
    {
        LoadVisualFixture("Драфт"); SelectTacticalHero(39);
        if (selectedHeroId != 39 || applyDraftButton?.IsEnabled != true) throw new InvalidOperationException("Valid hero selection failed.");
        RenderPage("Разведка"); ShowScoutDetails("enemy", 10002);
        if (selectedScoutId != 10002) throw new InvalidOperationException("Player selection failed.");
        RenderPage("Драфт");
        if (selectedHeroId != 39) throw new InvalidOperationException("Selection lost after reopening.");
        state!["board"]!["radiantPicks"]!.AsArray().Add(39); UpdateDraft();
        if (applyDraftButton?.IsEnabled == true || selectedHeroId == 39) throw new InvalidOperationException("Taken hero remains applicable.");
        state["meta"]!["draftReady"] = false; UpdateDraft(); SelectTacticalHero(1);
        if (selectedHeroId != 1) throw new InvalidOperationException("Hero selection blocked when model is not ready.");
        if (applyDraftButton?.IsEnabled == true) throw new InvalidOperationException("Apply enabled without ready model.");
    }

    internal void LoadPreviewScenario(string scenario)
    {
        LoadVisualFixture("Разведка");
        if (scenario == "empty") {
            state!["team"]!["enemy"] = new JsonArray(); stats!["stats"]!["enemyPlayers"] = new JsonArray(); selectedScoutId = 0;
        }
        else if (scenario == "loading") state!["ops"]!["active"] = new JsonArray(new JsonObject { ["kind"] = "scout", ["lastLine"] = "SAMPLE · 2 / 5 · ожидание источника" });
        else if (scenario == "error") foreach (var player in stats!["stats"]!["enemyPlayers"]!.AsArray()) player!["scoutStatus"] = "error";
        scoutVersion = ""; scoutSignalKey = "";
        UpdateLiveState(); RenderScoutDetails(); toast.Text = "SAMPLE DATA · " + scenario;
    }
}
