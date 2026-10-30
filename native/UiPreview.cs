using System;
using System.IO;
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
              { "heroId": 120, "score": 0.923 },
              { "heroId": 100, "score": 0.875 },
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

    internal void CaptureVisualPreview(string path)
    {
        UpdateLayout();
        var width = (int)Math.Ceiling(ActualWidth);
        var height = (int)Math.Ceiling(ActualHeight);
        if (width < 400 || height < 300)
            throw new InvalidOperationException("Preview window did not render at a useful size.");
        var screenshot = new RenderTargetBitmap(width, height,
            96, 96, PixelFormats.Pbgra32);
        screenshot.Render(this);
        var encoder = new PngBitmapEncoder();
        encoder.Frames.Add(BitmapFrame.Create(screenshot));
        using var stream = File.Create(path);
        encoder.Save(stream);
    }
}
